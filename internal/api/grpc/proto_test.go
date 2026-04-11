package grpc

import (
	"bytes"
	"errors"
	"testing"
)

type testProtoMessage struct {
	body []byte
}

func (m testProtoMessage) marshalProto() []byte { return append([]byte(nil), m.body...) }

func TestProtoAppendHelpersAndReadFields(t *testing.T) {
	var payload []byte
	payload = appendBool(payload, 1, true)
	payload = appendBool(payload, 2, false)
	payload = appendInt64(payload, 3, 42)
	payload = appendString(payload, 4, "hello")
	payload = appendBytes(payload, 5, []byte("raw"))
	payload = appendStrings(payload, 6, []string{"a", "", "b"})
	payload = appendMessage(payload, 7, testProtoMessage{body: []byte{0x08, 0x01}})

	seen := map[int]any{}
	err := readProtoFields(payload, func(field int, wireType int, raw []byte, value uint64) error {
		if wireType == wireBytes {
			seen[field] = append([]byte(nil), raw...)
		} else {
			seen[field] = value
		}
		return nil
	})
	if err != nil {
		t.Fatalf("readProtoFields: %v", err)
	}
	if seen[1].(uint64) != 1 || seen[3].(uint64) != 42 {
		t.Fatalf("unexpected varints: %#v", seen)
	}
	if string(seen[4].([]byte)) != "hello" || string(seen[5].([]byte)) != "raw" {
		t.Fatalf("unexpected bytes fields: %#v", seen)
	}
	if string(seen[6].([]byte)) != "b" {
		t.Fatalf("last repeated string should be captured in map, got %#v", seen[6])
	}
	if !bytes.Equal(seen[7].([]byte), []byte{0x08, 0x01}) {
		t.Fatalf("unexpected nested message payload: %#v", seen[7])
	}
	if !parseBool(1) || parseBool(0) {
		t.Fatalf("parseBool mismatch")
	}
}

func TestReadProtoFieldsErrors(t *testing.T) {
	if err := readProtoFields([]byte{0x80}, func(int, int, []byte, uint64) error { return nil }); err == nil {
		t.Fatalf("expected invalid tag error")
	}
	if err := readProtoFields([]byte{0x08, 0x80}, func(int, int, []byte, uint64) error { return nil }); err == nil {
		t.Fatalf("expected invalid varint error")
	}
	if err := readProtoFields([]byte{0x0a, 0x05, 'a'}, func(int, int, []byte, uint64) error { return nil }); err == nil {
		t.Fatalf("expected truncated field error")
	}
	if err := readProtoFields([]byte{0x0d}, func(int, int, []byte, uint64) error { return nil }); err == nil {
		t.Fatalf("expected unsupported wire type error")
	}

	sentinel := errors.New("stop")
	err := readProtoFields([]byte{0x08, 0x01}, func(int, int, []byte, uint64) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("callback error not propagated: %v", err)
	}
}
