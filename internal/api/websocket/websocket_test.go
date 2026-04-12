package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	restapi "github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/events"
)

func TestNormalizeEventAddsCanonicalAliases(t *testing.T) {
	out := NormalizeEvent(events.Event{
		Type: "reservation.upserted",
		Data: map[string]any{"ip": "10.0.1.10", "mac": "AA:BB:CC:DD:EE:FF"},
	})
	if len(out) < 2 {
		t.Fatalf("expected alias event, got %d", len(out))
	}
	if out[1].Type != "address.reserved" {
		t.Fatalf("expected address.reserved, got %s", out[1].Type)
	}
}

func TestHubBroadcastWithSubscription(t *testing.T) {
	broker := events.NewBroker(8)
	hub := NewHub(broker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx)

	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	conn, reader := mustDialWebSocket(t, ts.URL)
	defer conn.Close()

	msg := readTextMessage(t, reader)
	if msg.Type != "system.connected" {
		t.Fatalf("expected system.connected, got %s", msg.Type)
	}

	writeMaskedTextFrame(t, conn, `{"action":"subscribe","events":["discovery.*"]}`)
	time.Sleep(50 * time.Millisecond)
	broker.Publish(events.Event{Type: "lease.released", Data: map[string]any{"ip": "10.0.1.20"}})
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := readFrame(reader); err == nil {
		t.Fatalf("unexpected event for unsubscribed topic")
	}
	_ = conn.SetReadDeadline(time.Time{})

	broker.Publish(events.Event{Type: "discovery.scan_queued", Data: map[string]any{"scan_id": "scan-1"}})
	msg = readTextMessage(t, reader)
	if msg.Type != "discovery.scan_queued" && msg.Type != "discovery.started" {
		t.Fatalf("unexpected websocket message: %+v", msg)
	}
	msg = readTextMessage(t, reader)
	if msg.Type != "discovery.started" {
		t.Fatalf("expected discovery.started alias, got %+v", msg)
	}
}

func TestHubWildcardSubscriptionReceivesCanonicalEvent(t *testing.T) {
	broker := events.NewBroker(8)
	hub := NewHub(broker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx)

	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	conn, reader := mustDialWebSocket(t, ts.URL)
	defer conn.Close()

	_ = readTextMessage(t, reader)
	writeMaskedTextFrame(t, conn, `{"action":"subscribe","events":["subnet.*"]}`)
	time.Sleep(50 * time.Millisecond)

	broker.Publish(events.Event{Type: "subnet.upserted", Data: map[string]any{"cidr": "10.0.2.0/24", "name": "New VLAN"}})
	first := readTextMessage(t, reader)
	second := readTextMessage(t, reader)

	types := []string{first.Type, second.Type}
	if !containsType(types, "subnet.upserted") || !containsType(types, "subnet.created") {
		t.Fatalf("unexpected event types: %#v", types)
	}
}

func TestHubRejectsCrossOriginWhenAuthIsEnforced(t *testing.T) {
	broker := events.NewBroker(8)
	hub := NewHub(broker)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := restapi.WithIdentity(restapi.WithAuthEnforcement(r.Context(), true), auth.Identity{
			Username: "admin",
			Role:     auth.DefaultRoleAdmin,
		})
		hub.Handler().ServeHTTP(w, r.WithContext(ctx))
	}))
	defer ts.Close()

	status, _, _ := dialWebSocketHandshake(t, ts.URL, map[string]string{
		"Origin": "https://evil.example",
	})
	if !strings.Contains(status, "403") {
		t.Fatalf("expected 403 for cross-origin websocket, got %s", status)
	}
}

func TestHubRejectsMissingIdentityWhenAuthIsEnforced(t *testing.T) {
	broker := events.NewBroker(8)
	hub := NewHub(broker)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := restapi.WithAuthEnforcement(r.Context(), true)
		hub.Handler().ServeHTTP(w, r.WithContext(ctx))
	}))
	defer ts.Close()

	status, _, _ := dialWebSocketHandshake(t, ts.URL, nil)
	if !strings.Contains(status, "401") {
		t.Fatalf("expected 401 for missing identity, got %s", status)
	}
}

func TestClientSendAfterCloseDoesNotPanic(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()

	client := &Client{
		conn:          clientConn,
		reader:        bufio.NewReader(clientConn),
		writer:        bufio.NewWriter(clientConn),
		send:          make(chan EventMessage, 1),
		subscriptions: []string{"*"},
		done:          make(chan struct{}),
	}
	client.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Send() panicked after Close(): %v", r)
		}
	}()

	client.Send(EventMessage{Type: "lease.released"})
}

func mustDialWebSocket(t *testing.T, serverURL string) (net.Conn, *bufio.Reader) {
	t.Helper()
	status, conn, reader := dialWebSocketHandshake(t, serverURL, nil)
	if !strings.Contains(status, "101") {
		t.Fatalf("unexpected handshake status: %s", status)
	}
	return conn, reader
}

func dialWebSocketHandshake(t *testing.T, serverURL string, headers map[string]string) (string, net.Conn, *bufio.Reader) {
	t.Helper()
	address := strings.TrimPrefix(serverURL, "http://")
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request := "GET /ws HTTP/1.1\r\n" +
		"Host: " + address + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n"
	for name, value := range headers {
		request += name + ": " + value + "\r\n"
	}
	request += "\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read handshake status: %v", err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read handshake header: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return status, conn, reader
}

func writeMaskedTextFrame(t *testing.T, conn net.Conn, payload string) {
	t.Helper()
	mask := [4]byte{1, 2, 3, 4}
	body := []byte(payload)
	frame := []byte{0x81}
	switch {
	case len(body) < 126:
		frame = append(frame, 0x80|byte(len(body)))
	case len(body) <= 65535:
		frame = append(frame, 0x80|126, byte(len(body)>>8), byte(len(body)))
	default:
		t.Fatal("payload too large for test frame")
	}
	frame = append(frame, mask[:]...)
	for idx, b := range body {
		frame = append(frame, b^mask[idx%4])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func readTextMessage(t *testing.T, reader *bufio.Reader) EventMessage {
	t.Helper()
	opcode, payload, err := readFrame(reader)
	if err != nil {
		t.Fatalf("read websocket frame: %v", err)
	}
	if opcode != opcodeText {
		t.Fatalf("expected text opcode, got %d", opcode)
	}
	var msg EventMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode event message: %v", err)
	}
	return msg
}

func readFrame(reader *bufio.Reader) (byte, []byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	opcode := first & 0x0F
	length := uint64(second & 0x7F)
	switch length {
	case 126:
		buf := make([]byte, 2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(buf))
	case 127:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(buf)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	return opcode, payload, nil
}

func containsType(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
