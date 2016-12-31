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
	"io"
	"log"
	"strconv"
	"sync"

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

	// log is a history of messages sent with or relayed by the bridge.
	log []*logEntry
}

type logEntry struct {
	origin chat.Channel
	copies []message
}

type message struct {
	to  chat.Channel
	msg chat.Message
}

// New returns a new bridge that bridges a set of channels.
func New(channels ...chat.Channel) *Bridge {
	b := &Bridge{
		eventsMux:  make(chan event, 100),
		recvIn:     make(chan []interface{}, 1),
		recvOut:    make(chan interface{}),
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

// Closes stops bridging the channels, closes the bridge.
func (b *Bridge) Close(context.Context) error {
	close(b.closed)
	return <-b.closeError
}

type event struct {
	origin chat.Channel
	what   interface{}
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
			b.eventsMux <- event{origin: ch, what: ev}
		}
	}
}

func logMessage(b *Bridge, entry *logEntry) {
	b.Lock()
	b.log = append(b.log, entry)
	if len(b.log) > maxHistory {
		b.log = b.log[:maxHistory]
	}
	b.Unlock()
}

func relay(ctx context.Context, b *Bridge, event event) error {
	switch ev := event.what.(type) {
	case chat.Message:
		entry, err := send(ctx, b, event.origin, &ev.From, ev.Text)
		if err != nil {
			return err
		}
		entry.copies = append(entry.copies, message{to: event.origin, msg: ev})
		return nil

	case chat.Delete:
		editLog := findLogEntry(b, event.origin, ev.ID)
		for _, ch := range b.channels {
			if ch == event.origin {
				continue
			}
			msg := findCopy(editLog, ch)
			if msg == nil {
				log.Printf("edited message: not found\n")
				continue
			}
			var err error
			if err = ch.Delete(ctx, msg.ID); err != nil {
				return err
			}
		}

	case chat.Edit:
		editLog := findLogEntry(b, event.origin, ev.ID)
		for _, ch := range b.channels {
			if ch == event.origin {
				continue
			}
			msg := findCopy(editLog, ch)
			if msg == nil {
				log.Printf("edited message: not found\n")
				continue
			}
			var err error
			if msg.ID, err = ch.Edit(ctx, msg.ID, ev.Text); err != nil {
				return err
			}
		}

	case chat.Reply:
		entry, err := reply(ctx, b, event.origin, &ev.Reply.From, ev.ReplyTo, ev.Reply.Text)
		if err != nil {
			return err
		}
		entry.copies = append(entry.copies, message{to: event.origin, msg: ev.Reply})
		return nil

	case chat.Join:
		msg := ev.Who.Name() + " joined"
		for _, ch := range b.channels {
			if ch == event.origin {
				continue
			}
			if _, err := ch.Send(ctx, msg); err != nil {
				log.Printf("Failed to send join message to %s: %s\n", ch, err)
			}
		}
	case chat.Leave:
		msg := ev.Who.Name() + " left"
		for _, ch := range b.channels {
			if ch == event.origin {
				continue
			}
			if _, err := ch.Send(ctx, msg); err != nil {
				log.Printf("Failed to send leave message to %s: %s\n", ch, err)
			}
		}
	case chat.Rename:
		old := ev.From.Name()
		new := ev.To.Name()
		if old == new {
			break
		}
		msg := old + " renamed to " + new
		for _, ch := range b.channels {
			if ch == event.origin {
				continue
			}
			if _, err := ch.Send(ctx, msg); err != nil {
				log.Printf("Failed to send rename message: %s\n", err)
			}
		}
	}
	return nil
}

