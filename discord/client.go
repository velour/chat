// Package discord provides a Discord client API.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strconv"
	"sync"
	"time"
	"unicode"

	"github.com/eaburns/pretty"
	"github.com/velour/chat"
	"github.com/velour/chat/websocket"
)

const (
	apiURL = "https://discordapp.com/"
	cdnURL = "https://cdn.discordapp.com/"
)

const (
	OpDispatch       = 0
	OpHeartbeat      = 1
	OpIdentify       = 2
	OpResume         = 6
	OpInvalidSession = 9
	OpHello          = 10
	OpHeartbeatACK   = 11
)

type Client struct {
	token    string
	userID   string
	userName string

	cancelBackground context.CancelFunc
	backgroundDone   chan error
	rpcReq           chan rpc

	mu      sync.Mutex
	joined  map[string]*Channel
	deletes map[string]bool
}

func Dial(ctx context.Context, token string) (*Client, error) {

	background, cancel := context.WithCancel(ctx)
	cl := &Client{
		token:            token,
		cancelBackground: cancel,
		backgroundDone:   make(chan error),
		joined:           make(map[string]*Channel),
		deletes:          make(map[string]bool),
		rpcReq:           make(chan rpc),
	}

	go limitRPCs(background, cl.rpcReq)

	var user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := cl.get(ctx, "users/@me", &user); err != nil {
		return nil, err
	}
	cl.userID = user.ID
	cl.userName = user.Username

	ready := make(chan error)
	go runWithRetry(background, cl, ready)
	if err, ok := <-ready; ok {
		return nil, err
	}
	return cl, nil
}

func (cl *Client) Close(ctx context.Context) error {
	cl.cancelBackground()
	err, _ := <-cl.backgroundDone
	if err == context.DeadlineExceeded || err == context.Canceled {
		err = nil
	}
	for _, ch := range cl.joined {
		stop(ch) // but we don't wait for it.
	}
	return err
}

func (cl *Client) Join(ctx context.Context, guildName, chName string) (chat.Channel, error) {
	var guilds []idAndName
	if err := cl.get(ctx, "/users/@me/guilds", &guilds); err != nil {
		return nil, err
	}
	guildID := nameID(guilds, guildName)
	if guildID == "" {
		return nil, errors.New("guild " + guildName + " not found")
	}

	var channels []idAndName
	if err := cl.get(ctx, "guilds/"+guildID+"/channels", &channels); err != nil {
		return nil, err
	}
	chID := nameID(channels, chName)
	if chID == "" {
		return nil, errors.New("channel " + chName + " not found")
	}

	ch := &Channel{
		cl:        cl,
		id:        chID,
		name:      chName,
		guildID:   guildID,
		guildName: guildName,
	}
	start(ch)

	cl.mu.Lock()
	cl.joined[chID] = ch
	cl.mu.Unlock()
	return ch, nil
}

type idAndName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func nameID(nids []idAndName, name string) string {
	for _, x := range nids {
		if x.Name == name {
			return x.ID
		}
	}
	return ""
}

type msg struct {
	Op int         `json:"op"`
	T  string      `json:"t"`
	D  interface{} `json:"d"`
	S  int         `json:"s"`
}

type user struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
}

type pingPong struct {
	Op int `json:"op"`
	D  int `json:"d"`
}

