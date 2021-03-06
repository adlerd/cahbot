/* Copyright (C) 2013 David Adler */

package main

import irc "github.com/fluffle/goirc/client"
import "fmt"
import "cahbot"
import "time"
import "math/rand"

func main() {
	rand.Seed(time.Now().UnixNano())

	var remove irc.Remover

	cfg := irc.NewConfig(cahbot.IrcNick)
	cfg.SSL = false
	cfg.Server = "localhost:6667"
	cfg.NewNick = func(n string) string { cahbot.IrcNick = n + "^"; return cahbot.IrcNick }
	cfg.Recover = func(c *irc.Conn, l *irc.Line) {
		if err := recover(); err != nil {
			remove.Remove()
			c.Quit("Panic!")
			time.Sleep(time.Second)
			panic(err)
		}
	}
	c := irc.Client(cfg)

	c.HandleFunc("connected",
		func(conn *irc.Conn, line *irc.Line) { conn.Join(cahbot.IrcChannel) })

	quit := make(chan bool)
	remove = c.HandleFunc("disconnected",
		func(conn *irc.Conn, line *irc.Line) { quit <- true })

	c.HandleFunc("PRIVMSG", cahbot.HandlePrivMsg)

	if err := c.Connect(); err != nil {
		fmt.Printf("Connection error: %s\n", err.Error())
	}

	<-quit
}
