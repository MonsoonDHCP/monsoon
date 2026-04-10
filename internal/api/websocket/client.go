package websocket

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	opcodeContinuation = 0x0
	opcodeText         = 0x1
	opcodeBinary       = 0x2
	opcodeClose        = 0x8
	opcodePing         = 0x9
	opcodePong         = 0xA

	websocketGUID   = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	writeBufferSize = 32
	pingInterval    = 25 * time.Second
	writeTimeout    = 10 * time.Second
)

type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	hub    *Hub

	send chan EventMessage

	mu            sync.RWMutex
	subscriptions []string

	closeOnce sync.Once
	done      chan struct{}
}

type subscriptionMessage struct {
	Action string   `json:"action"`
	Events []string `json:"events"`
}

func Upgrade(w http.ResponseWriter, r *http.Request, hub *Hub) (*Client, error) {
	if !strings.EqualFold(r.Header.Get("Connection"), "Upgrade") && !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return nil, fmt.Errorf("missing websocket upgrade header")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("missing websocket protocol upgrade")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, fmt.Errorf("missing websocket key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("websocket hijacking unsupported")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := websocketAccept(key)
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(response); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &Client{
		conn:          conn,
		reader:        rw.Reader,
		writer:        rw.Writer,
		hub:           hub,
		send:          make(chan EventMessage, writeBufferSize),
		subscriptions: []string{"*"},
		done:          make(chan struct{}),
	}, nil
}

func (c *Client) Run() {
	defer c.Close()

	c.Send(newSystemMessage("system.connected", map[string]any{"ok": true}))

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- c.writeLoop()
	}()

	readErr := c.readLoop()
	select {
	case err := <-writeErr:
		if readErr == nil {
			readErr = err
		}
	default:
	}
	_ = readErr
}

func (c *Client) Send(message EventMessage) {
	if !c.isSubscribed(message.Type) {
		return
	}
	select {
	case c.send <- message:
	default:
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.hub != nil {
			c.hub.unregister(c)
		}
		close(c.send)
		_ = c.writeControlFrame(opcodeClose, nil)
		_ = c.conn.Close()
	})
}

func (c *Client) readLoop() error {
	for {
		header, payload, err := c.readFrame()
		if err != nil {
			if err == io.EOF || strings.Contains(strings.ToLower(err.Error()), "closed") {
				return nil
			}
			return err
		}
		switch header.opcode {
		case opcodeText:
			var msg subscriptionMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				continue
			}
			c.applySubscription(msg)
		case opcodePing:
			if err := c.writeControlFrame(opcodePong, payload); err != nil {
				return err
			}
		case opcodePong:
		case opcodeClose:
			return nil
		case opcodeContinuation, opcodeBinary:
		default:
		}
	}
}

func (c *Client) writeLoop() error {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return nil
		case <-ticker.C:
			if err := c.writeControlFrame(opcodePing, []byte("ping")); err != nil {
				return err
			}
		case message, ok := <-c.send:
			if !ok {
				return nil
			}
			if err := c.writeTextFrame(marshalEvent(message)); err != nil {
				return err
			}
		}
	}
}

func (c *Client) isSubscribed(eventType string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, pattern := range c.subscriptions {
		if matchesSubscription(pattern, eventType) {
			return true
		}
	}
	return false
}

func (c *Client) applySubscription(message subscriptionMessage) {
	events := make([]string, 0, len(message.Events))
	for _, item := range message.Events {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			events = append(events, trimmed)
		}
	}
	if len(events) == 0 {
		events = []string{"*"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(message.Action)) {
	case "subscribe", "":
		c.subscriptions = events
	case "unsubscribe":
		remaining := make([]string, 0, len(c.subscriptions))
		for _, current := range c.subscriptions {
			remove := false
			for _, item := range events {
				if strings.EqualFold(current, item) {
					remove = true
					break
				}
			}
			if !remove {
				remaining = append(remaining, current)
			}
		}
		if len(remaining) == 0 {
			remaining = []string{"*"}
		}
		c.subscriptions = remaining
	}
}

func (c *Client) writeTextFrame(payload []byte) error {
	return c.writeFrame(opcodeText, payload)
}

func (c *Client) writeControlFrame(opcode byte, payload []byte) error {
	return c.writeFrame(opcode, payload)
}

func (c *Client) writeFrame(opcode byte, payload []byte) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	header := []byte{0x80 | opcode}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127)
		size := make([]byte, 8)
		binary.BigEndian.PutUint64(size, uint64(len(payload)))
		header = append(header, size...)
	}
	if _, err := c.writer.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.writer.Write(payload); err != nil {
			return err
		}
	}
	return c.writer.Flush()
}

type frameHeader struct {
	opcode byte
	masked bool
	length uint64
}

func (c *Client) readFrame() (frameHeader, []byte, error) {
	var header frameHeader

	first, err := c.reader.ReadByte()
	if err != nil {
		return header, nil, err
	}
	second, err := c.reader.ReadByte()
	if err != nil {
		return header, nil, err
	}
	header.opcode = first & 0x0F
	header.masked = second&0x80 != 0
	header.length = uint64(second & 0x7F)

	switch header.length {
	case 126:
		buf := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, buf); err != nil {
			return header, nil, err
		}
		header.length = uint64(binary.BigEndian.Uint16(buf))
	case 127:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(c.reader, buf); err != nil {
			return header, nil, err
		}
		header.length = binary.BigEndian.Uint64(buf)
	}

	var maskKey [4]byte
	if header.masked {
		if _, err := io.ReadFull(c.reader, maskKey[:]); err != nil {
			return header, nil, err
		}
	}
	payload := make([]byte, header.length)
	if header.length > 0 {
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return header, nil, err
		}
	}
	if header.masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return header, payload, nil
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}