// Receive returns the next event from any of the bridged channels.
func (b *Bridge) Receive(ctx context.Context) (interface{}, error) {
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

func me(b *Bridge) chat.User {
	// TODO: use a more informative bridge User.
	// Option: get the User info from channels[0].
	return chat.User{
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

// send sends a message to all Channels on the Bridge except the origin channel.
// The sent message is logged in the Bridge history and returned.
func send(ctx context.Context, b *Bridge, origin chat.Channel, sendAs *chat.User, text string) (*logEntry, error) {
	entry := &logEntry{origin: origin}
	for _, ch := range b.channels {
		var err error
		var m chat.Message
		if ch == origin {
			continue
		}
		if sendAs == nil {
			m, err = ch.Send(ctx, text)
		} else {
			m, err = ch.SendAs(ctx, *sendAs, text)
		}
		if err != nil {
			return nil, err
		}
		entry.copies = append(entry.copies, message{to: ch, msg: m})
	}
	logMessage(b, entry)
	return entry, nil
}

func (b *Bridge) Send(ctx context.Context, text string) (chat.Message, error) {
	entry, err := send(ctx, b, b, nil, text)
	if err != nil {
		return chat.Message{}, err
	}
	msg := chat.Message{ID: nextID(b), From: me(b), Text: text}
	entry.copies = append(entry.copies, message{to: b, msg: msg})
	return msg, nil
}

func (b *Bridge) SendAs(ctx context.Context, sendAs chat.User, text string) (chat.Message, error) {
	entry, err := send(ctx, b, b, &sendAs, text)
	if err != nil {
		return chat.Message{}, err
	}
	msg := chat.Message{ID: nextID(b), From: sendAs, Text: text}
	entry.copies = append(entry.copies, message{to: b, msg: msg})
	return msg, nil
}

// Delete is a no-op for Bridge.
func (b *Bridge) Delete(context.Context, chat.MessageID) error { return nil }

// Edit is a no-op fro Bridge; it simply returns the given MessageID.
func (b *Bridge) Edit(_ context.Context, id chat.MessageID, _ string) (chat.MessageID, error) {
	return id, nil
}

func findLogEntry(b *Bridge, origin chat.Channel, id chat.MessageID) *logEntry {
	for _, m := range b.log {
		for _, c := range m.copies {
			if c.to == origin && c.msg.ID == id {
				return m
			}
		}
	}
	return nil
}

func findCopy(log *logEntry, ch chat.Channel) *chat.Message {
	if log == nil {
		return nil
	}
	for _, c := range log.copies {
		if c.to == ch {
			return &c.msg
		}
	}
	return nil
}

// reply replies to a message in all Channels on the Bridge except the origin.
// The replied message is logged to the Bridge history and returned.
// If history is not found for the replyTo message,
// this is treated as a normal send instead of a reply.
func reply(ctx context.Context, b *Bridge, origin chat.Channel, sendAs *chat.User, replyTo chat.Message, text string) (*logEntry, error) {
	entry := &logEntry{origin: origin}
	replyToLog := findLogEntry(b, origin, replyTo.ID)
	for _, ch := range b.channels {
		var err error
		var m chat.Message
		if ch == origin {
			continue
		}
		switch replyToCopy := findCopy(replyToLog, ch); {
		case replyToCopy != nil && sendAs == nil:
			m, err = ch.Reply(ctx, *replyToCopy, text)
		case replyToCopy != nil && sendAs != nil:
			m, err = ch.ReplyAs(ctx, *sendAs, *replyToCopy, text)
		case sendAs == nil:
			m, err = ch.Send(ctx, text)
		case sendAs != nil:
			m, err = ch.SendAs(ctx, *sendAs, text)
		}
		if err != nil {
			return nil, err
		}
		entry.copies = append(entry.copies, message{to: ch, msg: m})
	}
	logMessage(b, entry)
	return entry, nil
}

func (b *Bridge) Reply(ctx context.Context, replyTo chat.Message, text string) (chat.Message, error) {
	entry, err := reply(ctx, b, b, nil, replyTo, text)
	if err != nil {
		return chat.Message{}, err
	}
	msg := chat.Message{ID: nextID(b), From: me(b), Text: text}
	entry.copies = append(entry.copies, message{to: b, msg: msg})
	return msg, nil
}

func (b *Bridge) ReplyAs(ctx context.Context, sendAs chat.User, replyTo chat.Message, text string) (chat.Message, error) {
	entry, err := reply(ctx, b, b, &sendAs, replyTo, text)
	if err != nil {
		return chat.Message{}, err
	}
	msg := chat.Message{ID: nextID(b), From: sendAs, Text: text}
	entry.copies = append(entry.copies, message{to: b, msg: msg})
	return msg, nil
}
