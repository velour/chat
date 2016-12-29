// Package slack provides a slack client API.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/velour/chat"

	"golang.org/x/net/websocket"
)

var (
	api = url.URL{Scheme: "https", Host: "slack.com", Path: "/api"}
)

// A Client represents a connection to the slack API.
type Client struct {
	token   string
	id      string
	me      User
	webSock *websocket.Conn
	done    chan chan<- error

	users    map[chat.UserID]chat.User
	channels map[string]Channel
	nextID   uint64
	sync.Mutex
}

// NewClient returns a new slack client using the given token.
// The returned Client is connected to the RTM endpoint
// and automatically sends pings.
func Dial(ctx context.Context, token string) (*Client, error) {
	c := &Client{
		token:    token,
		done:     make(chan chan<- error),
		channels: make(map[string]Channel),
		users:    make(map[chat.UserID]chat.User),
	}

	var resp struct {
		Response
		URL  string `json:"url"`
		Self struct {
			ID string `json:"id"`
		} `json:"self"`
	}
	if err := c.do(&resp, "rtm.start"); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, ResponseError{resp.Response}
	}
	webSock, err := websocket.Dial(resp.URL, "", api.String())
	if err != nil {
		return nil, err
	}
	c.webSock = webSock
	c.id = resp.Self.ID

	event, err := c.next()
	if err != nil {
		return nil, err
	}
	if event.Type != "hello" {
		return nil, fmt.Errorf("expected hello, got %v", event)
	}

	go ping(c)
	go c.poll()

	return c, nil
}

func ping(c *Client) {
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case ch := <-c.done:
			ticker.Stop()
			ch <- c.webSock.Close()
			return
		case <-ticker.C:
			if err := c.send(map[string]interface{}{"type": "ping"}); err != nil {
				ticker.Stop()
				c.webSock.Close()
				<-c.done <- fmt.Errorf("failed to send ping: %v", err)
				return
			}
		}
	}
}

// Close ends the Slack client connection
func (c *Client) Close(_ context.Context) error {
	for _, ch := range c.channels {
		close(ch.in)
	}
	ch := make(chan error)
	c.done <- ch
	return <-ch
}

// Join returns a Channel for a Slack connection
// Note: Slack users must add the bot to their channel.
func (c *Client) join(channel string) (Channel, error) {
	c.Lock()
	defer c.Unlock()

	channels, err := c.channelsList()
	if err != nil {
		return Channel{}, err
	}
	for _, ch := range channels {
		if ch.Name == channel || ch.ID == channel {
			c.channels[ch.ID] = ch // use ID b/c other incoming messages will do so
			return ch, nil
		}
	}
	return Channel{}, errors.New("Channel not found")
}

func (c *Client) Join(ctx context.Context, channel string) (Channel, error) {
	return c.join(channel)
}

// next returns the next event from Slack.
// It never returns pong type messages.
func (c *Client) next() (Update, error) {
	for {
		event := Update{}
		if err := websocket.JSON.Receive(c.webSock, &event); err != nil {
			return Update{}, err
		}
		if event.Type != "pong" {
			return event, nil
		}
	}
}

func (c *Client) poll() {
	for {
		msg, err := c.next()
		if err != nil {
			continue
		}
		switch msg.Type {
		case "message":
			c.update(msg)
		}
	}
}

func (c *Client) update(u Update) {
	c.Lock()
	defer c.Unlock()

	ch, ok := c.channels[u.Channel]
	if !ok {
		ch, _ = c.join(u.Channel)
	}
	select {
	case ch.in <- []*Update{&u}:
	case us := <-ch.in:
		ch.in <- append(us, &u)
	}
}

// send sets the "id" field of the message to the next ID and sends it.
// It does not await a response.
func (c *Client) send(message map[string]interface{}) error {
	c.Lock()
	message["id"] = c.nextID
	c.nextID++
	c.Unlock()
	return websocket.JSON.Send(c.webSock, message)
}

// UsersList returns a list of all slack users.
func (c *Client) UsersList() ([]User, error) {
	var resp struct {
		Response
		Members []User `json:"members"`
	}
	if err := c.do(&resp, "users.list"); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, ResponseError{resp.Response}
	}
	return resp.Members, nil
}

// channelsList returns a list of all slack channels.
func (c *Client) channelsList() ([]Channel, error) {
	var resp struct {
		Response
		Channels []Channel `json:"channels"`
	}
	if err := c.do(&resp, "channels.list"); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, ResponseError{resp.Response}
	}
	channels := make([]Channel, 0)
	for _, ch := range resp.Channels {
		channels = append(channels, makeChannelFromChannel(c, ch))
	}
	return channels, nil
}

func (c *Client) generateNextID() uint64 {
	return atomic.AddUint64(&c.nextID, 1)
}

// groupsList returns a list of all slack groups — private channels.
func (c *Client) groupsList() ([]Channel, error) {
	var resp struct {
		Response
		Groups []Channel `json:"groups"`
	}
	if err := c.do(&resp, "groups.list"); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, ResponseError{resp.Response}
	}
	return resp.Groups, nil
}

// postMessage posts a message to the server with as the given username.
func (c *Client) postMessage(username, iconurl, channel, text string) error {
	if iconurl != "" {
		iconurl = "icon_url=" + iconurl
	}
	var resp Response
	err := c.do(&resp, "chat.postMessage",
		"username="+username,
		iconurl,
		"as_user=false",
		"channel="+channel,
		"text="+text)
	if err != nil {
		return err
	}
	if !resp.OK {
		return ResponseError{resp}
	}
	return nil
}

func (c *Client) getUser(id chat.UserID) (chat.User, error) {
	if u, ok := c.users[id]; ok {
		return u, nil
	}

	var resp struct {
		Response
		User User `json:"user"`
	}
	err := c.do(&resp, "users.info", "user="+string(id))
	if err != nil {
		return chat.User{}, err
	}
	if !resp.OK {
		return chat.User{}, fmt.Errorf("User not found: %s", id)
	}

	c.users[id] = chat.User{
		ID:       id,
		Nick:     resp.User.Name,
		Name:     resp.User.Profile.RealName,
		PhotoURL: resp.User.Profile.Image,
	}

	return c.users[id], nil
}

func (c *Client) do(resp interface{}, method string, args ...string) error {
	u := api
	u.Path = path.Join(u.Path, method)
	vals := make(url.Values)
	vals["token"] = []string{c.token}
	for _, a := range args {
		if a == "" {
			continue
		}
		fs := strings.SplitN(a, "=", 2)
		if len(fs) != 2 {
			return errors.New("bad arg: " + a)
		}
		vals[fs[0]] = []string{fs[1]}
	}
	u.RawQuery = vals.Encode()
	httpResp, err := http.Get(u.String())
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	return json.NewDecoder(httpResp.Body).Decode(resp)
}
