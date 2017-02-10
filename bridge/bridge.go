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
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/golang/sync/errgroup"
	"github.com/velour/chat"
)

const maxHistory = 500

var _ chat.Channel = &Bridge{}

// A Bridge is a chat.Channel that bridges multiple channels.
// Events sent on each bridged channel are:
// 1) relayed to all other channels in the bridge, and
// 2) multiplexed to the Bridge.Receive method.
type Bridge struct {
	// eventsMux multiplexes events incoming from the bridged channels.
	eventsMux chan chat.Event

	// recvIn simulates an infinite buffered channel
	// of events, multiplexed from the bridged channels.
	// The mux goroutine publishes events to this channel without blocking.
	// The recv goroutine forwards the events to recvOut.
	recvIn chan []chat.Event

	// recvOut publishes evetns to the Receive method.
	recvOut chan chat.Event

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

	// log is a history of messages sent with or relayed by the bridge.
	// The lock must be held to access log.
	// However, it's entries are never modified; they can be read without the lock.
	log [][]message
}

type message struct {
	To  chat.Channel
	Msg chat.Message
}

// New returns a new bridge that bridges a set of channels.
func New(channels ...chat.Channel) *Bridge {
	b := &Bridge{
		eventsMux:  make(chan chat.Event, 100),
		recvIn:     make(chan []chat.Event, 1),
		recvOut:    make(chan chat.Event),
		pollError:  make(chan error, 1),
		closeError: make(chan error, 1),
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

func (b *Bridge) Name() string        { return "bridge" }
func (b *Bridge) ServiceName() string { return "bridge" }

// Close stops bridging the channels, closes the bridge.
func (b *Bridge) Close(ctx context.Context) error {
	close(b.closed)
	err := <-b.closeError
	if err == io.EOF {
		err = errors.New("unexpected EOF")
	}
	return err
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
			if err := relay(ctx, b, ev); err != nil {
				b.closeError <- err
				return
			}
			select {
			case b.recvIn <- []chat.Event{ev}:
			case evs := <-b.recvIn:
				b.recvIn <- append(evs, ev)
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
			err = fmt.Errorf("failed to receive from %s on %s: %s\n",
				ch.Name(), ch.ServiceName(), err)
			// Don't block. We only report the first error.
			select {
			case b.pollError <- err:
			default:
			}
			return
		default:
			b.eventsMux <- ev
		}
	}
}

func logMessage(b *Bridge, entry []message) {
	b.Lock()
	b.log = append(b.log, entry)
	if len(b.log) > maxHistory {
		b.log = b.log[1:]
	}
	b.Unlock()
}

func relay(ctx context.Context, b *Bridge, event chat.Event) error {
	origin := event.Origin()
	origName := origin.Name() + " on " + origin.ServiceName()
	switch ev := event.(type) {
	case chat.Message:
		msgs, err := sendMessage(ctx, b, allChannelsExcept(b, origin), &ev)
		if err != nil {
			return err
		}
		msgs = append(msgs, message{To: origin, Msg: ev})
		logMessage(b, msgs)
		return nil

	case chat.Delete:
		findMessage := makeFindMessage(b, origin, ev.ID)
		to := allChannelsExcept(b, origin)
		return deleteMessage(ctx, to, findMessage)

	case chat.Edit:
		findMessage := makeFindMessage(b, origin, ev.OrigID)
		origMsg := findMessage(origin)
		if origMsg == nil {
			// If we didn't find the original message,
			// it's gone off the end of history.
			// This is a no-op.
			return nil
		}
		to := allChannelsExcept(b, origin)
		msgs, err := editMessage(ctx, to, findMessage, ev.New.Text)
		if err != nil {
			return err
		}
		origMsg.ID = ev.New.ID
		msgs = append(msgs, message{To: origin, Msg: *origMsg})
		logMessage(b, msgs)
		return nil

	case chat.Join:
		msg := chat.Message{Text: ev.Who.Name() + " joined " + origName}
		_, err := sendMessage(ctx, b, allChannelsExcept(b, origin), &msg)
		return err

	case chat.Leave:
		msg := chat.Message{Text: ev.Who.Name() + " left " + origName}
		_, err := sendMessage(ctx, b, allChannelsExcept(b, origin), &msg)
		return err

	case chat.Rename:
		old := ev.From.Name()
		new := ev.To.Name()
		if old == new {
			break
		}
		msg := chat.Message{Text: old + " renamed to " + new + " in " + origName}
		_, err := sendMessage(ctx, b, allChannelsExcept(b, origin), &msg)
		return err
	}
	return nil
}

// Receive returns the next event from any of the bridged channels.
func (b *Bridge) Receive(ctx context.Context) (chat.Event, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev, ok := <-b.recvOut:
		if !ok {
			return nil, io.EOF
		}
		return ev, nil
	}
}

