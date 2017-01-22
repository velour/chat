package irc

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/velour/chat"
)

const (
	actionPrefix = "\x01ACTION"
	actionSuffix = "\x01"
)

var _ chat.Client = &Client{}

// A Client is a client's connection to an IRC server.
type Client struct {
	server string
	conn   net.Conn
	in     *bufio.Reader
	out    chan outMessage
	error  chan error

	sync.Mutex
	nick     string
	channels map[string]*channel
}

// Dial connects to a remote IRC server.
func Dial(ctx context.Context, server, nick, fullname, pass string) (*Client, error) {
	var dialer net.Dialer
	c, err := dialer.DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, err
	}
	return dial(ctx, c, server, nick, fullname, pass)
}

// DialSSL connects to a remote IRC server using SSL.
func DialSSL(ctx context.Context, server, nick, fullname, pass string, trust bool) (*Client, error) {
	var dialer net.Dialer
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}
	config := tls.Config{InsecureSkipVerify: trust}
	c, err := tls.DialWithDialer(&dialer, "tcp", server, &config)
	if err != nil {
		return nil, err
	}
	return dial(ctx, c, server, nick, fullname, pass)
}

func dial(ctx context.Context, conn net.Conn, server, nick, fullname, pass string) (*Client, error) {
	c := &Client{
		server:   server,
		conn:     conn,
		in:       bufio.NewReader(conn),
		out:      make(chan outMessage),
		error:    make(chan error),
		nick:     nick,
		channels: make(map[string]*channel),
	}
	go limitSends(c)
	if err := register(ctx, c, nick, fullname, pass); err != nil {
		close(c.out)
		return nil, err
	}
	go poll(c)
	return c, nil
}

func register(ctx context.Context, c *Client, nick, fullname, pass string) error {
	if pass != "" {
		if err := send(ctx, c, PASS, pass); err != nil {
			return err
		}
	}
	if err := send(ctx, c, NICK, nick); err != nil {
		return err
	}
	if err := send(ctx, c, USER, nick, "0", "*", fullname); err != nil {
		return err
	}
	for {
		msg, err := next(ctx, c)
		if err != nil {
			return err
		}
		switch msg.Command {
		case ERR_NONICKNAMEGIVEN, ERR_ERRONEUSNICKNAME,
			ERR_NICKNAMEINUSE, ERR_NICKCOLLISION,
			ERR_UNAVAILRESOURCE, ERR_RESTRICTED,
			ERR_NEEDMOREPARAMS, ERR_ALREADYREGISTRED:
			if len(msg.Arguments) > 0 {
				return errors.New(msg.Arguments[len(msg.Arguments)-1])
			}
			return errors.New(CommandNames[msg.Command])

		case RPL_WELCOME:
			return nil

		default:
			/* ignore */
		}
	}
}

// Close closes the connection.
func (c *Client) Close(ctx context.Context) error {
	send(ctx, c, QUIT)
	closeErr := c.conn.Close()
	pollErr := <-c.error
	for _, ch := range c.channels {
		close(ch.in)
	}
	close(c.out)
	if closeErr != nil {
		return closeErr
	}
	return pollErr
}

type outMessage struct {
	msgs [][]byte
	err  chan<- error
}

// limitSends rate limits messages sent to the IRC server.
// It implements the algorithm described in RFC 1459 Section 8.10.
func limitSends(c *Client) {
	var t time.Time
	for send := range c.out {
		var err error
		for _, msg := range send.msgs {
			now := time.Now()
			if t.Before(now) {
				t = now
			}
			if t.After(now.Add(10 * time.Second)) {
				time.Sleep(t.Sub(now))
			}
			t = t.Add(2 * time.Second)
			if _, err = c.conn.Write(msg); err != nil {
				break
			}
		}
		send.err <- err
	}
}

