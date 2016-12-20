package telegram

import (
	"io"
	"strconv"
	"strings"

	"github.com/velour/bridge/chat"
)

type channel struct {
	client *Client
	chat   Chat

	// In simulates an infinite buffered channel
	// of Updates from the Client to this Channel.
	// The Client publishes Updates without blocking to in.
	in chan []*Update

	// Out publishes Updates to the Receive method.
	// If the in channel is closed, out is closed
	// after all pending Updates have been Received.
	out chan *Update
}

func newChannel(client *Client, chat Chat) *channel {
	ch := &channel{
		client: client,
		chat:   chat,
		in:     make(chan []*Update, 1),
		out:    make(chan *Update),
	}
	go func() {
		for {
			us, ok := <-ch.in
			if !ok {
				break
			}
			for _, u := range us {
				ch.out <- u
			}
		}
		close(ch.out)
	}()
	return ch
}

func (ch *channel) Receive() (interface{}, error) {
	for u := range ch.out {
		switch {
		case u.Message != nil && u.Message.From == nil:
			// If From is nil, this is a message sent to a channel.
			// chat.Message requires a user, so just skip these.
			continue

		case u.Message != nil && u.Message.ReplyToMessage != nil:
			if u.Message.ReplyToMessage.From == nil {
				// Replying to a channel send?
				// chat.Message requires a user.
				// Ignore the reply; just treat it as a normal message.
				return chatMessage(u.Message), nil
			}
			return chat.Reply{
				ReplyTo: chatMessage(u.Message.ReplyToMessage),
				Reply:   chatMessage(u.Message),
			}, nil

		case u.Message != nil && u.Message.NewChatMember != nil:
			return chat.Join{Who: chatUser(u.Message.NewChatMember)}, nil

		case u.Message != nil && u.Message.LeftChatMember != nil:
			return chat.Leave{Who: chatUser(u.Message.LeftChatMember)}, nil

		case u.Message != nil:
			return chatMessage(u.Message), nil

		case u.EditedMessage != nil:
			id := chatMessageID(u.EditedMessage)
			return chat.Edit{
				ID:    id,
				NewID: id,
				Text:  messageText(u.EditedMessage),
			}, nil
		}
	}
	return nil, io.EOF
}

func (ch *channel) Send(text string) (chat.MessageID, error) {
	req := map[string]interface{}{
		"chat_id":    ch.chat.ID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	var resp Message
	if err := rpc(ch.client, "sendMessage", req, &resp); err != nil {
		return "", err
	}
	return chatMessageID(&resp), nil
}

func (ch *channel) Delete(chat.MessageID) error { panic("unimplemented") }

func (ch *channel) Edit(chat.MessageID, string) (chat.MessageID, error) { panic("unimplemented") }

func (ch *channel) Reply(chat.Message, string) (chat.MessageID, error) { panic("unimplemented") }

func chatMessageID(m *Message) chat.MessageID {
	return chat.MessageID(strconv.FormatUint(m.MessageID, 10))
}

func messageText(m *Message) string {
	var text string
	if m.Text != nil {
		text = *m.Text
	}
	return text
}

// chatMessage assumes that m.From != nil.
func chatMessage(m *Message) chat.Message {
	return chat.Message{
		ID:   chatMessageID(m),
		From: chatUser(m.From),
		Text: messageText(m),
	}
}

// chatUser assumes that u != nil.
func chatUser(u *User) chat.User {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	nick := u.Username
	if nick == "" {
		nick = name
	}
	return chat.User{
		ID:   chat.UserID(strconv.FormatInt(u.ID, 10)),
		Nick: nick,
		Name: name,
	}
}
