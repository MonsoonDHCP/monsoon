package dhcpv4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	bootpHeaderLen = 236
	magicCookie    = 0x63825363
)

type Packet struct {
	Op     byte
	HType  byte
	HLen   byte
	Hops   byte
	XID    uint32
	Secs   uint16
	Flags  uint16
	CIAddr net.IP
	YIAddr net.IP
	SIAddr net.IP
	GIAddr net.IP
	CHAddr [16]byte
	SName  [64]byte
	File   [128]byte
	Options
}

func DecodePacket(raw []byte) (Packet, error) {
	if len(raw) < bootpHeaderLen+4 {
		return Packet{}, errors.New("packet too short")
	}
	p := Packet{}
	p.Op = raw[0]
	p.HType = raw[1]
	p.HLen = raw[2]
	p.Hops = raw[3]
	p.XID = binary.BigEndian.Uint32(raw[4:8])
	p.Secs = binary.BigEndian.Uint16(raw[8:10])
	p.Flags = binary.BigEndian.Uint16(raw[10:12])
	p.CIAddr = net.IP(raw[12:16]).To4()
	p.YIAddr = net.IP(raw[16:20]).To4()
	p.SIAddr = net.IP(raw[20:24]).To4()
	p.GIAddr = net.IP(raw[24:28]).To4()
	copy(p.CHAddr[:], raw[28:44])
	copy(p.SName[:], raw[44:108])
	copy(p.File[:], raw[108:236])

	cookie := binary.BigEndian.Uint32(raw[236:240])
	if cookie != magicCookie {
		return Packet{}, fmt.Errorf("invalid magic cookie: %x", cookie)
	}

	opts, err := DecodeOptions(raw[240:])
	if err != nil {
		return Packet{}, err
	}
	p.Options = opts
	return p, nil
}

func (p Packet) Encode() ([]byte, error) {
	out := make([]byte, bootpHeaderLen+4)
	out[0] = p.Op
	out[1] = p.HType
	out[2] = p.HLen
	out[3] = p.Hops
	binary.BigEndian.PutUint32(out[4:8], p.XID)
	binary.BigEndian.PutUint16(out[8:10], p.Secs)
	binary.BigEndian.PutUint16(out[10:12], p.Flags)
	copy(out[12:16], p.CIAddr.To4())
	copy(out[16:20], p.YIAddr.To4())
	copy(out[20:24], p.SIAddr.To4())
	copy(out[24:28], p.GIAddr.To4())
	copy(out[28:44], p.CHAddr[:])
	copy(out[44:108], p.SName[:])
	copy(out[108:236], p.File[:])
	binary.BigEndian.PutUint32(out[236:240], magicCookie)

	opt, err := p.Options.Encode()
	if err != nil {
		return nil, err
	}
	out = append(out, opt...)
	return out, nil
}

func (p Packet) ClientMAC() net.HardwareAddr {
	hlen := int(p.HLen)
	if hlen <= 0 || hlen > 16 {
		hlen = 6
	}
	return append(net.HardwareAddr(nil), p.CHAddr[:hlen]...)
}
