// +build ignore

// Package main is a demo to "test" the Telegram bot Client API.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"

	"github.com/eaburns/pretty"
	"github.com/velour/chat"
	"github.com/velour/chat/bridge"
	"github.com/velour/chat/irc"
	"github.com/velour/chat/slack"
	"github.com/velour/chat/telegram"
)

var (
	telegramToken = flag.String("telegram-token", "", "The bot's Telegram token")
	telegramGroup = flag.String("telegram-group", "", "The bot's Telegram group ID")

	ircNick    = flag.String("irc-nick", "", "The bot's IRC nickname")
	ircPass    = flag.String("irc-password", "", "The bot's IRC password")
	ircServer  = flag.String("irc-server", "irc.freenode.net:6697", "The IRC server")
	ircChannel = flag.String("irc-channel", "#velour-test", "The IRC channel")

	slackToken = flag.String("slack-token", "", "The bot's Slack token")
	slackRoom  = flag.String("slack-room", "", "The bot's slack room name (not ID)")

	httpPublic = flag.String("http-public", "http://localhost:8888", "The bridge's public base URL")
	httpServe  = flag.String("http-serve", "localhost:8888", "The bridge's HTTP server host")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	channels := []chat.Channel{}

	if *ircNick != "" {
		ircClient, err := irc.DialSSL(ctx, *ircServer, *ircNick, *ircNick, *ircPass, false)
		if err != nil {
			panic(err)
		}
		ircChannel, err := ircClient.Join(ctx, *ircChannel)
		if err != nil {
			panic(err)
		}

		channels = append(channels, ircChannel)
	}

	if *telegramToken != "" {
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
		baseURL, err := url.Parse(*httpPublic)
		if err != nil {
			panic(err)
		}
		baseURL.Path = path.Join(baseURL.Path, telegramMediaPath)
		telegramClient.SetLocalURL(*baseURL)
		go http.ListenAndServe(*httpServe, nil)

		channels = append(channels, telegramChannel)
	}

	if *slackToken != "" {
		slackClient, err := slack.Dial(ctx, *slackToken)
		if err != nil {
			panic(err)
		}
		slackChannel, err := slackClient.Join(ctx, *slackRoom)
		if err != nil {
			panic(err)
		}

		channels = append(channels, slackChannel)
	}

	b := bridge.New(channels...)
	log.Println("Bridge is up and running.")
	log.Println("Connecting:")
	for _, ch := range channels {
		log.Println(ch.Name(), "on", ch.ServiceName())
	}
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
