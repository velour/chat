// Package slack provides a slack client API.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/velour/chat"

	"golang.org/x/net/websocket"
)

var (
	api = url.URL{Scheme: "https", Host: "slack.com", Path: "/api"}
)

var _ chat.Client = &Client{}

// A Client represents a connection to the slack API.
type Client struct {
	token   string
	me      *User
	domain  string
	webSock *websocket.Conn

	pingError chan error
	pollError chan error

	// cancel cancels the background goroutines.
	cancel context.CancelFunc

	httpClient http.Client

	sync.Mutex
	channels map[string]*channel
	users    map[chat.UserID]chat.User
	media    map[string]File
	nextID   uint64
	localURL *url.URL
}

// Dial returns a new slack client using the given token.
// The returned Client is connected to the RTM endpoint
// and automatically sends pings.
func Dial(ctx context.Context, token string) (*Client, error) {
	c := &Client{
		token:     token,
		pingError: make(chan error, 1),
		pollError: make(chan error, 1),
		channels:  make(map[string]*channel),
		users:     make(map[chat.UserID]chat.User),
		media:     make(map[string]File),
	}

	var resp struct {
		ResponseHeader
		URL  string `json:"url"`
		Self struct {
			ID string `json:"id"`
		} `json:"self"`
		Team struct {
			Domain string `json:"domain"`
		} `json:"team"`
		Users []User `json:"users"`
	}
	if err := rpc(ctx, c, &resp, "rtm.start"); err != nil {
		return nil, err
	}
	webSock, err := websocket.Dial(resp.URL, "", api.String())
	if err != nil {
		return nil, err
	}
	c.webSock = webSock
	for _, u := range resp.Users {
		if u.ID == resp.Self.ID {
			c.me = &u
		}
		c.users[chat.UserID(u.ID)] = chatUser(&u)
	}
	if c.me == nil {
		return nil, fmt.Errorf("self user %s not in users list", resp.Self.ID)
	}
	c.domain = resp.Team.Domain

	switch event, err := c.next(ctx); {
	case err != nil:
		return nil, err
	case event.Type != "hello":
		return nil, fmt.Errorf("expected hello, got %v", event)
	}

	bkg := context.Background()
	bkg, c.cancel = context.WithCancel(bkg)
	go ping(bkg, c)
	go poll(bkg, c)

	return c, nil
}

func chatUser(u *User) chat.User {
	return chat.User{
		ID:          chat.UserID(u.ID),
		Nick:        u.Name,
		FullName:    u.Profile.RealName,
		DisplayName: u.Name,
		PhotoURL:    u.Profile.Image,
	}
}

// Close ends the Slack client connection
func (c *Client) Close(_ context.Context) error {
	// Cancel the background goroutines,
	// and wait for them to finish
	// before closing the socket.
	c.cancel()
	pollError := <-c.pollError
	pingError := <-c.pingError
	closeError := c.webSock.Close()

	switch {
	case closeError != nil:
		return closeError
	case pollError != nil:
		return pollError
	case pingError != nil:
		return pingError
	default:
		return nil
	}
}

// SetLocalURL enables URL generation for media, using the given URL as a prefix.
// For example, if SetLocalURL is called with "http://www.abc.com/slack/media",
// all Channels on the Client will begin populating non-empty chat.User.PhotoURL fields
// of the form http://www.abc.com/slack/media/<photo file>.
func (c *Client) SetLocalURL(u url.URL) {
	c.Lock()
	c.localURL = &u
	c.Unlock()
}

// Join returns a Channel for a Slack connection
// Note: Slack users must add the bot to their channel.
func (c *Client) join(ctx context.Context, channel string) (*channel, error) {
	c.Lock()
	defer c.Unlock()

	channels, err := c.channelsList(ctx)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if ch.Name() == channel || ch.ID == channel {
			c.channels[ch.ID] = ch // use ID b/c other incoming messages will do so
			return ch, nil
		}
	}
	return nil, errors.New("channel not found")
}

func (c *Client) Join(ctx context.Context, channel string) (chat.Channel, error) {
	return c.join(ctx, channel)
}

func ping(ctx context.Context, c *Client) {
	defer c.cancel()
	defer close(c.pingError)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.send(ctx, map[string]interface{}{"type": "ping"}); err != nil {
				c.pingError <- err
				return
			}
		}
	}
}

func poll(ctx context.Context, c *Client) {
	defer c.cancel()
	defer close(c.pollError)
	defer func() {
		for _, ch := range c.channels {
			close(ch.in)
		}
	}()
	for {
		switch msg, err := c.next(ctx); {
		case err == context.DeadlineExceeded || err == context.Canceled:
			return
		case err != nil:
			c.pollError <- err
			return
		case msg.Type == "message":
			c.update(ctx, msg)
		}
	}
}

func (c *Client) update(ctx context.Context, u Update) {
	c.Lock()
	defer c.Unlock()

	ch, ok := c.channels[u.Channel]
	if !ok {
		ch, _ = c.join(ctx, u.Channel)
	}
	select {
	case ch.in <- []*Update{&u}:
	case us := <-ch.in:
		ch.in <- append(us, &u)
	}
}

