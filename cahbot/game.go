package cahbot

import irc "github.com/fluffle/goirc/client"
import "container/ring"
import "strings"
import "fmt"
import "time"

const HAND_SIZE = 7

type User struct {
	Nick   string
	Active bool
	Joined bool
	Hand   [HAND_SIZE]*WhiteCard
}

var (
	activeUsers  *ring.Ring
	joiningUsers *ring.Ring
	users        map[string]*User = make(map[string]*User)
	IrcChannel   string           = "#cah"
	IrcNick      string           = "cahbot"
	leaderCard *BlackCard
	playedCards  []*WhiteCard
	lenLessOne   int
)

func MsgPrintF(conn *irc.Conn, target string, format string, a ...interface{}) {
	conn.Privmsg(target, fmt.Sprintf(format, a...))
}

func fillHand(user *User, conn *irc.Conn) {
	var drawn [HAND_SIZE]*WhiteCard
	for i, c := range user.Hand {
		if c == nil {
			card := drawCard()
			user.Hand[i] = card
			drawn[i] = card
		}
	}
	conn.Privmsg(user.Nick, cardsList(drawn[:], "You drew"))
}

func cardsList(cards []*WhiteCard, msgprefix string) (msg string) {
	for i, card := range cards {
		if card != nil {
			msg += fmt.Sprintf("%s (%c) \"%s\"", msgprefix, int('a')+i, card.Text)
			msgprefix = ";"
		}
	}
	return
}

func tellHand(nick string, user *User, rest string, conn *irc.Conn) {
	conn.Privmsg(user.Nick, cardsList(user.Hand[:], "You have"))
}

func userJoin(nick string, user *User, rest string, conn *irc.Conn) {
	if user == nil {
		user = new(User)
		user.Nick = nick
		users[nick] = user
	} else if user.Joined {
		conn.Privmsg(nick, "Already joined!")
		return
	}
	user.Joined = true
	newRing := ring.New(1)
	newRing.Value = user
	joiningUsers = newRing.Link(joiningUsers)
	conn.Privmsg(nick, "Succesfully joined. (Not yet active, wait for next round.)")
	MsgPrintF(conn, IrcChannel, "%s joined.", nick)
	if activeUsers == nil {
		nextRound(conn, nil)
	}
}

func nextRound(conn *irc.Conn, wait <-chan time.Time) {
	if activeUsers.Len()+joiningUsers.Len() < 3 {
		conn.Privmsg(IrcChannel, "Not enough users to start.")
		return
	}
	if joiningUsers != nil {
		activeMsg := ""
		activeMsgPrefix := "Now active:"
		joiningUsers.Do(func(v interface{}) {
			user := v.(*User)
			user.Active = true
			fillHand(user, conn)
			activeMsg += fmt.Sprintf("%s %s", activeMsgPrefix, user.Nick)
			activeMsgPrefix = ","
		})
		MsgPrintF(conn, IrcChannel, "%s.", activeMsg)
		if activeUsers == nil {
			activeUsers = joiningUsers
		} else {
			activeUsers = activeUsers.Link(joiningUsers)
		}
		joiningUsers = nil
	} else {
		activeUsers = activeUsers.Next()
	}
	if wait != nil {
		<- wait
	}
	leader := activeUsers.Value.(*User)
	leaderCard = nextBlackCard()
	MsgPrintF(conn, IrcChannel, "%s is the leader! The black card is \"%s\"", leader.Nick, leaderCard.Text)
	lenLessOne = activeUsers.Len() - 1
	playedCards = make([]*WhiteCard, 0, lenLessOne)
}

func leaderChoose(nick string, user *User, rest string, conn *irc.Conn) {
	if user != activeUsers.Value {
		conn.Privmsg(nick, "You're not the leader!")
		return
	}
	if len(playedCards) != lenLessOne {
		conn.Privmsg(nick, "Not all the cards are in!")
		return
	}
	err, card, _ := namedCard(playedCards, rest)
	if err != "" {
		conn.Privmsg(nick, err)
		return
	}
	MsgPrintF(conn, IrcChannel, "%s has chosen %s's card:", nick, card.Holder.Nick)
	MsgPrintF(conn, IrcChannel, "\"%s\": \"%s\"", leaderCard.Text, card.Text)
	for _, c := range playedCards {
		c.Holder.Hand[c.Index] = nil
		fillHand(c.Holder, conn)
		discardCard(c)
	}
	nextRound(conn,time.After(10*time.Second))
}

func namedCard(cards []*WhiteCard, rest string) (string, *WhiteCard, int) {
	spl := strings.Fields(rest)
	if len(spl) != 1 || len(spl[0]) != 1 {
		return "Could not parse. Give me a single letter plx.", nil, 0
	}
	c := int(spl[0][0]) - int('a')
	if c < 0 || c >= len(cards) {
		return "That's not a letter naming a card...", nil, 0
	}
	return "", cards[c], c
}

func checkReadyToChoose(conn *irc.Conn) {
	if len(playedCards) == lenLessOne {
		conn.Privmsg(IrcChannel, cardsList(playedCards, "All cards are in: "))
		conn.Privmsg(activeUsers.Value.(*User).Nick, "Time to choose.")
	}
	if len(playedCards) > lenLessOne {
		panic("Programming error, playedCards has too many elements.")
	}
}

func selectCard(nick string, user *User, rest string, conn *irc.Conn) {
	if user == activeUsers.Value {
		conn.Privmsg(nick, "You're the leader, use 'choose' when your time comes.")
		return
	}
	if len(playedCards) == lenLessOne {
		conn.Privmsg(nick, "Cards are locked for choosing.")
		return
	}
	err, card, index := namedCard(user.Hand[:], rest)
	if err != "" {
		conn.Privmsg(nick, err)
		return
	}
	card.Holder = user
	card.Index = index
	playedCards = append(playedCards, card)
	didPlay := make([]*WhiteCard, HAND_SIZE)
	didPlay[index] = card
	conn.Privmsg(nick, cardsList(didPlay, "You played"))
	checkReadyToChoose(conn)
}

type CommandFunc func(nick string, user *User, rest string, conn *irc.Conn)
type Command struct {
	Name          string
	Function      CommandFunc
	AllowInactive bool
}

func NewCommand(name string, function CommandFunc, allowInactive bool) *Command {
	c := new(Command)
	c.Name = name
	c.Function = function
	c.AllowInactive = allowInactive
	return c
}

var Commands []*Command = []*Command{
	NewCommand("join", userJoin, true),
	NewCommand("choose", leaderChoose, false),
	NewCommand("hand", tellHand, false),
	NewCommand("select", selectCard, false)}

func HandlePrivMsg(conn *irc.Conn, line *irc.Line) {
	if line.Args[0] != IrcNick {
		return
	}
	spl := strings.SplitN(line.Args[1], " ", 2)
	rest := ""
	if len(spl) > 1 {
		rest = spl[1]
	}
	nick := line.Nick
	user := users[nick]
	for _, c := range Commands {
		if strings.HasPrefix(c.Name, spl[0]) {
			if (user != nil && user.Active) || c.AllowInactive {
				c.Function(nick, user, rest, conn)
			} else {
				conn.Privmsg(nick, "You must be active (implying join) first!")
			}
			return
		}
	}
	conn.Privmsg(nick, "Unrecognized command")
}