type event struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`

	// For Message type.
	Author  *user  `json:"author"`
	Content string `json:"content"`

	// MESSAGE_DELETE_BULK:
	// Sets ChannelID and sets IDs instead of ID.
	IDs []string `json:"ids"`
}

func dispatchEvent(cl *Client, t string, data interface{}) error {
	// Hack: ctonvert an event held in an annoying map format into a struct
	// by using the json package.
	b, err := json.Marshal(data)
	if err != nil {
		log.Println("Discord failed to marshal", pretty.String(data))
		return err
	}
	var ev event
	if err := json.Unmarshal(b, &ev); err != nil {
		log.Println("Discord failed to unmarshal", pretty.String(data))
		return err
	}

	ch, ok := getChannel(cl, ev.ChannelID)
	if !ok || ev.Author != nil && ev.Author.ID == cl.userID {
		return nil
	}
	switch t {
	case "MESSAGE_CREATE":
		if m := eventMessage(ch, &ev); m.From != nil {
			send(ch, m)
		}
	case "MESSAGE_UPDATE":
		if m := eventMessage(ch, &ev); m.From != nil {
			send(ch, chat.Edit{OrigID: chat.MessageID(ev.ID), New: m})
		}
	case "MESSAGE_DELETE":
		if sendDelete(cl, ev.ID) {
			send(ch, chat.Delete{ID: chat.MessageID(ev.ID), Channel: ch})
		}
	case "MESSAGE_DELETE_BULK":
		for _, id := range ev.IDs {
			if sendDelete(cl, ev.ID) {
				send(ch, chat.Delete{ID: chat.MessageID(id), Channel: ch})
			}
		}
		/*
			case "GUILD_MEMBER_ADD":
			case "GUILD_MEMBER_REMOVE":
		*/
	}
	return nil
}

func sendDelete(cl *Client, id string) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.deletes[id] {
		delete(cl.deletes, id)
		return false
	}
	return true
}

func getChannel(cl *Client, id string) (*Channel, bool) {
	cl.mu.Lock()
	ch, ok := cl.joined[id]
	cl.mu.Unlock()
	return ch, ok
}

func send(ch *Channel, e chat.Event) {
	select {
	case ch.in <- []chat.Event{e}:
	case es := <-ch.in:
		ch.in <- append(es, e)
	}
}

func eventMessage(ch *Channel, ev *event) chat.Message {
	var m chat.Message
	m.ID = chat.MessageID(ev.ID)
	m.From = authorUser(ch, ev.Author)
	m.Text = ev.Content
	return m
}

func authorUser(ch *Channel, au *user) *chat.User {
	if au == nil {
		return nil
	}
	var u chat.User
	u.ID = chat.UserID(au.ID)
	u.Nick = au.Username
	u.FullName = au.Username
	u.DisplayName = au.Username
	u.Channel = ch
	ch.cl.mu.Lock()
	u.PhotoURL = cdnURL + path.Join("avatars", au.ID, au.Avatar+".png")
	ch.cl.mu.Unlock()
	return &u
}

func runWithRetry(ctx context.Context, cl *Client, ready chan<- error) {
	defer close(cl.backgroundDone)

	conn, err := dial(ctx, cl)
	if err != nil {
		ready <- err
		return
	}
	s, err := newSession(ctx, conn, cl.token)
	if err != nil {
		conn.Close(ctx)
		ready <- err
		return
	}
	close(ready)

	for {
		runErr := run(ctx, cl, conn, &s)
		conn.Close(ctx)
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Println("Discord disconnected")
		time.Sleep(5 * time.Second)

		if runErr == errInvalidSession {
			log.Println("Discord getting a new session.")
			s, err = newSession(ctx, conn, cl.token)
		} else {
			log.Println("Discord reconnecting")
			conn, err = dial(ctx, cl)
			if err == nil {
				log.Println("Discord resuming.")
				s, err = resumeSession(ctx, conn, s)
			}
		}
		if err != nil {
			cl.backgroundDone <- runErr
			return
		}
		log.Println("Discord reconnected")
	}
}

func run(ctx context.Context, cl *Client, conn *websocket.Conn, s *session) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 1)
	msgs := make(chan msg, 1)
	go func() {
		for {
			var m msg
			if err := conn.Recv(ctx, &m); err != nil {
				log.Println("Discord receive error:", err)
				errs <- err
				return
			}
			msgs <- m
		}
	}()

	ping := pingPong{Op: OpHeartbeat}
	pong := pingPong{Op: OpHeartbeatACK}
	tick := time.NewTicker(s.pingTime)
	defer tick.Stop()
	ponged := true
	for {
		select {
		case err := <-errs:
			return err
		case <-tick.C:
			if !ponged {
				return errors.New("pong timeout")
			}
			ping.D = s.seq
			if err := conn.Send(ctx, ping); err != nil {
				return err
			}
			ponged = false
		case m := <-msgs:
			switch {
			case m.Op == OpHeartbeatACK:
				ponged = true
			case m.Op == OpHeartbeat:
				if err := conn.Send(ctx, pong); err != nil {
					return err
				}
			case m.Op == OpInvalidSession:
				return errInvalidSession
			case m.Op == OpDispatch:
				s.seq = m.S
				dispatchEvent(cl, m.T, m.D)
			}
		}
	}
}

func dial(ctx context.Context, cl *Client) (*websocket.Conn, error) {
	var gateway struct {
		URL string
	}
	if err := cl.get(ctx, "gateway/bot", &gateway); err != nil {
		return nil, err
	}
	url, err := url.Parse(gateway.URL)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Authorization", "Bot "+cl.token)
	return websocket.DialHeader(ctx, header, url)
}

type session struct {
	id       string
	seq      int
	pingTime time.Duration
	token    string
}

var errInvalidSession = errors.New("invalid session")

func newSession(ctx context.Context, conn *websocket.Conn, token string) (s session, err error) {
	defer func() {
		if err != nil {
			conn.Close(ctx)
		}
	}()
	if s.pingTime, err = expectHello(ctx, conn); err != nil {
		return session{}, err
	}
	if err = identify(ctx, conn, token); err != nil {
		return session{}, err
	}
	if s.id, err = expectReady(ctx, conn); err != nil {
		return session{}, err
	}
	s.seq = -1
	s.token = token
	return s, nil
}

func resumeSession(ctx context.Context, conn *websocket.Conn, s session) (session, error) {
	type resume struct {
		Token     string `json:"token"`
		SessionID string `json:"session_id"`
		Seq       int    `json:"seq"`
	}
	msg := struct {
		Op int    `json:"op"`
		D  resume `json:"d"`
	}{
		Op: OpResume,
		D: resume{
			Token:     s.token,
			SessionID: s.id,
			Seq:       s.seq,
		},
	}
	var err error
	if s.pingTime, err = expectHello(ctx, conn); err != nil {
		return session{}, err
	}
	if err = conn.Send(ctx, msg); err != nil {
		return session{}, err
	}
	return s, nil
}

func expectHello(ctx context.Context, conn *websocket.Conn) (time.Duration, error) {
	var hello struct {
		Op int
		D  struct {
			Interval int32 `json:"heartbeat_interval"`
		}
	}
	if err := conn.Recv(ctx, &hello); err != nil {
		return 0, err
	}
	hi := time.Duration(hello.D.Interval) * time.Millisecond
	switch {
	case hello.Op == OpInvalidSession:
		return 0, errInvalidSession
	case hello.Op != OpHello:
		return 0, errors.New("expected a hello, got " + strconv.Itoa(hello.Op))
	case hello.D.Interval == 0:
		return 0, errors.New("no heartbeat interval")
	case hi < 2*time.Second:
		return 0, errors.New("ridiculous heartbeat interval: " + hi.String())
	}
	return hi - time.Second, nil
}

func identify(ctx context.Context, conn *websocket.Conn, token string) error {
	type props struct {
		OS      string `json:"$os"`
		Browser string `json:"$browser"`
		Device  string `json:"$device"`
	}
	type ident struct {
		Token      string `json:"token"`
		Properties props  `json:"properties"`
	}
	type msg struct {
		Op int   `json:"op"`
		D  ident `json:"d"`
	}
	m := &msg{
		Op: OpIdentify,
		D: ident{
			Token: token,
			Properties: props{
				OS:      runtime.GOOS,
				Browser: "github.com/velour/chat",
				Device:  runtime.GOARCH,
			},
		},
	}
	return conn.Send(ctx, m)
}

func expectReady(ctx context.Context, conn *websocket.Conn) (string, error) {
	var ready struct {
		Op int
		T  string
		D  struct {
			// unused fields elided
			SessionID       string                   `json:"session_id"`
			PrivateChannels []map[string]interface{} `json:"private_channels"`
		}
	}
	if err := conn.Recv(ctx, &ready); err != nil {
		return "", err
	}
	if ready.Op != OpDispatch {
		return "", errors.New("expected a ready, got non-event: " + strconv.Itoa(ready.Op))
	}
	if ready.T != "READY" {
		return "", errors.New("expected a ready, got: " + ready.T)
	}
	if ready.D.SessionID == "" {
		return "", errors.New("invalid, empty session ID")
	}
	return ready.D.SessionID, nil
}

func (cl *Client) get(ctx context.Context, method string, resp interface{}) error {
	return cl.rpc(ctx, http.MethodGet, method, nil, resp)
}

func (cl *Client) post(ctx context.Context, method string, req, resp interface{}) error {
	return cl.rpc(ctx, http.MethodPost, method, req, resp)
}

func (cl *Client) patch(ctx context.Context, method string, req, resp interface{}) error {
	return cl.rpc(ctx, http.MethodPatch, method, req, resp)
}

func (cl *Client) del(ctx context.Context, method string) error {
	return cl.rpc(ctx, http.MethodDelete, method, nil, nil)
}

type httpErr int

func (err httpErr) Error() string { return "HTTP error " + http.StatusText(int(err)) }

func (cl *Client) rpc(ctx context.Context, httpMethod, apiMethod string, req, resp interface{}) error {
	httpReq, err := newRequest(cl.token, httpMethod, apiMethod, req)
	if err != nil {
		return err
	}

	respChan := make(chan respOrError, 1)
	cl.rpcReq <- rpc{req: httpReq.WithContext(ctx), resp: respChan}
	respOrErr := <-respChan
	if respOrErr.err != nil {
		return err
	}
	httpResp := respOrErr.resp

	defer httpResp.Body.Close()
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return httpErr(httpResp.StatusCode)
	}
	if resp == nil {
		return nil
	}
	data, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, resp)
}

func newRequest(token, httpMethod, discordMethod string, req interface{}) (*http.Request, error) {
	var body io.Reader
	if req != nil {
		b, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	httpReq, err := http.NewRequest(httpMethod, apiURL, body)
	if err != nil {
		return nil, err
	}
	httpReq.URL.Path = path.Join("/api", discordMethod)
	httpReq.Header.Set("Authorization", "Bot "+token)
	if req != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	return httpReq, nil
}

type rpc struct {
	req  *http.Request
	resp chan<- respOrError
}

type respOrError struct {
	resp *http.Response
	err  error
}

func limitRPCs(ctx context.Context, rpcs <-chan rpc) {
	limits := make(map[string]time.Time)
	for {
		select {
		case <-ctx.Done():
			return
		case rpc := <-rpcs:
			k := limitKey(rpc.req)
			if when := limits[k]; time.Now().Before(when) {
				time.Sleep(time.Until(when))
			}
		retry:
			r, err := http.DefaultClient.Do(rpc.req.WithContext(ctx))
			if err == nil && r.StatusCode == http.StatusTooManyRequests {
				retry, err := retryHeader(r.Header, "Retry-After")
				if err == nil {
					time.Sleep(time.Until(retry))
					goto retry
				}
			}
			rpc.resp <- respOrError{resp: r, err: err}
			if err != nil {
				break
			}
			if v := r.Header["X-Ratelimit-Remaining"]; len(v) > 0 {
				n, err := strconv.Atoi(v[0])
				if err != nil {
					break
				}
				if n == 0 {
					retry, err := retryHeader(r.Header, "X-Ratelimit-Reset")
					if err != nil {
						break
					}
					limits[k] = retry
				}
			}
		}
		// Limit to an avg of 2 per second, as recommended by the Discord docs.
		time.Sleep(500 * time.Millisecond)
	}
}

func retryHeader(h http.Header, k string) (time.Time, error) {
	v, ok := h[k]
	if !ok || len(v) == 0 {
		return time.Time{}, errors.New("missing " + k + " header")
	}
	until, err := strconv.ParseInt(v[0], 10, 64)
	if err != nil {
		return time.Time{}, errors.New("malformed " + k + " value: " + v[0])
	}
	return time.Unix(until, 0), nil
}

// limitKey returns the key identifying the rate limit to use for this request.
// Discord uses different rate limits for each path, not counting the filename
// in the case that it is a parameter (all numbers).
// Except channel_id, guild_id, and webhook_id don't count as parameters.
// Finally, the http method doesn't affect the rate limit, except that
// as a special case, message deletes have their own limit.
func limitKey(req *http.Request) string {
	switch r := splitAll(req.URL.Path); {
	case len(r) == 0:
		return req.URL.Path

	case len(r) == 5 &&
		r[1] == "channels" &&
		r[3] == "messages" &&
		req.Method == http.MethodDelete:
		// Message deletes have their own rate limit.
		return http.MethodDelete

	case len(r) == 3 &&
		(r[1] == "channels" || r[1] == "guilds" || r[1] == "webhooks"):
		// channel_id, guild_id, and webhook_id count in the path,
		// and should not be stripped.
		return req.URL.Path

	// all other number-only filenames do not count, just their directory does.
	case isAllDigits(r[len(r)-1]):
		return path.Dir(req.URL.Path)

	default:
		return req.URL.Path
	}
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func splitAll(p string) []string {
	var elms []string
	for len(p) > 0 {
		dir, elm := path.Dir(p), path.Base(p)
		elms = append(elms, elm)
		if dir == p {
			break
		}
		p = dir
	}
	for i := 0; i < len(elms)/2; i++ {
		j := len(elms) - i - 1
		elms[i], elms[j] = elms[j], elms[i]
	}
	return elms
}
