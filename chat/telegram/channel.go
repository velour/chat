package telegram

import "github.com/velour/bridge/chat"

type channel struct {
	updates chan *Update
}

func newChannel() *channel {
	return &channel{
		updates: make(chan *Update, 100),
	}
}

func (ch *channel) Receive() (interface{}, error) { panic("unimplemented") }

func (ch *channel) Send(text string) (chat.MessageID, error) { panic("unimplemented") }

func (ch *channel) Delete(chat.MessageID) error { panic("unimplemented") }

func (ch *channel) Edit(chat.MessageID, string) (chat.MessageID, error) { panic("unimplemented") }

func (ch *channel) Reply(chat.Message, string) (chat.MessageID, error) { panic("unimplemented") }
