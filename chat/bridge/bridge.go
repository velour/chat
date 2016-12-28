// Package bridge is a chat.Channel that acts as a relay,
// bridging multiple chat.Channels into a single, logical one.
//
// A Bridge is created with a slice of other chat.Channels, called the bridged channels.
// Events sent on a bridged channel are relayed to all other channels
// and are also returned by the Bridge.Receive method.
//
// The send-style methods of chat.Channel (Send, Delete, Edit, and so on)
// are forwarded to all bridged channels.
// In this way, the Bridge itself is a chat.Channel.
// This is useful, for example, to implement a chat bot
// that also bridges channels on multiple chat clients.
package bridge

import (
	"context"
	"strconv"
	"sync"

	"github.com/velour/bridge/chat"
)

var _ chat.Channel = &Bridge{}

// A Bridge is a chat.Channel that bridges multiple channels.
// Events sent on each bridged channel are:
// 1) relayed to all other channels in the bridge, and
// 2) multiplexed to the Bridge.Receive method.
type Bridge struct {
	// eventsMux multiplexes events incoming from the bridged channels.
	eventsMux chan event

	// recvIn simulates an infinite buffered channel
	// of events, multiplexed from the bridged channels.
	// The mux goroutine publishes events to this channel without blocking.
	// The recv goroutine forwards the events to recvOut.
	recvIn chan []interface{}

	// recvOut publishes evetns to the Receive method.
	recvOut chan interface{}

	// pollError reports errors from the channel polling goroutines to the mux goroutine.
	// If the mux goroutine recieves a pollError, it forwards the error to closeError,
	// cancels all background goroutines, and returns.
	pollError chan error

	// closeError reports errors to the Close method.
	// The mux goroutine publishes to this channel, either forwarding an error
	// from pollError or by simply closing it without an error on successful Close.
	closeError chan error

	// closed is closed when the Close method is called.
	// On a clean close, this signals the mux goroutine to
	// cancel all background goroutines and close closeError.
	closed chan struct{}

	// channels are the channels being bridged.
	channels []chat.Channel

	sync.Mutex

	// nextID is the next ID for messages sent by the bridge.
	nextID int
}

// New returns a new bridge that bridges a set of channels.
func New(channels ...chat.Channel) *Bridge {
	b := &Bridge{
		eventsMux:  make(chan event, 100),
		recvIn:     make(chan []interface{}, 1),
		recvOut:    make(chan interface{}),
		pollError:  make(chan error, 1),
		closeError: make(chan error),
		closed:     make(chan struct{}),
		channels:   channels,
	}

	// Polling goroutines run in the background;
	// they are cancelled when the done channel is closed.
	ctx, cancel := context.WithCancel(context.Background())
	for _, ch := range channels {
		go poll(ctx, b, ch)
	}
	go recv(ctx, b)
	go mux(ctx, cancel, b)
	return b
}

// Closes stops bridging the channels, closes the bridge.
func (b *Bridge) Close(context.Context) error {
	close(b.closed)
	return <-b.closeError
}

type event struct {
	from chat.Channel
	what interface{}
}

// mux multiplexes:
// events incoming from bridged channels,
// errors coming from channel polling,
// and closing the bridge.
func mux(ctx context.Context, cancel context.CancelFunc, b *Bridge) {
	defer cancel()
	defer close(b.closeError)
	for {
		select {
		case <-b.closed:
			return
		case err := <-b.pollError:
			b.closeError <- err
			return
		case ev := <-b.eventsMux:
			if err := relay(ctx, ev, b.channels); err != nil {
				b.closeError <- err
				return
			}
			select {
			case b.recvIn <- []interface{}{ev.what}:
			case evs := <-b.recvIn:
				b.recvIn <- append(evs, ev.what)
			}
		}
	}
}

// recv forwards events to the Receive method.
// If the context is canceled, unreceived events are dropped.
func recv(ctx context.Context, b *Bridge) {
	defer close(b.recvOut)
	for {
		select {
		case <-ctx.Done():
			return
		case evs := <-b.recvIn:
			for _, ev := range evs {
				select {
				case <-ctx.Done():
					return
				case b.recvOut <- ev:
				}
			}
		}
	}

}

func poll(ctx context.Context, b *Bridge, ch chat.Channel) {
	for {
		switch ev, err := ch.Receive(ctx); {
		case err == context.Canceled || err == context.DeadlineExceeded:
			// Ignore context errors. These are expected. No need to report back.
			return
		case err != nil:
			// Don't block. We only report the first error.
			select {
			case b.pollError <- err:
			default:
			}
			return
		default:
			b.eventsMux <- event{from: ch, what: ev}
		}
	}
}

func relay(ctx context.Context, ev event, channels []chat.Channel) error {
	for _, ch := range channels {
		if ch == ev.from {
			continue
		}
		switch ev := ev.what.(type) {
		case chat.Message:
			if _, err := ch.SendAs(ctx, ev.From, ev.Text); err != nil {
				return err
			}
		case chat.Delete:
			// TODO
		case chat.Edit:
			// TODO
		case chat.Reply:
			// TODO
		case chat.Join:
			// TODO
		case chat.Leave:
			// TODO
		case chat.Rename:
			// TODO
		}
	}
	return nil
}

// Receive returns the next event from any of the bridged channels.
func (b *Bridge) Receive(ctx context.Context) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev := <-b.recvOut:
		return ev, nil
	}
}

func me(b *Bridge) chat.User {
	// TODO: use a more informative bridge User.
	// Option: get the User info from channels[0].
	return chat.User{
		ID:   chat.UserID("bridge"),
		Nick: "bridge",
		Name: "bridge",
	}
}

func nextID(b *Bridge) chat.MessageID {
	b.Lock()
	defer b.Unlock()
	b.nextID++
	return chat.MessageID(strconv.Itoa(b.nextID - 1))
}

// Send sends text to all channels on the Bridge.
func (b *Bridge) Send(ctx context.Context, text string) (chat.Message, error) {
	for _, ch := range b.channels {
		if _, err := ch.Send(ctx, text); err != nil {
			return chat.Message{}, err
		}
	}
	msg := chat.Message{ID: nextID(b), From: me(b), Text: text}
	return msg, nil
}

// SendAs sends text on behalf of a given user to all channels on the Bridge.
func (b *Bridge) SendAs(ctx context.Context, sendAs chat.User, text string) (chat.Message, error) {
	for _, ch := range b.channels {
		if _, err := ch.SendAs(ctx, sendAs, text); err != nil {
			return chat.Message{}, err
		}
	}
	msg := chat.Message{ID: nextID(b), From: sendAs, Text: text}
	return msg, nil
}

// Delete is a no-op for Bridge.
func (b *Bridge) Delete(context.Context, chat.MessageID) error { return nil }

// Edit is a no-op fro Bridge; it simply returns the given MessageID.
func (b *Bridge) Edit(_ context.Context, id chat.MessageID, _ string) (chat.MessageID, error) {
	return id, nil
}

// Reply is equivalent to Send for a Bridge.
func (b *Bridge) Reply(ctx context.Context, _ chat.Message, text string) (chat.Message, error) {
	return b.Send(ctx, text)
}

// ReplyAs is equivalent to SendAs for a Bridge.
func (b *Bridge) ReplyAs(ctx context.Context, user chat.User, _ chat.Message, text string) (chat.Message, error) {
	return b.SendAs(ctx, user, text)
}
