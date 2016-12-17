// Package telegram provides a Telegram bot client API.
package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/eaburns/pretty"
	"github.com/velour/bridge/chat"
)

var _ chat.Client = &Client{}

// Client implements the chat.Client interface using the Telegram bot API.
type Client struct {
	token string
	me    User
	error chan error

	sync.Mutex
	channels map[string]*channel
}

// New returns a new Client using the given token.
func New(token string) (*Client, error) {
	c := &Client{
		token:    token,
		error:    make(chan error),
		channels: make(map[string]*channel),
	}
	if err := rpc(c, "getMe", nil, &c.me); err != nil {
		return nil, err
	}
	go poll(c)
	return c, nil
}

func (c *Client) Join(name string) (chat.Channel, error) {
	c.Lock()
	defer c.Unlock()
	var ch *channel
	if ch = c.channels[name]; ch == nil {
		ch = newChannel()
		c.channels[name] = ch
	}
	return ch, nil
}

func poll(c *Client) {
	req := struct {
		Offset  uint64 `json:"offset"`
		Timeout uint64 `json:"timeout"`
	}{}
	req.Timeout = 1 // second

	var err error
	for {
		var updates []Update
		if err = rpc(c, "getUpdates", req, &updates); err != nil {
			break
		}
		for _, u := range updates {
			if u.UpdateID < req.Offset {
				// The API actually does not state that the array of Updates is ordered.
				panic("out of order updates")
			}
			req.Offset = u.UpdateID + 1
			update(c, &u)
		}
	}
	c.error <- err
}

func update(c *Client, u *Update) {
	pretty.Print(*u)
	fmt.Println("")

	var chat *Chat
	switch {
	case u.Message != nil:
		chat = &u.Message.Chat
	case u.EditedMessage != nil:
		chat = &u.EditedMessage.Chat
	}
	if chat == nil {
		return
	}
	if chat.Title == nil {
		// Ignore messages not sent to supergroups, channels, or groups.
		return
	}

	c.Lock()
	defer c.Unlock()

	var ch *channel
	if ch = c.channels[*chat.Title]; ch == nil {
		ch = newChannel()
		c.channels[*chat.Title] = ch
	}
	ch.updates <- u
}

func rpc(c *Client, method string, req interface{}, resp interface{}) error {
	url := "https://api.telegram.org/bot" + c.token + "/" + method
	var err error
	var httpResp *http.Response
	if req == nil {
		httpResp, err = http.Get(url)
	} else {
		buf := bytes.NewBuffer(nil)
		if err = json.NewEncoder(buf).Encode(req); err != nil {
			return err
		}
		httpResp, err = http.Post(url, "application/json", buf)
	}
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return errors.New(httpResp.Status)
	}

	result := struct {
		OK          bool        `json:"ok"`
		Description *string     `json:"description"`
		Result      interface{} `json:"result"`
	}{}
	if resp != nil {
		result.Result = resp
	}
	err = json.NewDecoder(httpResp.Body).Decode(&result)
	if !result.OK {
		if result.Description == nil {
			return errors.New("request failed")
		}
		return errors.New(*result.Description)
	}
	return nil
}