// send sends a single message to the server.
func send(ctx context.Context, c *Client, cmd string, args ...string) error {
	msg := Message{Command: cmd, Arguments: args}
	bs := msg.Bytes()
	if len(bs) > MaxBytes {
		return TooLongError{Message: bs[:MaxBytes], NTrunc: len(bs) - MaxBytes}
	}
	err := make(chan error, 1)
	go func() { c.out <- outMessage{msgs: [][]byte{bs}, err: err} }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-err:
		return err
	}
}

// sendPRIVMSGBatch sends a batch of PRIVMSGs to the same channel,
// together without any intervening send.
// This is useful for sending a multi-line message split across multiple IRC messages.
// Because the messages are published in a batch, they will all send,
// even if this call is abandoned due to context cancellation.
func sendPRIVMSGBatch(ctx context.Context, c *Client, channel string, texts ...string) error {
	var msgs [][]byte
	for _, txt := range texts {
		msg := Message{Command: PRIVMSG, Arguments: []string{channel, txt}}
		bs := msg.Bytes()
		if len(bs) > MaxBytes {
			return TooLongError{Message: bs[:MaxBytes], NTrunc: len(bs) - MaxBytes}
		}
		msgs = append(msgs, bs)
	}
	err := make(chan error, 1)
	go func() { c.out <- outMessage{msgs: msgs, err: err} }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-err:
		return err
	}
}

// next returns the next message from the server.
// It never returns a PING command;
// the client responds to PINGs automatically.
func next(ctx context.Context, c *Client) (Message, error) {
	for {
		switch msg, err := readWithContext(ctx, c.in); {
		case err != nil:
			return Message{}, err
		case msg.Command == PING:
			if err := send(ctx, c, PONG, msg.Arguments...); err != nil {
				return Message{}, err
			}
		default:
			return msg, nil
		}
	}
}

