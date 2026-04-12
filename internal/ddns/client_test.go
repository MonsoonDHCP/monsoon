package ddns

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/lease"
)

func TestBuildUpdatesAndTSIGMessage(t *testing.T) {
	client, err := NewClient(Config{
		ServerAddr:    "127.0.0.1:53",
		ForwardZone:   "lab.example",
		ReverseZone:   "1.168.192.in-addr.arpa",
		TSIGKey:       "dhcp-key.",
		TSIGSecret:    base64.StdEncoding.EncodeToString([]byte("secret-value")),
		TSIGAlgorithm: "hmac-sha256",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	item := lease.Lease{
		IP:       "192.168.1.44",
		Hostname: "printer-01",
		Duration: time.Hour,
	}
	updates, err := client.buildUpdates(ActionUpsert, item)
	if err != nil {
		t.Fatalf("buildUpdates() error = %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("expected forward and reverse updates, got %d", len(updates))
	}
	if updates[0].zone != "lab.example" {
		t.Fatalf("unexpected forward zone %q", updates[0].zone)
	}
	if updates[1].zone != "1.168.192.in-addr.arpa" {
		t.Fatalf("unexpected reverse zone %q", updates[1].zone)
	}

	wire, err := client.buildMessage(0x1234, updates[0], time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	flags := binary.BigEndian.Uint16(wire[2:4])
	if (flags>>11)&0x0f != opcodeUpdate {
		t.Fatalf("expected update opcode in flags %016b", flags)
	}
	if qd := binary.BigEndian.Uint16(wire[4:6]); qd != 1 {
		t.Fatalf("expected one zone question, got %d", qd)
	}
	if ns := binary.BigEndian.Uint16(wire[8:10]); ns != 2 {
		t.Fatalf("expected two update records, got %d", ns)
	}
	if ar := binary.BigEndian.Uint16(wire[10:12]); ar != 1 {
		t.Fatalf("expected one additional record for TSIG, got %d", ar)
	}

	offset := 12
	zone, next, err := parseName(wire, offset)
	if err != nil {
		t.Fatalf("parse zone name: %v", err)
	}
	if zone != "lab.example." {
		t.Fatalf("unexpected zone name %q", zone)
	}
	offset = next + 4
	for i := 0; i < 2; i++ {
		offset, err = skipRR(wire, offset)
		if err != nil {
			t.Fatalf("skip update rr %d: %v", i, err)
		}
	}
	keyName, next, err := parseName(wire, offset)
	if err != nil {
		t.Fatalf("parse tsig name: %v", err)
	}
	if keyName != "dhcp-key." {
		t.Fatalf("unexpected tsig key name %q", keyName)
	}
	if typ := binary.BigEndian.Uint16(wire[next : next+2]); typ != typeTSIG {
		t.Fatalf("expected TSIG record type, got %d", typ)
	}
}

func TestClientApplySendsUDPUpdate(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket() error = %v", err)
	}
	defer conn.Close()

	received := make(chan []byte, 2)
	go func() {
		for range 2 {
			buf := make([]byte, 1500)
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			packet := append([]byte(nil), buf[:n]...)
			received <- packet
			resp := make([]byte, 12)
			copy(resp[0:2], packet[0:2])
			binary.BigEndian.PutUint16(resp[2:4], 0x8000)
			binary.BigEndian.PutUint16(resp[4:6], 1)
			binary.BigEndian.PutUint16(resp[8:10], 0)
			binary.BigEndian.PutUint16(resp[10:12], 0)
			_, _ = conn.WriteTo(resp, addr)
		}
	}()

	client, err := NewClient(Config{
		ServerAddr:  conn.LocalAddr().String(),
		ForwardZone: "lab.example",
		ReverseZone: "1.168.192.in-addr.arpa",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	item := lease.Lease{
		IP:       "192.168.1.10",
		Hostname: "host-01",
		Duration: 10 * time.Minute,
	}
	if err := client.Apply(context.Background(), item); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	packets := make([][]byte, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(packets) < 2 {
		select {
		case packet := <-received:
			packets = append(packets, packet)
		case <-deadline:
			t.Fatal("timed out waiting for ddns update packets")
		}
	}
	for _, packet := range packets {
		if !strings.Contains(string(packet), "host-01") {
			t.Fatalf("expected packet to contain hostname label")
		}
	}
}

func parseName(msg []byte, offset int) (string, int, error) {
	var labels []string
	for {
		if offset >= len(msg) {
			return "", 0, context.DeadlineExceeded
		}
		length := int(msg[offset])
		offset++
		if length == 0 {
			break
		}
		if offset+length > len(msg) {
			return "", 0, context.DeadlineExceeded
		}
		labels = append(labels, string(msg[offset:offset+length]))
		offset += length
	}
	if len(labels) == 0 {
		return ".", offset, nil
	}
	return strings.Join(labels, ".") + ".", offset, nil
}

func skipRR(msg []byte, offset int) (int, error) {
	_, offset, err := parseName(msg, offset)
	if err != nil {
		return 0, err
	}
	if offset+10 > len(msg) {
		return 0, context.DeadlineExceeded
	}
	rdLength := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
	offset += 10
	if offset+rdLength > len(msg) {
		return 0, context.DeadlineExceeded
	}
	return offset + rdLength, nil
}
