package telegram

import (
	"context"
	"html"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

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

	// Created is the time that the Channel was created.
	created time.Time
}

func newChannel(client *Client, chat Chat) *channel {
	ch := &channel{
		client:  client,
		chat:    chat,
		in:      make(chan []*Update, 1),
		out:     make(chan *Update),
		created: time.Now(),
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

func (ch *channel) Name() string {
	if ch.chat.Title != nil {
		return *ch.chat.Title
	}
	return ""
}

func (ch *channel) ServiceName() string { return "Telegram" }

func (ch *channel) Receive(ctx context.Context) (interface{}, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case u, ok := <-ch.out:
			if !ok {
				return nil, io.EOF
			}
			switch ev, err := chatEvent(ch, u); {
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
func chatEvent(ch *channel, u *Update) (interface{}, error) {
	switch {
	case u.Message != nil && u.Message.Time().Before(ch.created):
	case u.EditedMessage != nil && u.EditedMessage.Time().Before(ch.created):
		// Ignore messages that originated before the channel was created.

	case u.Message != nil && u.Message.From == nil:
		// Ignore messages without a From field; chat.Message needs a From.

	case u.Message != nil:
		switch msg := u.Message; {
		case msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil:
			// If ReplyToMessage doesn't have a From, treat it as a regular Send,
			// because chat.Message needs a From to fill ReplyTo.
			replyTo := chatMessage(ch.client, msg.ReplyToMessage)
			reply := chatMessage(ch.client, msg)
			return chat.Reply{ReplyTo: replyTo, Reply: reply}, nil

		case msg.NewChatMember != nil:
			who := chatUser(ch.client, msg.NewChatMember)
			return chat.Join{Who: who}, nil

		case msg.LeftChatMember != nil:
			who := chatUser(ch.client, msg.NewChatMember)
			return chat.Leave{Who: who}, nil

		default:
			return chatMessage(ch.client, msg), nil
		}

	case u.EditedMessage != nil:
		msg := u.EditedMessage
		id := chatMessageID(msg)
		text := messageText(msg)
		return chat.Edit{ID: id, NewID: id, Text: text}, nil
	}
	return nil, nil
}

func (ch *channel) send(ctx context.Context, sendAs *chat.User, replyTo *chat.Message, text string) (chat.Message, error) {
	markdownText := text
	htmlText := html.EscapeString(text)
	if sendAs != nil {
		const mePrefix = "/me "
		name := sendAs.Name()
		if strings.HasPrefix(text, mePrefix) {
			htmlText = "<b>" + name + "</b> " + strings.TrimPrefix(htmlText, mePrefix)
			markdownText = "*" + name + "* " + strings.TrimPrefix(markdownText, mePrefix)
		} else {
			htmlText = "<b>" + name + ":</b> " + htmlText
			markdownText = "*" + name + "*: " + markdownText
		}
	}
	req := map[string]interface{}{
		"chat_id":    ch.chat.ID,
		"text":       markdownText,
		"parse_mode": "Markdown",
	}
	if replyTo != nil {
		req["reply_to_message_id"] = replyTo.ID
	}
	var resp Message
	err := rpc(ctx, ch.client, "sendMessage", req, &resp)
	if err != nil && strings.Contains(err.Error(), "Can't parse message text") {
		// If Telegram could not parse with Markdown, try again with HTML.
		// Telegram is picky about Markdown meta-characters;
		// it requires each * to have a terminating *, for example.
		// We can reliably send using HTML, because we can simply escape it.
		req["parse_mode"] = "HTML"
		req["text"] = htmlText
		err = rpc(ctx, ch.client, "sendMessage", req, &resp)
	}
	if err != nil {
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
	err := rpc(ctx, ch.client, "editMessageText", req, &resp)
	if err != nil && strings.Contains(err.Error(), "Can't parse message text") {
		// If Telegram could not parse with Markdown, try again with HTML.
		// Telegram is picky about Markdown meta-characters;
		// it requires each * to have a terminating *, for example.
		// We can reliably send using HTML, because we can simply escape it.
		req["parse_mode"] = "HTML"
		req["text"] = html.EscapeString(text)
		err = rpc(ctx, ch.client, "editMessageText", req, &resp)
	}
	if err != nil {
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

	var photoURL string
	c.Lock()
	if u, ok := c.users[user.ID]; c.localURL != nil && ok {
		u.Lock()
		newURL, _ := url.Parse(c.localURL.String())
		newURL.Path = path.Join(newURL.Path, u.photo)
		photoURL = newURL.String()
		u.Unlock()
	}
	c.Unlock()

	return chat.User{
		ID:          chat.UserID(strconv.FormatInt(user.ID, 10)),
		Nick:        nick,
		FullName:    name,
		DisplayName: name,
		PhotoURL:    photoURL,
	}
}
