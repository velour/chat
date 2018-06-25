// Copyright © 2016, The T Authors.

// Package websocket provides a wrapper for github.com/gorilla/websocket.
// The wrapper has limited features; the point is ease of use for some common cases.
// It does NOT check the request Origin header.
// All of its methods are safe for concurrent use.
// It automatically applies a send timeout.
// It transparently handles the closing handshake.
package websocket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// A HandshakeError is returned if Dial fails the handshake.
type HandshakeError struct {
	// Status is the string representation of the HTTP response status code.
	Status string
	// StatusCode is the numeric HTTP response status code.
	StatusCode int
}

func (err HandshakeError) Error() string { return err.Status }

// A Conn is a websocket connection.
type Conn struct {
	conn   *websocket.Conn
	send   chan sendReq
	recv   chan recvMsg
	closed chan struct{}
}

// Dial dials a websocket and returns a new Conn.
// If the handshake fails, a HandshakeError is returned.
func Dial(ctx context.Context, URL *url.URL) (*Conn, error) {
	return DialHeader(ctx, make(http.Header), URL)
}

// DialHeader dials a websocket and returns a new Conn
// using the given HTTP request header.
// If the handshake fails, a HandshakeError is returned.
func DialHeader(ctx context.Context, header http.Header, URL *url.URL) (*Conn, error) {
	var dialer = *websocket.DefaultDialer
	if dl, ok := ctx.Deadline(); ok {
		dialer.HandshakeTimeout = time.Until(dl)
	}

	conn, resp, err := dialer.Dial(URL.String(), header)
	if err == websocket.ErrBadHandshake && resp.StatusCode != http.StatusOK {
		return nil, HandshakeError{Status: resp.Status, StatusCode: resp.StatusCode}
	}
	if err != nil {
		return nil, err
	}
	return newConn(conn), nil
}

// Upgrade upgrades an HTTP handler and returns an new *Conn.
func Upgrade(ctx context.Context, w http.ResponseWriter, req *http.Request) (*Conn, error) {
	var upgrader = websocket.Upgrader{
		// There is no DefaultUpgrader, so use the timeout from the Dialer.
		HandshakeTimeout: websocket.DefaultDialer.HandshakeTimeout,
		CheckOrigin:      func(*http.Request) bool { return true },
	}
	if dl, ok := ctx.Deadline(); ok {
		upgrader.HandshakeTimeout = time.Until(dl)
	}

	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return nil, err
	}
	return newConn(conn), nil
}

func newConn(conn *websocket.Conn) *Conn {
	c := &Conn{
		conn:   conn,
		send:   make(chan sendReq, 10),
		recv:   make(chan recvMsg, 10),
		closed: make(chan struct{}),
	}
	go c.goSend()
	go c.goRecv()
	return c
}

// Close sends a close message to the peer and closes the websocket.
// If the context has a deadline, that deadline is used to wait for the close to send,
// otherwise it uses a 1 second deadline.
func (c *Conn) Close(ctx context.Context) error {
	dl := time.Now().Add(1 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		dl = d
	}
	c.conn.WriteControl(websocket.CloseMessage, nil, dl)

	close(c.send)
	close(c.closed)
	for range c.recv {
	}

	return c.conn.Close()
}

// Send sends a JSON-encoded message.
//
// Send must not be called on a closed connection.
func (c *Conn) Send(ctx context.Context, msg interface{}) error {
	result := make(chan error)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.send <- sendReq{msg: msg, result: result}:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-result:
		return r
	}
}

type sendReq struct {
	msg    interface{}
	result chan<- error
}

func (c *Conn) goSend() {
	for req := range c.send {
		req.result <- c.conn.WriteJSON(req.msg)
	}
}

// Recv receives the next JSON-encoded message into msg.
// If msg is nill, the received message is discarded.
//
// This function must be called continually until Close() is called,
// otherwise the connection will not respond to ping/pong messages.
//
// Calling Recv on a closed connection returns io.EOF.
func (c *Conn) Recv(ctx context.Context, msg interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r, ok := <-c.recv:
		if !ok {
			return io.EOF
		}
		if r.err != nil {
			return r.err
		}
		if msg == nil {
			return nil
		}
		return json.Unmarshal(r.p, msg)
	}
}

type recvMsg struct {
	p   []byte
	err error
}

func (c *Conn) goRecv() {
	defer close(c.recv)
	for {
		var t int
		var m recvMsg
		t, m.p, m.err = c.conn.ReadMessage()
		if _, ok := m.err.(*websocket.CloseError); ok {
			// We got a close. Subsequent calls to Recv will return io.EOF.
			// We will reply to the close when the caller calls Close().
			return
		}
		if m.err == nil && t != websocket.TextMessage {
			continue
		}
		// Send the bytes or the error to the next receiver,
		// but don't wait in the case that the connection was closed.
		select {
		case c.recv <- m:
		case <-c.closed:
			return
		}
		if m.err != nil {
			return
		}
	}
}
