package telegram

import (
	"context"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/velour/chat"
)

type channel struct {
	client *Client
	chat   Chat

	// In simulates an infinite buffered channel
	// of Updates from the Client to this channel.
	// The Client publishes Updates without blocking.
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
		for us := range ch.in {
			for _, u := range us {
				ch.out <- u
			}
		}
		close(ch.out)
	}()
	return ch
}

func (ch *channel) Receive(ctx context.Context) (interface{}, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case u, ok := <-ch.out:
			if !ok {
				return nil, io.EOF
			}
			switch ev, err := chatEvent(ch.client, u); {
			case err != nil:
				return nil, err
			case ev == nil:
				continue
			default:
				return ev, nil
			}
		}
	}
}

// chatEvent returns the chat event corresponding to the update.
// If the Update cannot be mapped, nil is returned with a nil error.
// This signifies an Update that sholud be ignored.
func chatEvent(c *Client, u *Update) (interface{}, error) {
	switch {
	case u.Message != nil && u.Message.From == nil:
		// If From is nil, this is a message sent to a channel.
		// chat.Message requires a user, so just skip these.
		return nil, nil

	case u.Message != nil && u.Message.ReplyToMessage != nil:
		if u.Message.ReplyToMessage.From == nil {
			// Replying to a channel send?
			// chat.Message requires a user.
			// Ignore the reply; just treat it as a normal message.
			return chatMessage(c, u.Message), nil
		}
		return chat.Reply{
			ReplyTo: chatMessage(c, u.Message.ReplyToMessage),
			Reply:   chatMessage(c, u.Message),
		}, nil

	case u.Message != nil && u.Message.NewChatMember != nil:
		return chat.Join{Who: chatUser(c, u.Message.NewChatMember)}, nil

	case u.Message != nil && u.Message.LeftChatMember != nil:
		return chat.Leave{Who: chatUser(c, u.Message.LeftChatMember)}, nil

	case u.Message != nil:
		return chatMessage(c, u.Message), nil

	case u.EditedMessage != nil:
		id := chatMessageID(u.EditedMessage)
		return chat.Edit{
			ID:    id,
			NewID: id,
			Text:  messageText(u.EditedMessage),
		}, nil
	}
	return nil, nil
}

func (ch *channel) send(ctx context.Context, sendAs *chat.User, replyTo *chat.Message, text string) (chat.Message, error) {
	if sendAs != nil {
		const mePrefix = "/me "
		if strings.HasPrefix(text, mePrefix) {
			text = "*" + sendAs.DisplayName() + "* " + strings.TrimPrefix(text, mePrefix)
		} else {
			text = "*" + sendAs.DisplayName() + "*: " + text
		}
	}
	req := map[string]interface{}{
		"chat_id":    ch.chat.ID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if replyTo != nil {
		req["reply_to_message_id"] = replyTo.ID
	}
	var resp Message
	if err := rpc(ctx, ch.client, "sendMessage", req, &resp); err != nil {
		return chat.Message{}, err
	}
	msg := chatMessage(ch.client, &resp)
	if sendAs != nil {
		msg.From = *sendAs
	}
	return msg, nil
}

func (ch *channel) Send(ctx context.Context, text string) (chat.Message, error) {
	return ch.send(ctx, nil, nil, text)
}

func (ch *channel) SendAs(ctx context.Context, sendAs chat.User, text string) (chat.Message, error) {
	return ch.send(ctx, &sendAs, nil, text)
}

// Delete is a no-op for Telegram, as it's bot API doesn't support message deletion.
func (ch *channel) Delete(context.Context, chat.MessageID) error { return nil }

func (ch *channel) Edit(ctx context.Context, messageID chat.MessageID, text string) (chat.MessageID, error) {
	req := map[string]interface{}{
		"chat_id":    ch.chat.ID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	var resp Message
	if err := rpc(ctx, ch.client, "editMessageText", req, &resp); err != nil {
		return "", err
	}
	return chatMessageID(&resp), nil
}

func (ch *channel) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.send(ctx, nil, &replyTo, text)
}

func (ch *channel) ReplyAs(ctx context.Context, sendAs chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.send(ctx, &sendAs, &replyTo, text)
}

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
func chatMessage(c *Client, m *Message) chat.Message {
	return chat.Message{
		ID:   chatMessageID(m),
		From: chatUser(c, m.From),
		Text: messageText(m),
	}
}

// chatUser assumes that u != nil.
func chatUser(c *Client, user *User) chat.User {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	nick := user.Username
	if nick == "" {
		nick = name
	}

	var url string
	c.Lock()
	if u, ok := c.users[user.ID]; c.localURL != nil && ok {
		u.Lock()
		url = path.Join(c.localURL.String(), u.photo)
		u.Unlock()
	}
	c.Unlock()

	return chat.User{
		ID:       chat.UserID(strconv.FormatInt(user.ID, 10)),
		Nick:     nick,
		Name:     name,
		PhotoURL: url,
	}
}
