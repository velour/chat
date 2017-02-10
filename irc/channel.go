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

	// InOrigin receives the server's origin string,
	// sent when receiving the JOIN message for this channel.
	inOrigin chan string

	// The origin sent by the server for my messages.
	// This is user to determin the server's PRIVMSG header size,
	// in order to truncate long PRIVMSGs such that the server's
	// relayed versions of the will not be too long.
	myOrigin   string
	originLock sync.Mutex

	// In simulates an infinite buffered channel
	// of events from the Client to this channel.
	// The Client publishes events without blocking.
	// To maintain order, only one goroutine can publish to in at a time.
	in chan []chat.Event

	// Out publishes events to the Receive method.
	// If the in channel is closed, out is closed.
	// after all pending events have been Received.
	out chan chat.Event

	mu sync.Mutex
	// Users is the set of all users in this channel.
	// To prevent races, the Client updates this map
	// upon receiving a NICK, QUIT, or PART.
	users map[string]bool
}

func newChannel(client *Client, name string) *channel {
	ch := &channel{
		client:   client,
		name:     name,
		inWho:    make(chan []string, 1),
		inOrigin: make(chan string, 1),
		in:       make(chan []chat.Event, 1),
		out:      make(chan chat.Event),
		users:    make(map[string]bool),
	}

	// Block all channel send operations until we have the origin.
	ch.originLock.Lock()

	go func() {
		ch.myOrigin = <-ch.inOrigin
		ch.originLock.Unlock()

		for ns := range ch.inWho {
			for _, n := range ns {
				ch.mu.Lock()
				ch.users[n] = true
				ch.mu.Unlock()
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

// sendEvent, sends an event to the channel.
// The caller must already hold the channel's lock.
func sendEvent(ch *channel, event chat.Event) {
	select {
	case ch.in <- []chat.Event{event}:
	case es := <-ch.in:
		ch.in <- append(es, event)
	}
}

func (ch *channel) PrettyPrint() string {
	return "\"" + ch.Name() + " at " + ch.ServiceName() + "\""
}

func (ch *channel) Name() string        { return ch.name }
func (ch *channel) ServiceName() string { return "IRC (" + ch.client.server + ")" }

func (ch *channel) Receive(ctx context.Context) (chat.Event, error) {
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

// splitPRIVMSG returns text, split such that each split begins with prefix,
// ends with suffix, contains no newlines, and is of no more than MaxBytes.
func splitPRIVMSG(origin, channelName, prefix, suffix, text string) []string {
	header := Message{Origin: origin, Command: PRIVMSG, Arguments: []string{channelName, ""}}
	maxTextSize := MaxBytes - (len(header.Bytes()) + len(prefix) + len(suffix))

	var texts []string
	for _, line := range strings.Split(text, "\n") {
		for len(line) > maxTextSize {
			texts = append(texts, prefix+line[:maxTextSize]+suffix)
			line = line[maxTextSize:]
		}
		if len(line) > 0 {
			texts = append(texts, prefix+line+suffix)
		}
	}
	return texts
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
	ch.originLock.Lock()
	origin := ch.myOrigin
	ch.originLock.Unlock()
	texts := splitPRIVMSG(origin, ch.name, prefix+linePrefix, suffix, text)
	if err := sendPRIVMSGBatch(ctx, ch.client, ch.name, texts...); err != nil {
		return chat.Message{}, err
	}
	msg := chat.Message{ID: chat.MessageID(text), Text: text}
	if sendAs == nil {
		ch.client.Lock()
		msg.From = chatUser(ch, ch.client.nick)
		ch.client.Unlock()
	} else {
		msg.From = sendAs
	}
	return msg, nil
}

func (ch *channel) Send(ctx context.Context, msg chat.Message) (chat.Message, error) {
	if msg.ReplyTo != nil {
		if msg.ReplyTo.From == nil {
			ch.client.Lock()
			msg.ReplyTo.From = chatUser(ch, ch.client.nick)
			ch.client.Unlock()
		}
		quote := "<" + msg.ReplyTo.From.Name() + "> "
		if _, err := ch.send(ctx, msg.From, quote, msg.ReplyTo.Text); err != nil {
			return chat.Message{}, nil
		}
	}
	msg, err := ch.send(ctx, msg.From, "", msg.Text)
	if err != nil {
		return chat.Message{}, nil
	}
	return msg, nil
}

// Delete is a no-op for IRC.
func (ch *channel) Delete(context.Context, chat.Message) error { return nil }

// Edit is a no-op for IRC, it simply returns the given Message.
func (ch *channel) Edit(_ context.Context, msg chat.Message) (chat.Message, error) {
	return msg, nil
}
