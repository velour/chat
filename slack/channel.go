package slack

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"strings"

	"github.com/velour/chat"
)

// A Channel object describes a slack channel.
type Channel struct {
	ID string `json:"id"`

	// ChannelName is the name of the channel WITHOUT a leading #.
	ChannelName string `json:"name"`

	client *Client
	in     chan []*Update
	out    chan *Update
}

// makeChannel creates a new channel
func makeChannel(c *Client, id, name string) Channel {
	return makeChannelFromChannel(c, Channel{ID: id, ChannelName: name})
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

func (ch Channel) Name() string        { return ch.ChannelName }
func (ch Channel) ServiceName() string { return ch.client.domain + ".slack.com" }

func (ch Channel) Receive(ctx context.Context) (interface{}, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case u, ok := <-ch.out:
			if !ok {
				return nil, io.EOF
			}
			switch ev, err := ch.chatEvent(ctx, u); {
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
func (ch *Channel) chatEvent(ctx context.Context, u *Update) (interface{}, error) {
	switch {
	case u.Type == "message" && u.User != "":
		return ch.chatMessage(ctx, u)

	default:
		return nil, nil
	}
}

// chatMessage converts a valid "message" type into a chat.Message
func (ch *Channel) chatMessage(ctx context.Context, msg *Update) (interface{}, error) {
	user, err := ch.client.getUser(ctx, msg.User)
	if err != nil {
		return nil, err
	}
	id := chat.MessageID(msg.Ts)
	text := html.UnescapeString(msg.Text)

	findUser := func(id string) (string, bool) {
		u, err := ch.client.getUser(ctx, chat.UserID(id))
		if err != nil {
			log.Printf("Failed to lookup mention user %s: %s\n", id, err)
			return "", false
		}
		return u.Name(), true
	}
	text = fixText(findUser, text)

	return chat.Message{ID: id, From: user, Text: text}, nil
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
		args = append(args, "username="+sendAs.Name())
		args = append(args, "as_user=false")
		args = append(args, "icon_url="+sendAs.PhotoURL)
	} else {
		sendAs = &chat.User{}
	}

	var resp struct {
		ResponseHeader
		TS string `json:"ts"` // message timestamp
	}
	if err := rpc(ctx, ch.client, &resp, "chat.postMessage", args...); err != nil {
		return chat.Message{}, err
	}

	id := chat.MessageID(resp.TS)
	msg := chat.Message{ID: id, From: *sendAs, Text: text}
	return msg, nil
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
	var resp ResponseHeader
	return rpc(ctx, ch.client, &resp,
		"chat.delete",
		"ts="+string(id),
		"channel="+ch.ID)
}

func (ch Channel) Edit(ctx context.Context, id chat.MessageID, newText string) (chat.MessageID, error) {
	var resp ResponseHeader
	if err := rpc(ctx, ch.client, &resp,
		"chat.update",
		"channel="+ch.ID,
		"ts="+string(id),
		"text="+newText); err != nil {
		return "", err
	}
	return id, nil
}

func (ch Channel) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	msg, err := ch.Send(ctx, ">"+replyTo.Text)
	if err != nil {
		return msg, err
	}
	return ch.Send(ctx, text)
}

func (ch Channel) ReplyAs(ctx context.Context, sendAs chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	msg, err := ch.SendAs(ctx, sendAs, ">"+replyTo.Text)
	if err != nil {
		return msg, err
	}
	return ch.SendAs(ctx, sendAs, text)
}
