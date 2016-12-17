// +build ignore

// Package main is a demo to "test" the Telegram bot Client API.
package main

import (
	"flag"

	"github.com/velour/bridge/chat/telegram"
)

var token = flag.String("token", "", "The bot's token")

func main() {
	flag.Parse()
	_, err := telegram.New(*token)
	if err != nil {
		panic(err)
	}
	select {}
}
