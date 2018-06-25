// +build ignore

// Package main is for testing out the discord client.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/eaburns/pretty"
	"github.com/velour/chat"
	"github.com/velour/chat/discord"
)

var (
	token   = flag.String("token", "", "The bot's super secret token")
	channel = flag.String("channel", "", "server:channel")
)

func main() {
	flag.Parse()
	if *token == "" {
		fmt.Println("need a token")
		os.Exit(1)
	}
	serverChan := strings.Split(*channel, ":")
	if *channel != "" && len(serverChan) != 2 {
		fmt.Println("malformed -channel=<server:channel>")
		os.Exit(1)
	}

	ctx := context.Background()
	cl, err := discord.Dial(ctx, *token)
	if err != nil {
		fmt.Println("dial error:", err.Error())
		os.Exit(1)
	}

	if len(serverChan) == 2 {
		go func() {
			fmt.Println("joining", serverChan[0], serverChan[1])
			ch, err := cl.Join(ctx, serverChan[0], serverChan[1])
			if err != nil {
				fmt.Println("failed to join", *channel, err)
				return
			}
			m, err := chat.Say(ctx, ch, "I have returned\nmultiple lines")
			if err != nil {
				fmt.Println("failed to say", err)
				return
			}
			go func() {
				time.Sleep(10 * time.Second)
				m.Text = "I am still here"
				if m, err = ch.Edit(ctx, m); err != nil {
					fmt.Println("failed to edit", err)
					return
				}
				if _, err := ch.Send(ctx, chat.Message{ReplyTo: &m, Text: "reply"}); err != nil {
					fmt.Println("failed to reply", err)
					return
				}
				if _, err := ch.Send(ctx, chat.Message{From: &chat.User{DisplayName: "jelca"}, Text: "send from\nmultiple lines"}); err != nil {
					fmt.Println("failed to send from", err)
					return
				}
				if _, err := ch.Send(ctx, chat.Message{From: &chat.User{DisplayName: "jelca"}, ReplyTo: &m, Text: "reply from"}); err != nil {
					fmt.Println("failed to reply from", err)
					return
				}
				time.Sleep(10 * time.Second)
				m.Text = "I am still here"
				if err = ch.Delete(ctx, m); err != nil {
					fmt.Println("failed to delete", err)
					return
				}
				time.Sleep(10 * time.Second)
				if m, err = chat.Say(ctx, ch, "/me tests emoting"); err != nil {
					fmt.Println("failed to emote", err)
					return
				}
			}()
			for {
				ev, err := ch.Receive(ctx)
				if err != nil {
					if err != io.EOF {
						log.Println("failed to recieve", err)
					}
					return
				}
				log.Println(pretty.String(ev))
			}
		}()
	}

	time.Sleep(100 * time.Second)
	if err := cl.Close(ctx); err != nil {
		fmt.Println("close error:", err.Error())
		os.Exit(1)
	}
}
