// +build ignore

// Package main is a demo to "test" the Telegram bot Client API.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/eaburns/pretty"
	"github.com/velour/chat"
	"github.com/velour/chat/bridge"
	"github.com/velour/chat/irc"
	"github.com/velour/chat/telegram"
)

var (
	telegramToken = flag.String("telegram-token", "", "The bot's Telegram token")
	telegramGroup = flag.String("telegram-group", "", "The bot's Telegram group ID")

	ircNick    = flag.String("irc-nick", "", "The bot's IRC nickname")
	ircPass    = flag.String("irc-password", "", "The bot's IRC password")
	ircServer  = flag.String("irc-server", "irc.freenode.net:6697", "The IRC server")
	ircChannel = flag.String("irc-channel", "#velour-test", "The IRC channel")

	httpPort = flag.String("http-port", ":8888", "The bridge's HTTP server port")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	ircClient, err := irc.DialSSL(ctx, *ircServer, *ircNick, *ircNick, *ircPass, false)
	if err != nil {
		panic(err)
	}
	ircChannel, err := ircClient.Join(ctx, *ircChannel)
	if err != nil {
		panic(err)
	}

	telegramClient, err := telegram.Dial(ctx, *telegramToken)
	if err != nil {
		panic(err)
	}
	telegramChannel, err := telegramClient.Join(ctx, *telegramGroup)
	if err != nil {
		panic(err)
	}

	const telegramMediaPath = "/telegram/media/"
	http.Handle(telegramMediaPath, telegramClient)
	telegramClient.SetLocalURL(url.URL{
		Scheme: "http",
		Host:   "localhost" + *httpPort,
		Path:   telegramMediaPath,
	})
	go http.ListenAndServe(*httpPort, nil)

	b := bridge.New(ircChannel, telegramChannel)
	if _, err := b.Send(ctx, "Hello, World!"); err != nil {
		panic(err)
	}

loop:
	for {
		ev, err := b.Receive(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		pretty.Print(ev)
		fmt.Println("")
		switch m := ev.(type) {
		case chat.Message:
			if m.Text == "LEAVE" {
				if _, err := b.Send(ctx, "Good bye"); err != nil {
					panic(err)
				}
				break loop
			}
		}
	}
	if err := b.Close(ctx); err != nil {
		panic(err)
	}
}
