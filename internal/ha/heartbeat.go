package ha

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

type heartbeatMessage struct {
	Type      string    `json:"type"`
	Node      string    `json:"node,omitempty"`
	Role      Role      `json:"role,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	Priority  int       `json:"priority,omitempty"`
	Draining  bool      `json:"draining,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Secret    string    `json:"secret,omitempty"`
}

func deriveListenAddr(peerAddr string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(peerAddr))
	if err != nil || strings.TrimSpace(port) == "" {
		return ":8068"
	}
	return ":" + port
}

func writeWireMessage(conn net.Conn, msg heartbeatMessage) error {
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = conn.Write(raw)
	return err
}

func readWireMessage(conn net.Conn) (heartbeatMessage, error) {
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	raw, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return heartbeatMessage{}, err
	}
	var msg heartbeatMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return heartbeatMessage{}, err
	}
	return msg, nil
}

func dialAndExchange(ctx context.Context, addr string, msg heartbeatMessage) (heartbeatMessage, time.Duration, error) {
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return heartbeatMessage{}, 0, err
	}
	defer conn.Close()
	if err := writeWireMessage(conn, msg); err != nil {
		return heartbeatMessage{}, 0, err
	}
	resp, err := readWireMessage(conn)
	return resp, time.Since(start), err
}

func validateSecret(expected, actual string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return fmt.Errorf("invalid peer secret")
	}
	return nil
}
