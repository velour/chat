package slack

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/velour/chat"
)

// A Channel object describes a slack channel.
type Channel struct {
	ID string `json:"id"`

	// Name is the name of the channel WITHOUT a leading #.
	Name string `json:"name"`

	client *Client
	in     chan []*Update
	out    chan *Update
}

// makeChannel creates a new channel
func makeChannel(c *Client, id, name string) Channel {
	return makeChannelFromChannel(c, Channel{ID: id, Name: name})
}

// makeChannelFromChannel fills in an empty channel's privates
//
// Used when a marshaler has created a Channel
func makeChannelFromChannel(c *Client, ch Channel) Channel {
	ch.client = c
	ch.in = make(chan []*Update, 1)
	ch.out = make(chan *Update)
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

func (ch Channel) Receive(ctx context.Context) (interface{}, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case u, ok := <-ch.out:
			if !ok {
				return nil, io.EOF
			}
			switch ev, err := ch.chatEvent(u); {
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
func (ch *Channel) chatEvent(u *Update) (interface{}, error) {
	switch {
	case u.Type == "message" && u.User != "":
		return ch.chatMessage(u)

	default:
		return nil, nil
	}
}

// chatMessage converts a valid "message" type into a chat.Message
func (ch *Channel) chatMessage(msg *Update) (interface{}, error) {
	user, err := ch.client.getUser(msg.User)
	if err != nil {
		return nil, err
	}
	return chat.Message{
		ID: chat.MessageID(msg.Ts),
		// TODO(cws): cache slack users/nicks
		From: user,
		Text: msg.Text,
	}, nil
}

// Send sends text to the Channel and returns the sent Message.
func (ch *Channel) send(ctx context.Context, sendAs *chat.User, text string) (chat.Message, error) {
	// Do not attempt to send empty messages
	// TODO(cws): make bridge just not crash when errors come back from Send/SendAs)
	if text == "" {
		return chat.Message{}, nil
	}

	text = filterOutgoing(text)

	args := []string{
		"channel=" + ch.ID,
		"text=" + text,
	}
	if sendAs != nil {
		args = append(args, "username="+sendAs.DisplayName())
		args = append(args, "as_user=false")
		args = append(args, "icon_url="+sendAs.PhotoURL)
	} else {
		sendAs = &chat.User{}
	}

	var resp struct {
		ResponseError
		ID string
	}
	err := ch.client.do(&resp, "chat.postMessage", args...)
	if err != nil {
		return chat.Message{}, err
	}
	if !resp.OK {
		return chat.Message{}, resp
	}

	id := chat.MessageID(resp.ID)
	msg := chat.Message{
		ID:   id,
		From: *sendAs,
		Text: text,
	}

	return msg, err
}

// filterOutgoing checks an outgoing Slack message body for network conversion issues
func filterOutgoing(text string) string {
	if strings.HasPrefix(text, "/me ") {
		text = strings.TrimPrefix(text, "/me ")
		text = fmt.Sprintf("_%s_", text)
	}
	return text
}

func (ch Channel) Send(ctx context.Context, text string) (chat.Message, error) {
	return ch.send(ctx, nil, text)
}

func (ch Channel) SendAs(ctx context.Context, sendAs chat.User, text string) (chat.Message, error) {
	return ch.send(ctx, &sendAs, text)
}

func (ch Channel) Delete(ctx context.Context, id chat.MessageID) error {
	var resp struct {
		ResponseError
	}
	err := ch.client.do(&resp, "chat.delete", "ts="+string(id), "channel="+ch.ID)
	if !resp.OK {
		return resp
	}
	return err
}

func (ch Channel) Edit(ctx context.Context, id chat.MessageID, newText string) (chat.MessageID, error) {
	var resp struct {
		ResponseError
	}
	err := ch.client.do(&resp, "chat.update", "channel="+ch.ID, "ts="+string(id), "text="+newText)
	if !resp.OK {
		return id, resp
	}
	return id, err
}

func (ch Channel) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.Send(ctx, text)
}

func (ch Channel) ReplyAs(ctx context.Context, sendAs chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.SendAs(ctx, sendAs, text)
}
