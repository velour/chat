package irc

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/velour/chat"
)

type channel struct {
	client *Client
	name   string

	// InWho simulates an infinite buffered channel
	// of strings from the Client to this channel.
	// Each string is the nick of a user in this channel.
	//
	// Upon creation, the channel issues a WHO request.
	// It then proceeds to read nicks from this channel until closed.
	// At that point, all users in the channel are known,
	// and the channel goes into normal operation.
	inWho chan []string

	// In simulates an infinite buffered channel
	// of events from the Client to this channel.
	// The Client publishes events without blocking.
	in chan []interface{}

	// Out publishes events to the Receive method.
	// If the in channel is closed, out is closed.
	// after all pending events have been Received.
	out chan interface{}

	sync.Mutex
	// Users is the set of all users in this channel.
	// To prevent races, the Client updates this map
	// upon receiving a NICK, QUIT, or PART.
	users map[string]bool
}

func newChannel(client *Client, name string) *channel {
	ch := &channel{
		client: client,
		name:   name,
		inWho:  make(chan []string, 1),
		in:     make(chan []interface{}, 1),
		out:    make(chan interface{}),
		users:  make(map[string]bool),
	}
	go func() {
		for ns := range ch.inWho {
			for _, n := range ns {
				ch.Lock()
				ch.users[n] = true
				ch.Unlock()
			}
		}
		for es := range ch.in {
			for _, e := range es {
				ch.out <- e
			}
		}
		close(ch.out)
	}()
	return ch
}

func (ch *channel) Name() string        { return ch.name }
func (ch *channel) ServiceName() string { return "IRC (" + ch.client.server + ")" }

func (ch *channel) Receive(ctx context.Context) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev, ok := <-ch.out:
		if !ok {
			return nil, io.EOF
		}
		return ev, nil
	}
}

// send sends a message to the channel.
// linePrefix is prepended to each line after any prefix indicating the sendAs user.
func (ch *channel) send(ctx context.Context, sendAs *chat.User, linePrefix, text string) (chat.Message, error) {
	const mePrefix = "/me "
	var prefix, suffix string
	if sendAs != nil {
		if strings.HasPrefix(text, mePrefix) {
			text = strings.TrimPrefix(text, mePrefix)
			prefix = "*" + sendAs.Name() + " "
		} else {
			prefix = "<" + sendAs.Name() + "> "
		}
	} else if strings.HasPrefix(text, mePrefix) {
		text = strings.TrimPrefix(text, mePrefix)
		prefix = actionPrefix + " "
		suffix = actionSuffix
	}
	for _, t := range strings.Split(text, "\n") {
		t = prefix + linePrefix + t + suffix
		if err := send(ctx, ch.client, PRIVMSG, ch.name, t); err != nil {
			return chat.Message{}, err
		}
	}
	msg := chat.Message{ID: chat.MessageID(text), Text: text}
	if sendAs == nil {
		ch.client.Lock()
		msg.From = chatUser(ch.client.nick)
		ch.client.Unlock()
	} else {
		msg.From = *sendAs
	}
	return msg, nil
}

func (ch *channel) Send(ctx context.Context, text string) (chat.Message, error) {
	return ch.send(ctx, nil, "", text)
}

func (ch *channel) SendAs(ctx context.Context, sendAs chat.User, text string) (chat.Message, error) {
	return ch.send(ctx, &sendAs, "", text)
}

// Delete is a no-op for IRC.
func (ch *channel) Delete(context.Context, chat.MessageID) error { return nil }

// Edit is a no-op for IRC, it simply returns the given MessageID.
func (c *channel) Edit(_ context.Context, id chat.MessageID, _ string) (chat.MessageID, error) {
	return id, nil
}

func (ch *channel) reply(ctx context.Context, sendAs *chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	quote := replyTo.From.Name() + "> "
	if _, err := ch.send(ctx, sendAs, quote, replyTo.Text); err != nil {
		return chat.Message{}, nil
	}
	msg, err := ch.send(ctx, sendAs, "", text)
	if err != nil {
		return chat.Message{}, nil
	}
	return msg, nil
}

func (ch *channel) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.reply(ctx, nil, replyTo, text)
}

func (ch *channel) ReplyAs(ctx context.Context, sendAs chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.reply(ctx, &sendAs, replyTo, text)
}
