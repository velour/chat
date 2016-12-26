package irc

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/velour/bridge/chat"
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

func (ch *channel) Receive(ctx context.Context) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev, ok := <-ch.out:
		if !ok {
			return nil, io.EOF
		}
		ch.Lock()
		log.Printf("%s users %+v\n", ch.name, ch.users)
		ch.Unlock()
		return ev, nil
	}
}

func (ch *channel) Send(ctx context.Context, text string) (chat.Message, error) {
	// IRC doesn't support newlines in messages.
	// Send a separate message for each line.
	for _, t := range strings.Split(text, "\n") {
		// TODO: split the message if it was too long.
		if strings.HasPrefix(t, "/me") {
			// If the string begins with /me, convert it to a CTCP action.
			t = strings.TrimPrefix(t, "/me")
			t = actionPrefix + " " + strings.TrimSpace(t) + actionSuffix
		}
		log.Println(t)
		if err := send(ctx, ch.client, PRIVMSG, ch.name, t); err != nil {
			return chat.Message{}, err
		}
	}
	ch.client.Lock()
	nick := ch.client.nick
	ch.client.Unlock()
	msg := chat.Message{ID: chat.MessageID(text), From: chatUser(nick), Text: text}
	return msg, nil
}

// Delete is a no-op for IRC.
func (ch *channel) Delete(context.Context, chat.MessageID) error { return nil }

// Edit is a no-op for IRC, it simply returns the given MessageID.
func (c *channel) Edit(_ context.Context, id chat.MessageID, _ string) (chat.MessageID, error) {
	return id, nil
}

// Reply is equivalent to Send for IRC.
//
// TODO: quote the replyTo message and add the reply text after it.
func (ch *channel) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	return ch.Send(ctx, text)
}
