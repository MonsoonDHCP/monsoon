package dhcpv6

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	OptionClientID      uint16 = 1
	OptionServerID      uint16 = 2
	OptionIANA          uint16 = 3
	OptionIATA          uint16 = 4
	OptionIAAddr        uint16 = 5
	OptionOptionRequest uint16 = 6
	OptionPreference    uint16 = 7
	OptionElapsedTime   uint16 = 8
	OptionRelayMessage  uint16 = 9
	OptionStatusCode    uint16 = 13
	OptionRapidCommit   uint16 = 14
	OptionUserClass     uint16 = 15
	OptionVendorClass   uint16 = 16
	OptionInterfaceID   uint16 = 18
	OptionDNSServers    uint16 = 23
	OptionDomainList    uint16 = 24
	OptionIAPD          uint16 = 25
	OptionIAPrefix      uint16 = 26
	OptionClientFQDN    uint16 = 39
)

const (
	StatusSuccess      uint16 = 0
	StatusUnspecFail   uint16 = 1
	StatusNoAddrsAvail uint16 = 2
	StatusNoBinding    uint16 = 3
	StatusNotOnLink    uint16 = 4
	StatusUseMulticast uint16 = 5
)

type Option struct {
	Code  uint16
	Value []byte
}

type Options []Option

func DecodeOptions(raw []byte) (Options, error) {
	out := make(Options, 0, 8)
	for len(raw) > 0 {
		if len(raw) < 4 {
			return nil, errors.New("truncated option header")
		}
		code := binary.BigEndian.Uint16(raw[0:2])
		length := int(binary.BigEndian.Uint16(raw[2:4]))
		raw = raw[4:]
		if len(raw) < length {
			return nil, fmt.Errorf("option %d truncated", code)
		}
		value := append([]byte(nil), raw[:length]...)
		raw = raw[length:]
		out = append(out, Option{Code: code, Value: value})
	}
	return out, nil
}

func (o Options) Encode() ([]byte, error) {
	out := make([]byte, 0, 256)
	for _, item := range o {
		if len(item.Value) > 65535 {
			return nil, fmt.Errorf("option %d too large", item.Code)
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint16(buf[0:2], item.Code)
		binary.BigEndian.PutUint16(buf[2:4], uint16(len(item.Value)))
		out = append(out, buf...)
		out = append(out, item.Value...)
	}
	return out, nil
}

func (o Options) Get(code uint16) ([]byte, bool) {
	for _, item := range o {
		if item.Code == code {
			return append([]byte(nil), item.Value...), true
		}
	}
	return nil, false
}

func (o Options) Values(code uint16) [][]byte {
	values := make([][]byte, 0, 2)
	for _, item := range o {
		if item.Code == code {
			values = append(values, append([]byte(nil), item.Value...))
		}
	}
	return values
}

func (o *Options) Set(code uint16, value []byte) {
	filtered := make(Options, 0, len(*o)+1)
	for _, item := range *o {
		if item.Code != code {
			filtered = append(filtered, item)
		}
	}
	filtered = append(filtered, Option{Code: code, Value: append([]byte(nil), value...)})
	*o = filtered
}

func (o *Options) Add(code uint16, value []byte) {
	*o = append(*o, Option{Code: code, Value: append([]byte(nil), value...)})
}

func (o Options) ClientID() []byte {
	value, _ := o.Get(OptionClientID)
	return value
}

func (o *Options) SetClientID(duid []byte) {
	o.Set(OptionClientID, duid)
}

func (o Options) ServerID() []byte {
	value, _ := o.Get(OptionServerID)
	return value
}

func (o *Options) SetServerID(duid []byte) {
	o.Set(OptionServerID, duid)
}

func (o Options) HasRapidCommit() bool {
	_, ok := o.Get(OptionRapidCommit)
	return ok
}

func (o *Options) SetRapidCommit() {
	o.Set(OptionRapidCommit, nil)
}

func (o *Options) SetDNSServers(ips []net.IP) {
	buf := make([]byte, 0, len(ips)*16)
	for _, ip := range ips {
		v6 := ip.To16()
		if v6 == nil {
			continue
		}
		buf = append(buf, v6...)
	}
	o.Set(OptionDNSServers, buf)
}

func (o *Options) SetDomainList(domains []string) {
	o.Set(OptionDomainList, encodeDomainList(domains))
}

func (o Options) IANAs() []IANA {
	values := o.Values(OptionIANA)
	out := make([]IANA, 0, len(values))
	for _, value := range values {
		if item, err := DecodeIANA(value); err == nil {
			out = append(out, item)
		}
	}
	return out
}

func (o *Options) AddIANA(item IANA) {
	o.Add(OptionIANA, item.Encode())
}

func (o *Options) SetStatus(code uint16, message string) {
	value := make([]byte, 2)
	binary.BigEndian.PutUint16(value[:2], code)
	value = append(value, []byte(message)...)
	o.Set(OptionStatusCode, value)
}

type IANA struct {
	IAID    uint32
	T1      uint32
	T2      uint32
	Options Options
}

func (i IANA) Encode() []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint32(buf[0:4], i.IAID)
	binary.BigEndian.PutUint32(buf[4:8], i.T1)
	binary.BigEndian.PutUint32(buf[8:12], i.T2)
	opts, _ := i.Options.Encode()
	return append(buf, opts...)
}

func DecodeIANA(raw []byte) (IANA, error) {
	if len(raw) < 12 {
		return IANA{}, errors.New("iana too short")
	}
	opts, err := DecodeOptions(raw[12:])
	if err != nil {
		return IANA{}, err
	}
	return IANA{
		IAID:    binary.BigEndian.Uint32(raw[0:4]),
		T1:      binary.BigEndian.Uint32(raw[4:8]),
		T2:      binary.BigEndian.Uint32(raw[8:12]),
		Options: opts,
	}, nil
}

type IAAddress struct {
	Address           net.IP
	PreferredLifetime uint32
	ValidLifetime     uint32
	Options           Options
}

func (i IAAddress) Encode() []byte {
	buf := make([]byte, 24)
	copy(buf[0:16], i.Address.To16())
	binary.BigEndian.PutUint32(buf[16:20], i.PreferredLifetime)
	binary.BigEndian.PutUint32(buf[20:24], i.ValidLifetime)
	opts, _ := i.Options.Encode()
	return append(buf, opts...)
}

func DecodeIAAddress(raw []byte) (IAAddress, error) {
	if len(raw) < 24 {
		return IAAddress{}, errors.New("iaaddr too short")
	}
	opts, err := DecodeOptions(raw[24:])
	if err != nil {
		return IAAddress{}, err
	}
	return IAAddress{
		Address:           append(net.IP(nil), raw[0:16]...),
		PreferredLifetime: binary.BigEndian.Uint32(raw[16:20]),
		ValidLifetime:     binary.BigEndian.Uint32(raw[20:24]),
		Options:           opts,
	}, nil
}

func encodeDomainList(domains []string) []byte {
	var out []byte
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		labels := strings.Split(domain, ".")
		for _, label := range labels {
			if label == "" || len(label) > 63 {
				continue
			}
			out = append(out, byte(len(label)))
			out = append(out, []byte(label)...)
		}
		out = append(out, 0)
	}
	return out
}
