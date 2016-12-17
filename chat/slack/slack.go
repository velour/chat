// Package slack provides a slack client API.
package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

var (
	api = url.URL{Scheme: "https", Host: "slack.com", Path: "/api"}
)

// A ResponseError is a slack response with ok=false and an error message.
type ResponseError struct{ Response }

func (err ResponseError) Error() string {
	return "response error: " + err.Response.Error
}

// Response is a header common to all slack responses.
type Response struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Warning string `json:"warning"`
}

// A User object describes a slack user.
type User struct {
	ID string `json:"id"`
	// Name is the username without a leading @.
	Name string `json:"name"`
	// BUG(eaburns): Add remaining User object fields.
}

// A Channel object describes a slack channel.
type Channel struct {
	ID string `json:"id"`
	// Name is the name of the channel without a leading #.
	Name       string `json:"name"`
	IsArchived bool   `json:"is_archived"`
}

// A Client represents a connection to the slack API.
type Client struct {
	token   string
	id      string
	webSock *websocket.Conn
	done    chan chan<- error

	nextID int
	sync.Mutex
}

// NewClient returns a new slack client using the given token.
// The returned Client is connected to the RTM endpoint
// and automatically sends pings.
func NewClient(token string) (*Client, error) {
	c := &Client{token: token, done: make(chan chan<- error)}

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

	event, err := c.Next()
	if err != nil {
		return nil, err
	}
	if hello, ok := event["type"].(string); !ok || hello != "hello" {
		return nil, fmt.Errorf("expected hello, got %v", event)
	}

	go ping(c)

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
			if err := c.Send(map[string]interface{}{"type": "ping"}); err != nil {
				ticker.Stop()
				c.webSock.Close()
				<-c.done <- fmt.Errorf("failed to send ping: %v", err)
				return
			}
		}
	}
}

// Close closes the connection.
func (c *Client) Close() error {
	ch := make(chan error)
	c.done <- ch
	return <-ch
}

// ID returns the client's ID.
func (c *Client) ID() string { return c.id }

// Next returns the next event from Slack.
// It never returns pong type messages.
func (c *Client) Next() (map[string]interface{}, error) {
	for {
		event := make(map[string]interface{})
		if err := websocket.JSON.Receive(c.webSock, &event); err != nil {
			return nil, err
		}
		if t, ok := event["type"].(string); !ok || t != "pong" {
			return event, nil
		}
	}
}

// Send sets the "id" field of the message to the next ID and sends it.
// It does not await a response.
func (c *Client) Send(message map[string]interface{}) error {
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

// ChannelsList returns a list of all slack channels.
func (c *Client) ChannelsList() ([]Channel, error) {
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
	return resp.Channels, nil
}

// GroupsList returns a list of all slack groups — private channels.
func (c *Client) GroupsList() ([]Channel, error) {
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

// PostMessage posts a message to the server with as the given username.
func (c *Client) PostMessage(username, iconurl, channel, text string) error {
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
