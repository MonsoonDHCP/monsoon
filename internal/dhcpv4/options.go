package dhcpv4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	OptionPad                  byte = 0
	OptionSubnetMask           byte = 1
	OptionRouter               byte = 3
	OptionDomainNameServer     byte = 6
	OptionHostName             byte = 12
	OptionDomainName           byte = 15
	OptionRequestedIP          byte = 50
	OptionIPAddressLeaseTime   byte = 51
	OptionMessageType          byte = 53
	OptionServerIdentifier     byte = 54
	OptionParameterRequestList byte = 55
	OptionMessage              byte = 56
	OptionMaxMessageSize       byte = 57
	OptionRenewalTimeValue     byte = 58
	OptionRebindingTimeValue   byte = 59
	OptionVendorClassID        byte = 60
	OptionClientIdentifier     byte = 61
	OptionUserClass            byte = 77
	OptionRelayAgentInfo       byte = 82
	OptionRapidCommit          byte = 80
	OptionEnd                  byte = 255
)

const (
	MessageDiscover byte = 1
	MessageOffer    byte = 2
	MessageRequest  byte = 3
	MessageDecline  byte = 4
	MessageAck      byte = 5
	MessageNak      byte = 6
	MessageRelease  byte = 7
	MessageInform   byte = 8
)

type Options map[byte][]byte

func DecodeOptions(raw []byte) (Options, error) {
	opts := make(Options)
	for i := 0; i < len(raw); {
		code := raw[i]
		i++
		switch code {
		case OptionPad:
			continue
		case OptionEnd:
			return opts, nil
		}
		if i >= len(raw) {
			return nil, errors.New("truncated option length")
		}
		l := int(raw[i])
		i++
		if i+l > len(raw) {
			return nil, fmt.Errorf("option %d truncated", code)
		}
		val := append([]byte(nil), raw[i:i+l]...)
		i += l
		opts[code] = val
	}
	return opts, nil
}

func (o Options) Encode() ([]byte, error) {
	if o == nil {
		return []byte{OptionEnd}, nil
	}
	out := make([]byte, 0, 312)
	for code, val := range o {
		if code == OptionPad || code == OptionEnd {
			continue
		}
		if len(val) > 255 {
			return nil, fmt.Errorf("option %d too large", code)
		}
		out = append(out, code, byte(len(val)))
		out = append(out, val...)
	}
	out = append(out, OptionEnd)
	return out, nil
}

func (o Options) MessageType() (byte, bool) {
	v, ok := o[OptionMessageType]
	if !ok || len(v) != 1 {
		return 0, false
	}
	return v[0], true
}

func (o Options) SetMessageType(t byte) {
	o[OptionMessageType] = []byte{t}
}

func (o Options) RequestedIP() (net.IP, bool) {
	v, ok := o[OptionRequestedIP]
	if !ok || len(v) != 4 {
		return nil, false
	}
	return net.IP(v).To4(), true
}

func (o Options) ClientIdentifier() []byte {
	if v, ok := o[OptionClientIdentifier]; ok {
		return append([]byte(nil), v...)
	}
	return nil
}

func (o Options) Hostname() string {
	if v, ok := o[OptionHostName]; ok {
		return string(v)
	}
	return ""
}

func (o Options) VendorClass() string {
	if v, ok := o[OptionVendorClassID]; ok {
		return string(v)
	}
	return ""
}

func (o Options) UserClass() string {
	if v, ok := o[OptionUserClass]; ok {
		return string(v)
	}
	return ""
}

func (o Options) SetIPv4(code byte, ip net.IP) {
	o[code] = append([]byte(nil), ip.To4()...)
}

func (o Options) SetIPv4List(code byte, ips []net.IP) {
	b := make([]byte, 0, 4*len(ips))
	for _, ip := range ips {
		b = append(b, ip.To4()...)
	}
	o[code] = b
}

func (o Options) SetDurationSeconds(code byte, sec uint32) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, sec)
	o[code] = b
}

func (o Options) SetString(code byte, s string) {
	o[code] = []byte(s)
}
