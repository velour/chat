// +build ignore

// Package main is a demo to "test" the Telegram bot Client API.
package main

import (
	"flag"
	"fmt"

	"github.com/eaburns/pretty"
	"github.com/velour/bridge/chat/telegram"
)

var token = flag.String("token", "", "The bot's token")

func main() {
	flag.Parse()
	c, err := telegram.New(*token)
	if err != nil {
		panic(err)
	}

	ch, err := c.Join("-159332884")
	if err != nil {
		panic(err)
	}

	id, err := ch.Send("Hello.")
	if err != nil {
		panic(err)
	}
	fmt.Println("sent message ID", id)

	for {
		ev, err := ch.Receive()
		if err != nil {
			panic(err)
		}
		pretty.Print(ev)
		fmt.Println("")
	}
}
