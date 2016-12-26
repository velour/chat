// +build ignore

// Package main is a demo to "test" the IRC bot Client API.
package main

import (
	"flag"
	"fmt"

	"github.com/eaburns/pretty"
	"github.com/velour/bridge/chat"
	"github.com/velour/bridge/chat/irc"
)

var (
	nick    = flag.String("n", "", "The bot's nickname")
	pass    = flag.String("p", "", "The bot's password")
	server  = flag.String("s", "irc.freenode.net:6697", "The server")
	channel = flag.String("c", "#velour-test", "The channel")
)

func main() {
	flag.Parse()
	c, err := irc.DialSSL(*server, *nick, *nick, *pass, false)
	if err != nil {
		panic(err)
	}

	ch, err := c.Join(*channel)
	if err != nil {
		panic(err)
	}

	if _, err := ch.Send("Hello,\nWorld!"); err != nil {
		panic(err)
	}

loop:
	for {
		ev, err := ch.Receive()
		if err != nil {
			panic(err)
		}
		pretty.Print(ev)
		fmt.Println("")
		switch m := ev.(type) {
		case chat.Message:
			if m.Text == "LEAVE" {
				break loop
			}
		}
	}
	if err := c.Close(); err != nil {
		panic(err)
	}
}