func me(b *Bridge) *chat.User {
	// TODO: use a more informative bridge User.
	// Option: get the User info from channels[0].
	return &chat.User{
		ID:          chat.UserID("bridge"),
		Nick:        "bridge",
		FullName:    "bridge",
		DisplayName: "bridge",
	}
}

func nextID(b *Bridge) chat.MessageID {
	b.Lock()
	defer b.Unlock()
	b.nextID++
	return chat.MessageID(strconv.Itoa(b.nextID - 1))
}

func (b *Bridge) Send(ctx context.Context, msg chat.Message) (chat.Message, error) {
	msgs, err := sendMessage(ctx, b, b.channels, &msg)
	if err != nil {
		return chat.Message{}, err
	}
	msg.ID = nextID(b)
	msgs = append(msgs, message{To: b, Msg: msg})
	logMessage(b, msgs)
	return msg, nil
}

// Delete is a no-op for Bridge.
func (b *Bridge) Delete(context.Context, chat.Message) error { return nil }

// Edit is a no-op for Bridge; it simply returns the given Message.
func (b *Bridge) Edit(_ context.Context, msg chat.Message) (chat.Message, error) {
	return msg, nil
}

// sendMessage sends a message to multiple channels,
// returning a slice of the messages.
func sendMessage(ctx context.Context, b *Bridge, channels []chat.Channel, msg *chat.Message) ([]message, error) {
	findReply := func(chat.Channel) *chat.Message { return nil }
	if msg.ReplyTo != nil {
		findReply = makeFindMessage(b, msg.Origin(), msg.ReplyTo.ID)
	}

	// Limit the time we wait for the sends.
	// For example, IRC can block almost indefinitely due to rate-limiting.
	// We don't want to hang the bridge, instead, wait for a short time,
	// and give up on any error returns from whatever didn't finish.
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second))
	defer cancel()

	var group errgroup.Group
	messages := make([]message, len(channels))
	for i, ch := range channels {
		i, ch := i, ch
		group.Go(func() error {
			var err error
			m := *msg
			m.ReplyTo = findReply(ch)
			m, err = ch.Send(ctx, m)
			if err != nil && err != context.DeadlineExceeded {
				return fmt.Errorf("failed to send message to %s on %s: %s\n",
					ch.Name(), ch.ServiceName(), err)
			}
			messages[i] = message{To: ch, Msg: m}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return messages, nil
}

func editMessage(ctx context.Context, channels []chat.Channel, findMessage findMessageFunc, text string) ([]message, error) {
	var group errgroup.Group
	messages := make([]message, len(channels))
	for i, ch := range channels {
		i, ch := i, ch
		group.Go(func() error {
			msg := findMessage(ch)
			if msg == nil {
				return nil
			}
			if msg.Text == text {
				// Don't call ch.Edit if the text hasn't changed.
				// Telegram considers this an error.
				// However, Slack generates such events.
				// It's important not to drop them at the Slack level,
				// because the bridge still needs to update the message ID.
				return nil
			}
			m := *msg
			m.Text = text
			newMsg, err := ch.Edit(ctx, m)
			if err != nil {
				return fmt.Errorf("failed to send edit to %s on %s: %s",
					ch.Name(), ch.ServiceName(), err)
			}
			messages[i] = message{To: ch, Msg: newMsg}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return messages, nil
}

func deleteMessage(ctx context.Context, channels []chat.Channel, findMessage findMessageFunc) error {
	var group errgroup.Group
	for _, ch := range channels {
		ch := ch
		group.Go(func() error {
			msg := findMessage(ch)
			if msg == nil {
				return nil
			}
			if err := ch.Delete(ctx, *msg); err != nil {
				return fmt.Errorf("failed to send delete to %s on %s: %s",
					ch.Name(), ch.ServiceName(), err)
			}
			return nil
		})
	}
	return group.Wait()
}

func allChannelsExcept(b *Bridge, exclude chat.Channel) []chat.Channel {
	var channels []chat.Channel
	for _, ch := range b.channels {
		if ch != exclude {
			channels = append(channels, ch)
		}
	}
	return channels
}

type findMessageFunc func(chat.Channel) *chat.Message

func makeFindMessage(b *Bridge, origin chat.Channel, id chat.MessageID) findMessageFunc {
	b.Lock()
	var entry []message
outter:
	for _, e := range b.log {
		for _, c := range e {
			if c.To == origin && c.Msg.ID == id {
				entry = e
				break outter
			}
		}
	}
	b.Unlock()
	return func(ch chat.Channel) *chat.Message {
		if entry == nil {
			return nil
		}
		for _, c := range entry {
			if c.To == ch {
				return &c.Msg
			}
		}
		return nil
	}
}
