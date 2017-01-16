// Package telegram provides a Telegram bot client API.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/velour/chat"
	"golang.org/x/image/webp"
)

const (
	longPollSeconds    = 100
	minPhotoUpdateTime = 30 * time.Minute
	megabyte           = 1000000
	// Telegram's filesize limit for bots is 20 megabytes.
	fileSizeLimit = 20 * megabyte
)

var _ chat.Client = &Client{}

// Client implements the chat.Client interface using the Telegram bot API.
type Client struct {
	token string
	me    User
	// pollError communicates any errors during getUpdate polling
	// to the Close method.
	pollError chan error
	// Cancel cancels the background goroutines.
	cancel context.CancelFunc

	sync.Mutex
	channels map[int64]*channel
	users    map[int64]*user
	media    map[string]*media
	localURL *url.URL
}

type user struct {
	sync.Mutex
	User
	// photo is the file ID of the user's profile photo.
	photo string
	// photoTime is the last time the user's profile photo was updated.
	photoTime time.Time
}

type media struct {
	sync.Mutex
	File
	// Expires is the time that the URL expires.
	expires time.Time
}

// Dial returns a new Client using the given token.
func Dial(ctx context.Context, token string) (*Client, error) {
	c := &Client{
		token:     token,
		pollError: make(chan error, 1),
		channels:  make(map[int64]*channel),
		users:     make(map[int64]*user),
		media:     make(map[string]*media),
	}
	if err := rpc(ctx, c, "getMe", nil, &c.me); err != nil {
		return nil, err
	}

	bkg := context.Background()
	bkg, c.cancel = context.WithCancel(bkg)
	updates := make(chan []Update, 1)
	go poll(bkg, c, updates)
	go demux(bkg, c, updates)

	return c, nil
}

