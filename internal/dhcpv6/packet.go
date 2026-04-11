package dhcpv6

import (
	"errors"
	"net"
)

const (
	MessageSolicit            byte = 1
	MessageAdvertise          byte = 2
	MessageRequest            byte = 3
	MessageConfirm            byte = 4
	MessageRenew              byte = 5
	MessageRebind             byte = 6
	MessageReply              byte = 7
	MessageRelease            byte = 8
	MessageDecline            byte = 9
	MessageReconfigure        byte = 10
	MessageInformationRequest byte = 11
	MessageRelayForward       byte = 12
	MessageRelayReply         byte = 13
)

type Packet struct {
	MessageType   byte
	TransactionID [3]byte
	HopCount      byte
	LinkAddress   net.IP
	PeerAddress   net.IP
	Options       Options
}

func DecodePacket(raw []byte) (Packet, error) {
	if len(raw) < 4 {
		return Packet{}, errors.New("packet too short")
	}
	pkt := Packet{MessageType: raw[0]}
	switch pkt.MessageType {
	case MessageRelayForward, MessageRelayReply:
		if len(raw) < 34 {
			return Packet{}, errors.New("relay packet too short")
		}
		pkt.HopCount = raw[1]
		pkt.LinkAddress = append(net.IP(nil), raw[2:18]...)
		pkt.PeerAddress = append(net.IP(nil), raw[18:34]...)
		opts, err := DecodeOptions(raw[34:])
		if err != nil {
			return Packet{}, err
		}
		pkt.Options = opts
	default:
		copy(pkt.TransactionID[:], raw[1:4])
		opts, err := DecodeOptions(raw[4:])
		if err != nil {
			return Packet{}, err
		}
		pkt.Options = opts
	}
	return pkt, nil
}

func (p Packet) Encode() ([]byte, error) {
	opts, err := p.Options.Encode()
	if err != nil {
		return nil, err
	}
	switch p.MessageType {
	case MessageRelayForward, MessageRelayReply:
		out := make([]byte, 34)
		out[0] = p.MessageType
		out[1] = p.HopCount
		copyIPv6(out[2:18], p.LinkAddress)
		copyIPv6(out[18:34], p.PeerAddress)
		return append(out, opts...), nil
	default:
		out := make([]byte, 4)
		out[0] = p.MessageType
		copy(out[1:4], p.TransactionID[:])
		return append(out, opts...), nil
	}
}

func (p Packet) IsRelay() bool {
	return p.MessageType == MessageRelayForward || p.MessageType == MessageRelayReply
}

func (p Packet) Encapsulated() (Packet, bool, error) {
	raw, ok := p.Options.Get(OptionRelayMessage)
	if !ok {
		return Packet{}, false, nil
	}
	inner, err := DecodePacket(raw)
	return inner, true, err
}

func copyIPv6(dst []byte, ip net.IP) {
	if len(dst) < 16 {
		return
	}
	v6 := ip.To16()
	if v6 == nil {
		v6 = net.IPv6zero
	}
	copy(dst, v6[:16])
}
