package dhcpv4

import (
	"net"
	"testing"
)

func TestPacketRoundTrip(t *testing.T) {
	p := Packet{
		Op:     1,
		HType:  1,
		HLen:   6,
		XID:    0x11223344,
		Flags:  0x8000,
		CIAddr: net.IPv4(0, 0, 0, 0),
		YIAddr: net.IPv4(0, 0, 0, 0),
		SIAddr: net.IPv4(10, 0, 0, 1),
		GIAddr: net.IPv4(0, 0, 0, 0),
		Options: Options{
			OptionMessageType:        {MessageDiscover},
			OptionHostName:           []byte("host1"),
			OptionRequestedIP:        net.IPv4(10, 0, 1, 50).To4(),
			OptionClientIdentifier:   []byte{1, 2, 3, 4},
			OptionServerIdentifier:   net.IPv4(10, 0, 1, 1).To4(),
			OptionIPAddressLeaseTime: []byte{0, 0, 0x70, 0x80},
		},
	}
	copy(p.CHAddr[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})

	raw, err := p.Encode()
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.XID != p.XID {
		t.Fatalf("xid mismatch: got %x want %x", decoded.XID, p.XID)
	}
	if mt, ok := decoded.Options.MessageType(); !ok || mt != MessageDiscover {
		t.Fatalf("message type mismatch")
	}
	if decoded.ClientMAC().String() != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("mac mismatch: %s", decoded.ClientMAC().String())
	}
}

func TestDecodeOptionsRejectsTruncated(t *testing.T) {
	_, err := DecodeOptions([]byte{OptionMessageType, 2, 1})
	if err == nil {
		t.Fatalf("expected error")
	}
}
