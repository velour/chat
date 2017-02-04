package telegram

import (
	"context"
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

func (ch *channel) PrettyPrint() string {
	return "\"" + ch.Name() + " at " + ch.ServiceName() + "\""
}

func (ch *channel) Name() string {
	if ch.chat.Title != nil {
		return *ch.chat.Title
	}
	return ""
}

func (ch *channel) ServiceName() string { return "Telegram" }

func (ch *channel) Receive(ctx context.Context) (chat.Event, error) {
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
func chatEvent(ch *channel, u *Update) (chat.Event, error) {
	switch {
	case u.Message != nil && u.Message.Time().Before(ch.created):
	case u.EditedMessage != nil && u.EditedMessage.Time().Before(ch.created):
		// Ignore messages that originated before the channel was created.

	case u.Message != nil && u.Message.From == nil:
		// Ignore messages without a From field; chat.Message needs a From.

	case u.Message != nil:
		switch msg := u.Message; {
		case msg.NewChatMember != nil:
			who := chatUser(ch, *msg.NewChatMember)
			return chat.Join{Who: *who}, nil

		case msg.LeftChatMember != nil:
			who := chatUser(ch, *msg.NewChatMember)
			return chat.Leave{Who: *who}, nil

		case msg.Document != nil:
			if url := mediaURL(ch.client, msg.Document.FileID); url != "" {
				return chat.Message{
					ID:   chatMessageID(msg),
					From: chatUser(ch, *msg.From),
					Text: "/me shared a file: " + url,
				}, nil
			}

		case msg.Photo != nil:
			if url := mediaURL(ch.client, largestPhoto(*msg.Photo)); url != "" {
				return chat.Message{
					ID:   chatMessageID(msg),
					From: chatUser(ch, *msg.From),
					Text: "/me shared a photo: " + url,
				}, nil
			}

		case msg.Sticker != nil:
			fileID := msg.Sticker.FileID
			if msg.Sticker.Thumb != nil {
				fileID = msg.Sticker.Thumb.FileID
			}
			var icon string
			if msg.Sticker.Emoji != nil {
				icon = *msg.Sticker.Emoji
			}
			url := mediaURL(ch.client, fileID)
			if url != "" {
				// Slack does not unfurl URLs posted within the last hour.
				// But we want stickers to unfurl each time they are posted.
				// So, we add a nonce to the end, makeing each unique.
				url = url + "?nonce=" + strconv.FormatInt(time.Now().UnixNano(), 16)
			}
			var text string
			switch {
			case icon != "" && url != "":
				text = "/me sent a sticker " + icon + ": " + url
			case icon != "" && url == "":
				text = "/me sent a sticker " + icon
			case icon == "" && url != "":
				text = "/me sent a sticker: " + url
			}
			if text != "" {
				return chat.Message{
					ID:   chatMessageID(msg),
					From: chatUser(ch, *msg.From),
					Text: text,
				}, nil
			}

		case msg.Text != nil:
			return *chatMessage(ch, msg), nil
		}

	case u.EditedMessage != nil:
		msg := u.EditedMessage
		id := chatMessageID(msg)
		return chat.Edit{OrigID: id, New: *chatMessage(ch, msg)}, nil
	}
	return nil, nil
}

func (ch *channel) Send(ctx context.Context, msg chat.Message) (chat.Message, error) {
	req := map[string]interface{}{
		"chat_id":    ch.chat.ID,
		"text":       formatText(msg),
		"parse_mode": "HTML",
	}
	if msg.ReplyTo != nil {
		req["reply_to_message_id"] = msg.ReplyTo.ID
	}
	var resp Message
	if err := rpc(ctx, ch.client, "sendMessage", req, &resp); err != nil {
		return chat.Message{}, err
	}
	msg.ID = chatMessageID(&resp)
	return msg, nil
}

// Delete is a no-op for Telegram, as it's bot API doesn't support message deletion.
func (ch *channel) Delete(context.Context, chat.Message) error { return nil }

func (ch *channel) Edit(ctx context.Context, msg chat.Message) (chat.Message, error) {
	req := map[string]interface{}{
		"chat_id":    ch.chat.ID,
		"message_id": msg.ID,
		"text":       formatText(msg),
		"parse_mode": "HTML",
	}
	var resp Message
	if err := rpc(ctx, ch.client, "editMessageText", req, &resp); err != nil {
		return chat.Message{}, err
	}
	msg.ID = chatMessageID(&resp)
	return msg, nil
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
func chatMessage(ch *channel, m *Message) *chat.Message {
	msg := &chat.Message{
		ID:   chatMessageID(m),
		From: chatUser(ch, *m.From),
		Text: messageText(m),
	}
	if m.ReplyToMessage != nil && m.ReplyToMessage.From != nil {
		msg.ReplyTo = chatMessage(ch, m.ReplyToMessage)
	}
	return msg
}

// chatUser returns a chat.User from a User.
// Must not be called with the ch.client Lock held.
func chatUser(ch *channel, user User) *chat.User {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	nick := user.Username
	if nick == "" {
		nick = name
	}
	photoURL, _ := userPhotoURL(ch.client, user.ID)
	return &chat.User{
		ID:          chat.UserID(strconv.FormatInt(user.ID, 10)),
		Nick:        nick,
		FullName:    name,
		DisplayName: name,
		PhotoURL:    photoURL,
		Channel:     ch,
	}
}

func userPhotoURL(c *Client, userID int64) (string, bool) {
	c.Lock()
	defer c.Unlock()
	u, ok := c.users[userID]
	if c.localURL == nil || !ok {
		return "", false
	}
	u.Lock()
	defer u.Unlock()
	newURL, _ := url.Parse(c.localURL.String())
	newURL.Path = path.Join(newURL.Path, u.photo)
	return newURL.String(), true
}

// mediaURL returns the URL for the fileID if the client has localURL set,
// otherwise it returns the empty string.
func mediaURL(c *Client, fileID string) string {
	c.Lock()
	defer c.Unlock()
	if c.localURL == nil {
		return ""
	}
	newURL, _ := url.Parse(c.localURL.String())
	newURL.Path = path.Join(newURL.Path, fileID)
	return newURL.String()
}
