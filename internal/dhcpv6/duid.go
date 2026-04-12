package dhcpv6

import (
	"encoding/binary"
	"errors"
	"net"
	"time"
)

const (
	DUIDTypeLLT  uint16 = 1
	DUIDTypeEN   uint16 = 2
	DUIDTypeLL   uint16 = 3
	DUIDTypeUUID uint16 = 4
)

type DUID struct {
	Type             uint16
	HardwareType     uint16
	Time             uint32
	EnterpriseNumber uint32
	LinkLayerAddr    net.HardwareAddr
	UUID             [16]byte
	Raw              []byte
}

func ParseDUID(raw []byte) (DUID, error) {
	if len(raw) < 2 {
		return DUID{}, errors.New("duid too short")
	}
	kind := binary.BigEndian.Uint16(raw[0:2])
	out := DUID{Type: kind, Raw: append([]byte(nil), raw...)}
	switch kind {
	case DUIDTypeLLT:
		if len(raw) < 8 {
			return DUID{}, errors.New("duid-llt too short")
		}
		out.HardwareType = binary.BigEndian.Uint16(raw[2:4])
		out.Time = binary.BigEndian.Uint32(raw[4:8])
		out.LinkLayerAddr = append(net.HardwareAddr(nil), raw[8:]...)
	case DUIDTypeEN:
		if len(raw) < 6 {
			return DUID{}, errors.New("duid-en too short")
		}
		out.EnterpriseNumber = binary.BigEndian.Uint32(raw[2:6])
	case DUIDTypeLL:
		if len(raw) < 4 {
			return DUID{}, errors.New("duid-ll too short")
		}
		out.HardwareType = binary.BigEndian.Uint16(raw[2:4])
		out.LinkLayerAddr = append(net.HardwareAddr(nil), raw[4:]...)
	case DUIDTypeUUID:
		if len(raw) < 18 {
			return DUID{}, errors.New("duid-uuid too short")
		}
		copy(out.UUID[:], raw[2:18])
	default:
		return DUID{}, errors.New("unsupported duid type")
	}
	return out, nil
}

func GenerateDUIDLL(hardwareType uint16, mac net.HardwareAddr) []byte {
	buf := make([]byte, 4+len(mac))
	binary.BigEndian.PutUint16(buf[0:2], DUIDTypeLL)
	binary.BigEndian.PutUint16(buf[2:4], hardwareType)
	copy(buf[4:], mac)
	return buf
}

func GenerateDUIDUUID(uuid [16]byte) []byte {
	buf := make([]byte, 18)
	binary.BigEndian.PutUint16(buf[0:2], DUIDTypeUUID)
	copy(buf[2:18], uuid[:])
	return buf
}

func duidTime(now time.Time) uint32 {
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if now.Before(epoch) {
		return 0
	}
	return uint32(now.UTC().Sub(epoch).Seconds())
}
