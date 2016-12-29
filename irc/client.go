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

	"github.com/velour/chat"
)

const (
	actionPrefix = "\x01ACTION"
	actionSuffix = "\x01"
)

var _ chat.Client = &Client{}

// A Client is a client's connection to an IRC server.
type Client struct {
	conn  net.Conn
	in    *bufio.Reader
	error chan error

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
	return dial(ctx, c, nick, fullname, pass)
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
	return dial(ctx, c, nick, fullname, pass)
}

func dial(ctx context.Context, conn net.Conn, nick, fullname, pass string) (*Client, error) {
	c := &Client{
		conn:     conn,
		in:       bufio.NewReader(conn),
		error:    make(chan error),
		nick:     nick,
		channels: make(map[string]*channel),
	}
	if err := register(ctx, c, nick, fullname, pass); err != nil {
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
	if closeErr != nil {
		return closeErr
	}
	return pollErr
}

func send(ctx context.Context, c *Client, cmd string, args ...string) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	msg := Message{Command: cmd, Arguments: args}
	bs := msg.Bytes()
	if len(bs) > MaxBytes {
		return TooLongError{Message: bs[:MaxBytes], NTrunc: len(bs) - MaxBytes}
	}
	err := make(chan error, 1)
	go func() {
		_, e := c.conn.Write(bs)
		err <- e
	}()
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
				continue
			}
			ch.Lock()
			ch.users[msg.Origin] = true
			ch.Unlock()
			sendEvent(c, channelName, &msg, chat.Join{Who: chatUser(msg.Origin)})

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
			ch.Lock()
			delete(ch.users, msg.Origin)
			ch.Unlock()
			sendEvent(c, channelName, &msg, chat.Leave{Who: chatUser(msg.Origin)})

		case NICK:
			if len(msg.Arguments) < 1 {
				log.Printf("Received bad NICK: %+v\n", msg)
				continue
			}
			newNick := msg.Arguments[0]
			rename := chat.Rename{
				From: chatUser(msg.Origin),
				To:   chatUser(newNick),
			}

			c.Lock()
			if newNick == c.nick {
				// The bot's nick was changed.
				c.nick = msg.Origin
			}
			for channelName, ch := range c.channels {
				ch.Lock()
				if ch.users[msg.Origin] {
					delete(ch.users, msg.Origin)
					ch.users[newNick] = true
					sendEventLocked(c, channelName, &msg, rename)
				}
				ch.Unlock()
			}
			c.Unlock()

		case QUIT:
			leave := chat.Leave{Who: chatUser(msg.Origin)}
			c.Lock()
			for channelName, ch := range c.channels {
				ch.Lock()
				if ch.users[msg.Origin] {
					delete(ch.users, msg.Origin)
					sendEventLocked(c, channelName, &msg, leave)
				}
				ch.Unlock()
			}
			c.Unlock()

		case PRIVMSG:
			if len(msg.Arguments) < 2 {
				log.Printf("Received bad PRIVMSG: %+v\n", msg)
				continue
			}
			text := msg.Arguments[1]
			if strings.HasPrefix(text, actionPrefix) {
				// IRC sends /me actions using CTCP ACTION.
				// Convert it to raw text prefixed by "/me ".
				text = strings.TrimPrefix(text, actionPrefix)
				text = strings.TrimSuffix(text, actionSuffix)
				text = "/me " + strings.TrimSpace(text)
			}
			message := chat.Message{
				ID:   chat.MessageID(text),
				From: chatUser(msg.Origin),
				Text: text,
			}
			sendEvent(c, msg.Arguments[0], &msg, message)

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

func chatUser(nick string) chat.User {
	return chat.User{ID: chat.UserID(nick), Nick: nick, Name: nick}
}

func sendEvent(c *Client, channelName string, msg *Message, event interface{}) {
	c.Lock()
	defer c.Unlock()
	sendEventLocked(c, channelName, msg, event)
}

// Just like sendEvent, but assumes that c.Lock is held.
func sendEventLocked(c *Client, channelName string, msg *Message, event interface{}) {
	ch, ok := c.channels[channelName]
	if !ok {
		log.Printf("Unknown channel %s received message %+v", channelName, msg)
		return
	}
	select {
	case ch.in <- []interface{}{event}:
	case es := <-ch.in:
		ch.in <- append(es, event)
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
