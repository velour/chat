package discord

import (
	"context"
	"io"
	"strings"

	"github.com/velour/chat"
)

type Channel struct {
	cl        *Client
	id        string
	name      string
	guildID   string
	guildName string

	// in handles incoming events from the client.
	// It supports non-blocking sends.
	// out handles outgoing events to calls to Recieve.
	// Both are created and plumbed by calling start().
	//
	// Calling stop() will close in,
	// and when all pending events are read,
	// out will be closed.
	in  chan []chat.Event
	out chan chat.Event
}

func start(ch *Channel) {
	ch.in = make(chan []chat.Event, 1)
	ch.out = make(chan chat.Event)
	go func() {
		defer close(ch.out)
		for evs := range ch.in {
			for _, ev := range evs {
				ch.out <- ev
			}
		}
	}()
}

func stop(ch *Channel) {
	close(ch.in)
	go func() {
		for range ch.out {
		}
	}()
}

func (ch *Channel) Name() string { return ch.name }

func (ch *Channel) ServiceName() string {
	return "Discord " + ch.guildName + " " + ch.name
}

func (ch *Channel) Receive(ctx context.Context) (chat.Event, error) {
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

func (ch *Channel) Send(ctx context.Context, m chat.Message) (chat.Message, error) {
	req := struct {
		Content string `json:"content"`
	}{
		Content: content(ch, &m),
	}
	var ev event // Message type
	if err := ch.cl.post(ctx, "channels/"+ch.id+"/messages", req, &ev); err != nil {
		return chat.Message{}, err
	}
	m.ID = chat.MessageID(ev.ID)
	return m, nil
}

func (ch *Channel) Delete(ctx context.Context, m chat.Message) error {
	ch.cl.mu.Lock()
	ch.cl.deletes[string(m.ID)] = true
	ch.cl.mu.Unlock()
	err := ch.cl.del(ctx, "channels/"+ch.id+"/messages/"+string(m.ID))
	if code, ok := err.(httpErr); ok && code == 404 {
		return nil
	}
	return err
}

func (ch *Channel) Edit(ctx context.Context, m chat.Message) (chat.Message, error) {
	req := struct {
		Content string `json:"content"`
	}{
		Content: content(ch, &m),
	}
	var ev event // Message type
	err := ch.cl.patch(ctx, "channels/"+ch.id+"/messages/"+string(m.ID), req, &ev)
	if err != nil {
		if code, ok := err.(httpErr); ok && code == 404 {
			return m, nil
		}
		return chat.Message{}, err
	}
	m.ID = chat.MessageID(ev.ID)
	return m, nil
}

func content(ch *Channel, m *chat.Message) string {
	from, emFrom := from(ch, m.From), emFrom(ch, m.From)

	var replyTo string
	if m.ReplyTo != nil {
		var who string
		if m.ReplyTo.From == nil {
			who = ch.cl.userName
		} else {
			who = m.ReplyTo.From.DisplayName
		}
		replyTo = from + "_" + who + " said_: `" + m.ReplyTo.Text + "`\n"
	}
	var s strings.Builder
	for _, line := range strings.Split(m.Text, "\n") {
		if strings.HasPrefix(line, "/me ") {
			s.WriteString(emFrom)
			s.WriteRune('_')
			s.WriteString(strings.TrimPrefix(line, "/me "))
			s.WriteRune('_')
		} else {
			s.WriteString(from)
			s.WriteString(line)
		}
		s.WriteRune('\n')
	}
	return replyTo + s.String()
}

func from(ch *Channel, u *chat.User) string {
	if u == nil || u.Channel == ch && string(u.ID) == ch.cl.userID {
		return ""
	}
	return "**" + u.DisplayName + "**: "
}

func emFrom(ch *Channel, u *chat.User) string {
	if u == nil || u.Channel == ch && string(u.ID) == ch.cl.userID {
		return ""
	}
	return "_" + u.DisplayName + "_ "
}
