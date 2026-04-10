package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	codecString byte = 1
	codecBytes  byte = 2
	codecInt64  byte = 3
	codecUint64 byte = 4
	codecBool   byte = 5
	codecJSON   byte = 6
)

func EncodeValue(v any) ([]byte, error) {
	switch t := v.(type) {
	case string:
		return append([]byte{codecString}, []byte(t)...), nil
	case []byte:
		return append([]byte{codecBytes}, t...), nil
	case int64:
		buf := make([]byte, 1+binary.MaxVarintLen64)
		buf[0] = codecInt64
		n := binary.PutVarint(buf[1:], t)
		return buf[:1+n], nil
	case uint64:
		buf := make([]byte, 1+binary.MaxVarintLen64)
		buf[0] = codecUint64
		n := binary.PutUvarint(buf[1:], t)
		return buf[:1+n], nil
	case bool:
		if t {
			return []byte{codecBool, 1}, nil
		}
		return []byte{codecBool, 0}, nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return append([]byte{codecJSON}, raw...), nil
	}
}

func DecodeValue(raw []byte, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty value")
	}
	kind := raw[0]
	body := raw[1:]

	switch p := out.(type) {
	case *string:
		if kind != codecString {
			return fmt.Errorf("type mismatch")
		}
		*p = string(body)
		return nil
	case *[]byte:
		if kind != codecBytes {
			return fmt.Errorf("type mismatch")
		}
		*p = append([]byte(nil), body...)
		return nil
	case *int64:
		if kind != codecInt64 {
			return fmt.Errorf("type mismatch")
		}
		v, n := binary.Varint(body)
		if n <= 0 {
			return fmt.Errorf("invalid int64 encoding")
		}
		*p = v
		return nil
	case *uint64:
		if kind != codecUint64 {
			return fmt.Errorf("type mismatch")
		}
		v, n := binary.Uvarint(body)
		if n <= 0 {
			return fmt.Errorf("invalid uint64 encoding")
		}
		*p = v
		return nil
	case *bool:
		if kind != codecBool || len(body) == 0 {
			return fmt.Errorf("type mismatch")
		}
		*p = body[0] == 1
		return nil
	default:
		if kind != codecJSON {
			return fmt.Errorf("unsupported decode destination")
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		return dec.Decode(out)
	}
}
