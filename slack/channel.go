package slack

import (
	"context"
	"errors"
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

	// replies is a map containing all received, unhandled replies, keyed by ID.
	// In Slack, replies come across as two events:
	// First the reply message with subtype="" and with thread_id=<ReplyTo ID>,
	// then second, the reply-to message, with subtype="message_replied".
	// We save the first here, attach it to the next message_replied subtype
	// with the matching thread_id, and remove it from this list.
	replies map[string]*chat.Message
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
	ch.replies = make(map[string]*chat.Message)
	go func() {
		for us := range ch.in {
			for _, u := range us {
				ch.out <- u
			}
		}
		close(ch.out)
	}()
}

func (ch *channel) PrettyPrint() string {
	return "\"" + ch.Name() + " at " + ch.ServiceName() + "\""
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

// getUserByID returns a chat.User of a userID for a user in this Channel.
func getUserByID(ctx context.Context, ch *channel, id chat.UserID) (*chat.User, error) {
	ch.client.Lock()
	defer ch.client.Unlock()

	u, ok := ch.client.users[id]
	if !ok {
		var resp struct {
			ResponseHeader
			User User `json:"user"`
		}
		if err := rpc(ctx, ch.client, &resp, "users.info", "user="+string(id)); err != nil {
			return nil, err
		}
		u = chatUser(&resp.User)
		ch.client.users[id] = u
	}

	u.Channel = ch
	return &u, nil
}

// getUserByNick looks up a user by their nick.
func getUserByNick(ch *channel, nick string) (*chat.User, error) {
	ch.client.Lock()
	defer ch.client.Unlock()

	for _, u := range ch.client.users {
		if u.Nick == nick {
			u.Channel = ch
			return &u, nil
		}
	}
	return nil, errors.New("nick not found: " + nick)
}

// chatEvent returns the chat event corresponding to the update.
// If the Update cannot be mapped, nil is returned with a nil error.
// This signifies an Update that sholud be ignored.
func (ch *channel) chatEvent(ctx context.Context, u *Update) (chat.Event, error) {
	var myURL string
	ch.client.Lock()
	if ch.client.localURL != nil {
		myURL = ch.client.localURL.String()
	}
	ch.client.Unlock()

	switch {
	case u.Type == "message":
		switch {
		case len(u.Attachments) > 0 && u.Attachments[0].ImageURL != "":
			attachment := u.Attachments[0]
			var user *chat.User
			if u.User != "" {
				var err error
				if user, err = getUserByID(ctx, ch, u.User); err != nil {
					return nil, err
				}
			} else {
				// AuthorName isn't guaranteed to be a nick.
				// Ignore any error, and defer to the the fallback case.
				user, _ = getUserByNick(ch, attachment.AuthorName)
			}
			var text string
			if user == nil {
				text = attachment.AuthorName + " sent an image: "
			} else {
				text = "/me sent an image: "
			}
			if attachment.Title != "" {
				text += attachment.Title + " — "
			}
			text += attachment.ImageURL
			if attachment.Footer != "" {
				text += "\n" + attachment.Footer
			}
			id := chat.MessageID(u.Ts)
			return chat.Message{ID: id, From: user, Text: text}, nil

		case u.SubType == "" || u.SubType == "me_message":
			if u.User == "" || u.Text == "" {
				return nil, nil
			}
			if u.ThreadTS != "" {
				// This is the first message in a pair of reply events—save it.
				msg, err := chatMessage(ctx, ch, u)
				if err != nil {
					return nil, err
				}
				log.Printf("stashing reply %s", u.Ts)
				ch.replies[u.Ts] = msg
				return nil, nil
			}
			if msg, err := chatMessage(ctx, ch, u); err != nil {
				return nil, err
			} else {
				return *msg, nil
			}

		case u.SubType == "message_replied" && u.Message != nil:
			log.Printf("handling a reply")
			n := len(u.Message.Replies)
			if n == 0 {
				log.Printf("no replies array")
				return nil, nil
			}
			ts := u.Message.Replies[n-1].TS

			reply, ok := ch.replies[ts]
			if !ok {
				log.Printf("reply %s not found", ts)
				return nil, nil
			}
			delete(ch.replies, ts)

			if u.Message.User == "" {
				log.Printf("no message user")
				return nil, nil
			}
			replyTo, err := chatMessage(ctx, ch, u.Message)
			if err != nil {
				return nil, err
			}
			reply.ReplyTo = replyTo

			return *reply, nil

		case u.SubType == "message_changed" && u.Message != nil:
			if u.Message.User == "" {
				return nil, nil
			}
			msg, err := chatMessage(ctx, ch, u.Message)
			if err != nil {
				return nil, err
			}
			return chat.Edit{OrigID: chat.MessageID(u.Message.Ts), New: *msg}, nil

		case u.SubType == "message_deleted":
			return chat.Delete{ID: chat.MessageID(u.DeletedTS), Channel: ch}, nil

		case u.SubType == "file_share" && u.File != nil && myURL != "":
			if u.User == "" {
				return nil, nil
			}
			user, err := getUserByID(ctx, ch, u.User)
			if err != nil {
				return nil, err
			}
			fileURL, err := url.Parse(myURL)
			if err != nil {
				panic(err)
			}
			fileURL.Path = path.Join(fileURL.Path, u.File.ID)
			text := "/me shared a file: " + fileURL.String()
			id := chat.MessageID(u.Ts)
			return chat.Message{ID: id, From: user, Text: text}, nil
		}
	}
	return nil, nil
}

func chatMessage(ctx context.Context, ch *channel, u *Update) (*chat.Message, error) {
	user, err := getUserByID(ctx, ch, u.User)
	if err != nil {
		return nil, err
	}
	findUser := func(id string) (string, bool) {
		u, err := getUserByID(ctx, ch, chat.UserID(id))
		if err != nil {
			log.Printf("Failed to lookup mention user %s: %s\n", id, err)
			return "", false
		}
		return u.Name(), true
	}
	msg := &chat.Message{
		ID:   chat.MessageID(u.Ts),
		From: user,
		Text: fixText(findUser, html.UnescapeString(u.Text)),
	}
	if u.SubType == "me_message" {
		msg.Text = "/me " + msg.Text
	}
	return msg, nil
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
	msg := chat.Message{ID: id, From: sendAs, Text: text}
	return msg, nil
}

func (ch *channel) Send(ctx context.Context, msg chat.Message) (chat.Message, error) {
	if msg.ReplyTo != nil {
		if msg.ReplyTo.From == nil {
			me := chatUser(ch.client.me)
			msg.ReplyTo.From = &me
		}
		txt := "_" + msg.ReplyTo.From.Name() + " said_:\n>" + msg.ReplyTo.Text
		if _, err := ch.send(ctx, msg.From, txt); err != nil {
			return chat.Message{}, err
		}
	}
	return ch.send(ctx, msg.From, msg.Text)
}

func (ch *channel) Delete(ctx context.Context, msg chat.Message) error {
	var resp ResponseHeader
	err := rpc(ctx, ch.client, &resp,
		"chat.delete",
		"ts="+string(msg.ID),
		"channel="+ch.ID)
	if err != nil {
		if rpcErr, ok := err.(rpcErr); ok && rpcErr.httpStatus == 404 {
			return nil
		}
		return err
	}
	return nil
}

func (ch *channel) Edit(ctx context.Context, msg chat.Message) (chat.Message, error) {
	var resp struct {
		ResponseHeader
		TS chat.MessageID `json:"ts"`
	}
	err := rpc(ctx, ch.client, &resp,
		"chat.update",
		"channel="+ch.ID,
		"ts="+string(msg.ID),
		"text="+msg.Text)
	if err != nil {
		if rpcErr, ok := err.(rpcErr); ok && rpcErr.httpStatus == 404 {
			return msg, nil
		}
		return chat.Message{}, err
	}
	msg.ID = resp.TS
	return msg, nil
}
