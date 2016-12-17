// Package telegram provides a Telegram bot client API.
package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

// A Client represents a client connection to the Telegram bot API.
type Client struct {
	token   string
	updates chan Update
	error   chan error
	me      User
}

// New returns a new Client using the given token.
func New(token string) (*Client, error) {
	c := &Client{
		token:   token,
		updates: make(chan Update, 100),
		error:   make(chan error),
	}
	if err := rpc(c, "getMe", nil, &c.me); err != nil {
		return nil, err
	}
	go c.run()
	return c, nil
}

func (c *Client) run() {
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
			c.updates <- u
		}
	}
	close(c.updates)
	select {
	case c.error <- err:
	}
}

// Updates returns the Client's Update channel.
// On error, the Update channel is closed. Err() returns the error.
func (c *Client) Updates() <-chan Update { return c.updates }

// Err returns the first Update error encountered by the Client.
// Note, Err blocks until an error occurs.
func (c *Client) Err() error { return <-c.error }

// Me returns the bot's User information at the time the Client was created.
func (c *Client) Me() User { return c.me }

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