// Join returns a chat.Channel corresponding to
// a Telegram group, supergroup, chat, or channel ID.
// The ID string must be the base 10 chat ID number.
func (c *Client) Join(ctx context.Context, idString string) (chat.Channel, error) {
	var err error
	var req struct {
		ChatID int64 `json:"chat_id"`
	}
	if req.ChatID, err = strconv.ParseInt(idString, 10, 64); err != nil {
		return nil, err
	}
	var chat Chat
	if err := rpc(ctx, c, "getChat", req, &chat); err != nil {
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

func (c *Client) Close(context.Context) error {
	c.cancel()
	select {
	case err := <-c.pollError:
		return err
	default:
		return nil
	}
}

// SetLocalURL enables URL generation for media, using the given URL as a prefix.
// For example, if SetLocalURL is called with "http://www.abc.com/telegram/media",
// all Channels on the Client will begin populating non-empty chat.User.PhotoURL fields
// of the form http://www.abc.com/telegram/media/<photo file>.
func (c *Client) SetLocalURL(u url.URL) {
	c.Lock()
	c.localURL = &u
	c.Unlock()
}

// Poll does long-polling on the getUpdates method,
// sending Update slices to the updates channel.
// Any errors are sent to c.pollError, and the function returns.
// On return, it closes the updates channel.
func poll(ctx context.Context, c *Client, updates chan<- []Update) {
	defer close(updates)
	req := struct {
		Offset  uint64 `json:"offset"`
		Timeout uint64 `json:"timeout"`
	}{
		Timeout: longPollSeconds, // seconds
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var us []Update
			if err := rpc(ctx, c, "getUpdates", req, &us); err != nil {
				c.pollError <- err
				return
			}
			if n := len(us); n > 0 {
				req.Offset = us[n-1].UpdateID + 1
				updates <- us
			}
		}
	}
}

// Demux de-multiplexes Updates, sending them to the appropriate Channel.
// If either updates is closed or the context is cancelled,
// demux closes all Channel.in channels and returns.
func demux(ctx context.Context, c *Client, updates <-chan []Update) {
	defer func() {
		for _, ch := range c.channels {
			close(ch.in)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case us, ok := <-updates:
			if !ok {
				return
			}
			for _, u := range us {
				update(ctx, c, u)
			}
		}
	}
}

func update(ctx context.Context, c *Client, u Update) {
	var chat *Chat
	var from *User
	switch {
	case u.Message != nil:
		chat = &u.Message.Chat
		from = u.Message.From
	case u.EditedMessage != nil:
		chat = &u.EditedMessage.Chat
		from = u.EditedMessage.From
	}
	if chat == nil || chat.Title == nil {
		// Ignore messages not sent to supergroups, channels, or groups.
		return
	}

	c.Lock()
	defer c.Unlock()

	if from != nil {
		u, ok := c.users[from.ID]
		if !ok {
			u = &user{User: *from}
			c.users[from.ID] = u
		}
		updateUser(ctx, c, u, *from)
	}

	var ch *channel
	if ch = c.channels[chat.ID]; ch == nil {
		ch = newChannel(c, *chat)
		c.channels[chat.ID] = ch
	}
	select {
	case ch.in <- []*Update{&u}:
	case us := <-ch.in:
		ch.in <- append(us, &u)
	}
}

// getChatAdministrators returns ChatMembers for each administrator in the group,
// adding newly discovered Users to the users map.
func getChatAdministrators(ctx context.Context, c *Client, chatID int64) ([]ChatMember, error) {
	req := map[string]interface{}{"chat_id": chatID}
	var resp []ChatMember
	if err := rpc(ctx, c, "getChatAdministrators", req, &resp); err != nil {
		return nil, err
	}
	c.Lock()
	defer c.Unlock()
	for _, cm := range resp {
		u, ok := c.users[cm.User.ID]
		if !ok {
			u = &user{User: cm.User}
			c.users[cm.User.ID] = u
		}
		updateUser(ctx, c, u, cm.User)
	}
	return resp, nil
}

func updateUser(ctx context.Context, c *Client, u *user, latest User) {
	u.Lock()
	defer u.Unlock()
	u.User = latest
	if time.Since(u.photoTime) < minPhotoUpdateTime {
		return
	}
	photo, err := getProfilePhoto(ctx, c, u.ID)
	if err != nil {
		log.Printf("Failed to get user %+v profile photo: %s\n", u, err)
		return
	}
	u.photo = photo
	u.photoTime = time.Now()
}

func getProfilePhoto(ctx context.Context, c *Client, userID int64) (string, error) {
	type userProfilePhotos struct {
		Photos [][]PhotoSize `json:"photos"`
	}
	var resp userProfilePhotos
	req := map[string]interface{}{"user_id": userID, "limit": 1}
	if err := rpc(ctx, c, "getUserProfilePhotos", req, &resp); err != nil {
		return "", err
	}
	if len(resp.Photos) == 0 {
		return "", nil
	}
	return largestPhoto(resp.Photos[0]), nil
}

func largestPhoto(photos []PhotoSize) string {
	size := -1
	var photo string
	for _, ps := range photos {
		if ps.FileSize != nil && *ps.FileSize >= fileSizeLimit {
			continue
		}
		if sz := ps.Width * ps.Height; size < 0 || sz > size {
			photo = ps.FileID
			size = sz
		}
	}
	if photo == "" && len(photos) > 0 {
		return photos[0].FileID
	}
	return photo
}

// ServeHTTP serves files, photos, and other media from Telegram.
// It only handles GET requests, and
// the final path element of the request must be a Telegram File ID.
// The response is the corresponding file data.
func (c *Client) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if req.Method != http.MethodGet {
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}
	url, err := getMediaURL(ctx, c, path.Base(req.URL.Path))
	if err != nil {
		http.Error(w, "Telegram getFile failed", http.StatusBadRequest)
		return
	}
	if url == "" {
		http.Error(w, "Telegram file path missing", http.StatusBadRequest)
		return
	}
	resp, err := http.Get(url)
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
	if err := copyResponse(w, resp.Body, resp.Header); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func copyResponse(w io.Writer, body io.Reader, header map[string][]string) error {
	var mime string
	if ms, ok := header["Content-Type"]; ok && len(ms) > 0 {
		mime = ms[0]
	}
	if mime == "application/octet-stream" {
		log.Printf("Trying to determine Content-Type from application/octet-stream")
		// Try to detect a more specific Content-Type.
		data, err := ioutil.ReadAll(io.LimitReader(body, 512))
		if err != nil {
			log.Printf("Failed to keep 512 bytes to find Content-Type: %s", err)
		} else {
			mime = http.DetectContentType(data)
		}
		body = io.MultiReader(bytes.NewBuffer(data), body)
	}

	if mime == "image/webp" {
		// Re-encode webp images as PNG, because Slack won't inline webp.
		img, err := webp.Decode(body)
		if err == nil {
			return png.Encode(w, img)
		}
		log.Printf("Failed to decode webp image: %s", err)
	}
	_, err := io.Copy(w, body)
	return err
}

func getMediaURL(ctx context.Context, c *Client, fileID string) (string, error) {
	c.Lock()
	m, ok := c.media[fileID]
	if !ok {
		m = new(media)
		c.media[fileID] = m
	}
	m.Lock()
	defer m.Unlock()
	c.Unlock()
	if !ok || time.Now().Before(m.expires) {
		var err error
		if m.File, err = getFile(ctx, c, fileID); err != nil {
			return "", err
		}
		// The URL is valid for an hour; expire it a bit before to be safe.
		m.expires = time.Now().Add(50 * time.Minute)
		c.media[fileID] = m
	}
	var url string
	if m.FilePath != nil {
		url = "https://api.telegram.org/file/bot" + c.token + "/" + *m.FilePath
	}
	return url, nil
}

func getFile(ctx context.Context, c *Client, fileID string) (File, error) {
	var resp File
	req := map[string]interface{}{"file_id": fileID}
	if err := rpc(ctx, c, "getFile", req, &resp); err != nil {
		return File{}, err
	}
	return resp, nil
}

func rpc(ctx context.Context, c *Client, method string, req interface{}, resp interface{}) error {
	err := make(chan error, 1)
	go func() { err <- _rpc(c, method, req, resp) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-err:
		if err != nil {
			log.Printf("Telegram RPC %s %+v failed: %s\n", method, req, err)
		}
		return err
	}
}

func _rpc(c *Client, method string, req interface{}, resp interface{}) error {
	url := "https://api.telegram.org/bot" + c.token + "/" + method

	httpResp, err := reqWithRetry(url, method, req)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	result := struct {
		OK          bool        `json:"ok"`
		Description *string     `json:"description"`
		Result      interface{} `json:"result"`
	}{}
	if resp != nil {
		result.Result = resp
	}
	switch err = json.NewDecoder(httpResp.Body).Decode(&result); {
	case !result.OK && result.Description != nil:
		return errors.New(*result.Description)
	case httpResp.StatusCode != http.StatusOK:
		return errors.New(httpResp.Status)
	case !result.OK:
		return errors.New("request failed")
	default:
		return nil
	}
}

const (
	maxRetry   = 3
	retryDelay = 250 * time.Millisecond
)

// reqWithRetry makes an HTTP request to the URL, retrying on a 500 response.
func reqWithRetry(url, method string, req interface{}) (*http.Response, error) {
	var err error
	var data []byte
	if req != nil {
		if data, err = json.Marshal(req); err != nil {
			return nil, err
		}
	}

	var i int
	var httpResp *http.Response
	for {
		if data != nil {
			httpResp, err = http.Post(url, "application/json", bytes.NewBuffer(data))
		} else {
			httpResp, err = http.Get(url)
		}
		if err != nil {
			return nil, err
		}
		if c := httpResp.StatusCode; c < 500 || c >= 600 {
			return httpResp, nil
		}
		i++
		if i == maxRetry {
			log.Printf("Method %s got %s response, giving up", method, httpResp.Status)
			return httpResp, nil
		}
		log.Printf("Method %s got %s response, retrying", method, httpResp.Status)
		time.Sleep(retryDelay)
	}
}
