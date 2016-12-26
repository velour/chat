// +build ignore

// Package main is a demo to "test" the Telegram bot Client API.
package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/eaburns/pretty"
	"github.com/velour/bridge/chat"
	"github.com/velour/bridge/chat/telegram"
)

var token = flag.String("token", "", "The bot's token")

func main() {
	flag.Parse()
	ctx := context.Background()
	c, err := telegram.Dial(ctx, *token)
	if err != nil {
		panic(err)
	}

	ch, err := c.Join(ctx, "-159332884")
	if err != nil {
		panic(err)
	}

	if _, err := ch.Send(ctx, "Hello, World!"); err != nil {
		panic(err)
	}

loop:
	for {
		ev, err := ch.Receive(ctx)
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
	if err := c.Close(ctx); err != nil {
		panic(err)
	}
}
