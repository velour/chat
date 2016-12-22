// Package telegram provides a Telegram bot client API.
package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/velour/bridge/chat"
)

var _ chat.Client = &Client{}

// Client implements the chat.Client interface using the Telegram bot API.
type Client struct {
	token string
	me    User
	error chan error
	close chan bool

	sync.Mutex
	channels map[int64]*channel
}

// New returns a new Client using the given token.
func New(token string) (*Client, error) {
	c := &Client{
		token:    token,
		error:    make(chan error),
		close:    make(chan bool),
		channels: make(map[int64]*channel),
	}
	if err := rpc(c, "getMe", nil, &c.me); err != nil {
		return nil, err
	}
	go poll(c)
	return c, nil
}

// Join returns a chat.Channel corresponding to
// a Telegram group, supergroup, chat, or channel ID.
// The ID string must be the base 10 chat ID number.
func (c *Client) Join(idString string) (chat.Channel, error) {
	var err error
	var req struct {
		ChatID int64 `json:"chat_id"`
	}
	if req.ChatID, err = strconv.ParseInt(idString, 10, 64); err != nil {
		return nil, err
	}
	var chat Chat
	if err := rpc(c, "getChat", req, &chat); err != nil {
		return nil, err
	}

	c.Lock()
	defer c.Unlock()
	var ch *channel
	if ch = c.channels[chat.ID]; ch == nil {
		ch = newChannel(c, chat)
		c.channels[chat.ID] = ch
	}
	return ch, nil
}

func (c *Client) Close() error {
	close(c.close)
	err := <-c.error
	for _, ch := range c.channels {
		close(ch.in)
	}
	return err
}

func poll(c *Client) {
	req := struct {
		Offset  uint64 `json:"offset"`
		Timeout uint64 `json:"timeout"`
	}{}
	req.Timeout = 1 // second

	var err error
loop:
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
		select {
		case <-c.close:
			break loop
		default:
		}
	}
	c.error <- err
}

func update(c *Client, u *Update) {
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
	if ch = c.channels[chat.ID]; ch == nil {
		ch = newChannel(c, *chat)
		c.channels[chat.ID] = ch
	}
	select {
	case ch.in <- []*Update{u}:
	case us := <-ch.in:
		ch.in <- append(us, u)
	}
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