// next returns the next event from Slack.
// It never returns pong type messages.
func (c *Client) next(ctx context.Context) (Update, error) {
	err := make(chan error, 1)
	for {
		var u Update
		go func() {
		again:
			e := jsonCodec.Receive(c.webSock, &u)
			if _, ok := e.(*json.UnmarshalTypeError); ok {
				// Not all RTM events can be unmarshaled into an Update.
				// However, all "message" type events can,
				// and that's all we care about.
				// Ignore any events that failed to unmarshal
				// due to UnmarshalTypeError.
				goto again
			}
			err <- e
		}()
		select {
		case <-ctx.Done():
			return Update{}, ctx.Err()
		case err := <-err:
			if err != nil {
				return Update{}, err
			}
			if u.Type != "pong" {
				return u, nil
			}
		}
	}
}

// send sends an RTM message. It returns without waiting for a response.
func (c *Client) send(ctx context.Context, message map[string]interface{}) error {
	c.Lock()
	message["id"] = c.nextID
	c.nextID++
	c.Unlock()
	err := make(chan error, 1)
	go func() { err <- websocket.JSON.Send(c.webSock, message) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-err:
		return err
	}
}

// channelsList returns a list of all slack channels.
func (c *Client) channelsList(ctx context.Context) ([]*channel, error) {
	var resp struct {
		ResponseHeader
		Channels []channel `json:"channels"`
	}
	if err := rpc(ctx, c, &resp, "channels.list"); err != nil {
		return nil, err
	}
	channels := make([]*channel, 0)
	for _, ch := range resp.Channels {
		ch := ch
		initChannel(c, &ch)
		channels = append(channels, &ch)
	}
	return channels, nil
}

// postMessage posts a message to the server with as the given username.
func (c *Client) postMessage(ctx context.Context, username, iconurl, channel, text string) error {
	if iconurl != "" {
		iconurl = "icon_url=" + iconurl
	}
	var resp ResponseHeader
	return rpc(ctx, c, &resp, "chat.postMessage",
		"username="+username,
		iconurl,
		"as_user=false",
		"channel="+channel,
		"text="+text)
}

// ServeHTTP serves files, photos, and other media from Slack.
// It only handles GET requests, and
// the final path element of the request must be a Slack File ID.
// The response is the corresponding file data.
func (c *Client) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if req.Method != http.MethodGet {
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}
	f, err := filesInfo(ctx, c, path.Base(req.URL.Path))
	if err != nil {
		http.Error(w, "Slack files.info failed", http.StatusBadRequest)
		return
	}

	authReq, err := http.NewRequest("GET", f.URLPrivateDownload, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	authReq.Header["Authorization"] = []string{"Bearer " + c.token}

	resp, err := c.httpClient.Do(authReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		if data, err := ioutil.ReadAll(io.LimitReader(resp.Body, 512)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(w, string(data), resp.StatusCode)
		}
		return
	}
	w.Header()["Content-Type"] = []string{f.Mimetype}
	if _, err := io.Copy(w, resp.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func filesInfo(ctx context.Context, c *Client, fileID string) (File, error) {
	c.Lock()
	defer c.Unlock()
	if f, ok := c.media[fileID]; ok {
		return f, nil
	}
	var resp struct {
		ResponseHeader
		File `json:"file"`
	}
	if err := rpc(ctx, c, &resp, "files.info", "file="+fileID, "count=0"); err != nil {
		return File{}, err
	}
	c.media[fileID] = resp.File
	return resp.File, nil
}

type Response interface {
	Header() ResponseHeader
}

func rpc(ctx context.Context, c *Client, resp Response, method string, args ...string) error {
	err := make(chan error, 1)
	go func() { err <- _rpc(c, resp, method, args) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-err:
		return err
	}
}

func _rpc(c *Client, resp Response, method string, args []string) error {
	u := rpcURL(c, method, args)
	httpResp, err := http.Get(u.String())
	if err != nil {
		log.Printf("Slack RPC %s %+v failed: %s\n", method, args, err)
		return err
	}
	defer httpResp.Body.Close()
	if err = decodeJSON(httpResp.Body, resp); err != nil {
		return err
	}
	if h := resp.Header(); !h.OK {
		log.Printf("Slack RPC %s %+v response error: %s\n", method, args, h.Error)
		return errors.New(h.Error)
	} else if h.Warning != "" {
		log.Printf("Slack RPC %s %+v response warning: %s\n", method, args, h.Warning)
	}
	return nil
}

func rpcURL(c *Client, method string, args []string) *url.URL {
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
			panic("bad arg: " + a)
		}
		vals[fs[0]] = []string{fs[1]}
	}
	u.RawQuery = vals.Encode()
	return &u
}

// Like websocket.JSON, but logs a verbose error if decode fails.
var jsonCodec = websocket.Codec{
	Marshal: func(v interface{}) ([]byte, byte, error) {
		data, err := json.Marshal(v)
		return data, websocket.TextFrame, err
	},

	Unmarshal: func(data []byte, _ byte, v interface{}) error {
		if err := json.Unmarshal(data, v); err != nil {
			log.Printf("Error decoding JSON into %T: %s", v, err)
			log.Println(string(data))
			return err
		}
		return nil
	},
}

// decodeJSON decodes a JSON object from r into res.
// If there is an error decoding the JSON,
// the entire JSON contents is logged along with the error.
func decodeJSON(r io.Reader, v interface{}) error {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, v); err != nil {
		log.Printf("Error decoding JSON into %T: %s", v, err)
		log.Println(string(data))
		return err
	}
	return nil
}
