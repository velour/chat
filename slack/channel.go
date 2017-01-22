package slack

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/url"
	"path"
	"strings"

	"github.com/velour/chat"
)

// A channel object describes a slack channel.
type channel struct {
	ID string `json:"id"`

	// ChannelName is the name of the channel WITHOUT a leading #.
	ChannelName string `json:"name"`

	client *Client
	in     chan []*Update
	out    chan *Update
}

// newChannel creates a new channel
func newChannel(c *Client, id, name string) *channel {
	ch := &channel{ID: id, ChannelName: name}
	initChannel(c, ch)
	return ch
}

// initChannel fills in an empty channel's privates
//
// Used when a marshaler has created a Channel
func initChannel(c *Client, ch *channel) {
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
}

func (ch *channel) Name() string        { return ch.ChannelName }
func (ch *channel) ServiceName() string { return ch.client.domain + ".slack.com" }

func (ch *channel) Receive(ctx context.Context) (chat.Event, error) {
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

// getUser returns a chat.User of a userID for a user in this Channel.
func getUser(ctx context.Context, ch *channel, id chat.UserID) (chat.User, error) {
	ch.client.Lock()
	defer ch.client.Unlock()

	u, ok := ch.client.users[id]
	if !ok {
		var resp struct {
			ResponseHeader
			User User `json:"user"`
		}
		if err := rpc(ctx, ch.client, &resp, "users.info", "user="+string(id)); err != nil {
			return chat.User{}, err
		}
		u = chat.User{
			ID:          id,
			Nick:        resp.User.Name,
			FullName:    resp.User.Profile.RealName,
			DisplayName: resp.User.Name,
			PhotoURL:    resp.User.Profile.Image,
		}
		ch.client.users[id] = u
	}

	u.Channel = ch
	return u, nil
}

// chatEvent returns the chat event corresponding to the update.
// If the Update cannot be mapped, nil is returned with a nil error.
// This signifies an Update that sholud be ignored.
func (ch *channel) chatEvent(ctx context.Context, u *Update) (chat.Event, error) {
	if u.User == "" {
		// ignore updates without users.
		return nil, nil
	}
	user, err := getUser(ctx, ch, u.User)
	if err != nil {
		return nil, err
	}

	var myURL string
	ch.client.Lock()
	if ch.client.localURL != nil {
		myURL = ch.client.localURL.String()
	}
	ch.client.Unlock()

	switch {
	case u.Type == "message" && u.SubType == "file_share" && myURL != "":
		fileURL, err := url.Parse(myURL)
		if err != nil {
			panic(err)
		}
		fileURL.Path = path.Join(fileURL.Path, u.File.ID)
		text := "/me shared a file: " + fileURL.String()
		id := chat.MessageID(u.Ts)
		return chat.Message{ID: id, From: user, Text: text}, nil

	case u.Type == "message":
		id := chat.MessageID(u.Ts)
		findUser := func(id string) (string, bool) {
			u, err := getUser(ctx, ch, chat.UserID(id))
			if err != nil {
				log.Printf("Failed to lookup mention user %s: %s\n", id, err)
				return "", false
			}
			return u.Name(), true
		}
		text := fixText(findUser, html.UnescapeString(u.Text))
		return chat.Message{ID: id, From: user, Text: text}, nil
	}
	return nil, nil
}

// Send sends text to the Channel and returns the sent Message.
func (ch *channel) send(ctx context.Context, sendAs *chat.User, text string) (chat.Message, error) {
	// Do not attempt to send empty messages
	// TODO(cws): make bridge just not crash when errors come back from Send/SendAs)
	if text == "" {
		return chat.Message{}, nil
	}

	if strings.HasPrefix(text, "/me ") {
		text = strings.TrimPrefix(text, "/me ")
		// Add a space before the closing _ so if text ends with a URL,
		// Slack doesn't think that the closing _ is really part of the URL.
		text = fmt.Sprintf("_%s _", text)
	}

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

func (ch *channel) Send(ctx context.Context, text string) (chat.Message, error) {
	return ch.send(ctx, nil, text)
}

func (ch *channel) SendAs(ctx context.Context, sendAs chat.User, text string) (chat.Message, error) {
	log.Printf("Sending as: %+v %q\n", sendAs, text)
	return ch.send(ctx, &sendAs, text)
}

func (ch *channel) Delete(ctx context.Context, id chat.MessageID) error {
	var resp ResponseHeader
	return rpc(ctx, ch.client, &resp,
		"chat.delete",
		"ts="+string(id),
		"channel="+ch.ID)
}

func (ch *channel) Edit(ctx context.Context, msg chat.Message) (chat.Message, error) {
	var resp struct {
		ResponseHeader
		TS chat.MessageID `json:"ts"`
	}
	if err := rpc(ctx, ch.client, &resp,
		"chat.update",
		"channel="+ch.ID,
		"ts="+string(msg.ID),
		"text="+msg.Text); err != nil {
		return chat.Message{}, err
	}
	msg.ID = resp.TS
	return msg, nil
}

func (ch *channel) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	msg, err := ch.Send(ctx, ">"+replyTo.Text)
	if err != nil {
		return msg, err
	}
	return ch.Send(ctx, text)
}

func (ch *channel) ReplyAs(ctx context.Context, sendAs chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	msg, err := ch.SendAs(ctx, sendAs, ">"+replyTo.Text)
	if err != nil {
		return msg, err
	}
	return ch.SendAs(ctx, sendAs, text)
}

func (ch *channel) Who(ctx context.Context) ([]chat.User, error) {
	var resp struct {
		ResponseHeader
		Channel struct {
			Members []chat.UserID `json:"members"`
		} `json:"channel"`
	}
	if err := rpc(ctx, ch.client, &resp, "channels.info", "channel="+ch.ID); err != nil {
		return nil, err
	}
	var users []chat.User
	for _, id := range resp.Channel.Members {
		u, err := getUser(ctx, ch, id)
		if err != nil {
			log.Printf("Failed to lookup Who user %s: %s\n", id, err)
			continue
		}
		users = append(users, u)
	}
	return users, nil
}
