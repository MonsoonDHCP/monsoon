package grpc

import (
	"encoding/binary"
	"fmt"
)

const (
	wireVarint = 0
	wireBytes  = 2
)

type protoMarshaler interface {
	marshalProto() []byte
}

func appendTag(dst []byte, field int, wireType int) []byte {
	return binary.AppendUvarint(dst, uint64(field<<3|wireType))
}

func appendBool(dst []byte, field int, value bool) []byte {
	if !value {
		return dst
	}
	dst = appendTag(dst, field, wireVarint)
	return binary.AppendUvarint(dst, 1)
}

func appendInt64(dst []byte, field int, value int64) []byte {
	if value == 0 {
		return dst
	}
	dst = appendTag(dst, field, wireVarint)
	if value < 0 {
		return binary.AppendVarint(dst, value)
	}
	return binary.AppendUvarint(dst, uint64(value))
}

func appendString(dst []byte, field int, value string) []byte {
	if value == "" {
		return dst
	}
	return appendBytes(dst, field, []byte(value))
}

func appendBytes(dst []byte, field int, value []byte) []byte {
	if len(value) == 0 {
		return dst
	}
	dst = appendTag(dst, field, wireBytes)
	dst = binary.AppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}

func appendStrings(dst []byte, field int, values []string) []byte {
	for _, value := range values {
		dst = appendString(dst, field, value)
	}
	return dst
}

func appendMessage(dst []byte, field int, value protoMarshaler) []byte {
	if value == nil {
		return dst
	}
	body := value.marshalProto()
	dst = appendTag(dst, field, wireBytes)
	dst = binary.AppendUvarint(dst, uint64(len(body)))
	return append(dst, body...)
}

func readProtoFields(data []byte, fn func(field int, wireType int, raw []byte, value uint64) error) error {
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 {
			return fmt.Errorf("invalid protobuf tag")
		}
		data = data[n:]

		field := int(tag >> 3)
		wireType := int(tag & 0x7)
		switch wireType {
		case wireVarint:
			value, read := binary.Uvarint(data)
			if read <= 0 {
				return fmt.Errorf("invalid protobuf varint")
			}
			if err := fn(field, wireType, nil, value); err != nil {
				return err
			}
			data = data[read:]
		case wireBytes:
			length, read := binary.Uvarint(data)
			if read <= 0 {
				return fmt.Errorf("invalid protobuf length")
			}
			data = data[read:]
			if uint64(len(data)) < length {
				return fmt.Errorf("protobuf field truncated")
			}
			raw := data[:length]
			if err := fn(field, wireType, raw, 0); err != nil {
				return err
			}
			data = data[length:]
		default:
			return fmt.Errorf("unsupported protobuf wire type %d", wireType)
		}
	}
	return nil
}

func parseBool(value uint64) bool {
	return value != 0
}