func poll(c *Client) {
	var err error
loop:
	for {
		var msg Message
		if msg, err = next(context.Background(), c); err != nil {
			break loop
		}
		switch msg.Command {
		case JOIN:
			if len(msg.Arguments) < 1 {
				log.Printf("Received bad JOIN: %+v\n", msg)
				continue
			}
			channelName := msg.Arguments[0]

			c.Lock()
			ch, ok := c.channels[channelName]
			myNick := c.nick
			c.Unlock()
			if !ok {
				log.Printf("Unknown channel %s received WHOREPLY", channelName)
				continue
			}
			if msg.Origin == myNick {
				// ch.inOrigin is only received once.
				// Don't block in case this is a stray JOIN
				// after having already received one.
				select {
				case ch.inOrigin <- msg.Origin + "!" + msg.User + "@" + msg.Host:
				default:
				}
				continue
			}
			ch.mu.Lock()
			ch.users[msg.Origin] = true
			ch.mu.Unlock()
			join := chat.Join{Who: *chatUser(ch, msg.Origin)}
			sendEvent(ch, join)

		case PART:
			if len(msg.Arguments) < 1 {
				log.Printf("Received bad PART: %+v\n", msg)
				continue
			}
			channelName := msg.Arguments[0]
			c.Lock()
			ch, ok := c.channels[channelName]
			myNick := c.nick
			c.Unlock()
			if !ok {
				log.Printf("Unknown channel %s received WHOREPLY", channelName)
				continue
			}
			if msg.Origin == myNick {
				continue
			}
			ch.mu.Lock()
			delete(ch.users, msg.Origin)
			ch.mu.Unlock()
			leave := chat.Leave{Who: *chatUser(ch, msg.Origin)}
			sendEvent(ch, leave)

		case NICK:
			if len(msg.Arguments) < 1 {
				log.Printf("Received bad NICK: %+v\n", msg)
				continue
			}
			newNick := msg.Arguments[0]

			c.Lock()
			if newNick == c.nick {
				// The bot's nick was changed.
				c.nick = msg.Origin
			}
			for _, ch := range c.channels {
				ch.mu.Lock()
				if ch.users[msg.Origin] {
					delete(ch.users, msg.Origin)
					ch.users[newNick] = true
					rename := chat.Rename{
						From: *chatUser(ch, msg.Origin),
						To:   *chatUser(ch, newNick),
					}
					sendEvent(ch, rename)
				}
				ch.mu.Unlock()
			}
			c.Unlock()

		case QUIT:
			c.Lock()
			for _, ch := range c.channels {
				ch.mu.Lock()
				if ch.users[msg.Origin] {
					delete(ch.users, msg.Origin)
					leave := chat.Leave{Who: *chatUser(ch, msg.Origin)}
					sendEvent(ch, leave)
				}
				ch.mu.Unlock()
			}
			c.Unlock()

		case PRIVMSG:
			if len(msg.Arguments) < 2 {
				log.Printf("Received bad PRIVMSG: %+v\n", msg)
				continue
			}
			text := msg.Arguments[1]
			chName := msg.Arguments[0]
			c.Lock()
			ch, ok := c.channels[chName]
			c.Unlock()
			if !ok {
				log.Printf("Unknown channel %s received PRIVMSG", chName)
				continue
			}
			if strings.HasPrefix(text, actionPrefix) {
				// IRC sends /me actions using CTCP ACTION.
				// Convert it to raw text prefixed by "/me ".
				text = strings.TrimPrefix(text, actionPrefix)
				text = strings.TrimSuffix(text, actionSuffix)
				text = "/me " + strings.TrimSpace(text)
			}
			message := chat.Message{
				ID:   chat.MessageID(text),
				From: chatUser(ch, msg.Origin),
				Text: text,
			}
			sendEvent(ch, message)

		case RPL_WHOREPLY:
			if len(msg.Arguments) < 6 {
				log.Printf("Received bad WHOREPLY: %+v\n", msg)
				continue
			}
			channelName := msg.Arguments[1]
			nick := msg.Arguments[5]
			c.Lock()
			ch, ok := c.channels[channelName]
			myNick := c.nick
			c.Unlock()
			if !ok {
				log.Printf("Unknown channel %s received WHOREPLY", channelName)
				continue
			}
			if nick == myNick {
				continue
			}
			select {
			case ch.inWho <- []string{nick}:
			case ns := <-ch.inWho:
				ch.inWho <- append(ns, nick)
			}

		case RPL_ENDOFWHO:
			if len(msg.Arguments) < 2 {
				log.Printf("Received bad ENDOFWHO: %+v\n", msg)
				continue
			}
			channelName := msg.Arguments[1]
			c.Lock()
			ch, ok := c.channels[channelName]
			c.Unlock()
			if !ok {
				log.Printf("Unknown channel %s received WHOREPLY", channelName)
				return
			}
			close(ch.inWho)
		}
	}
	if strings.Contains(err.Error(), "use of closed network connection") {
		// If the error was 'use of closed network connection', the user called Client.Close.
		// It's not an error.
		err = nil
	}
	c.error <- err
}

func chatUser(ch *channel, nick string) *chat.User {
	return &chat.User{
		ID:          chat.UserID(nick),
		Nick:        nick,
		DisplayName: nick,
		Channel:     ch,
	}
}

func (c *Client) Join(ctx context.Context, channelName string) (chat.Channel, error) {
	c.Lock()
	defer c.Unlock()
	if ch, ok := c.channels[channelName]; ok {
		return ch, nil
	}

	// JOIN and WHO happen with c.Lock held.
	// Maybe it's bad practice to do network sends with a lock held,
	// but it guarantees that everything on c.channels is JOINed.
	// In otherwords, we should never receive a message
	// for a channel that is not already on c.channels.
	if err := send(ctx, c, JOIN, channelName); err != nil {
		return nil, err
	}
	if err := send(ctx, c, WHO, channelName); err != nil {
		return nil, err
	}
	ch := newChannel(c, channelName)
	c.channels[channelName] = ch
	return ch, nil
}
