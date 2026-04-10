package dhcpv6

import (
	"fmt"
	"net/netip"
)

type PrefixDelegationPool struct {
	Prefix           netip.Prefix
	DelegationLength int
	next             netip.Addr
}

func NewPrefixDelegationPool(prefix string, delegationLength int) (*PrefixDelegationPool, error) {
	parsed, err := netip.ParsePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if !parsed.Addr().Is6() {
		return nil, fmt.Errorf("prefix delegation requires IPv6 prefix")
	}
	if delegationLength < parsed.Bits() || delegationLength > 128 {
		return nil, fmt.Errorf("invalid delegation length")
	}
	return &PrefixDelegationPool{
		Prefix:           parsed.Masked(),
		DelegationLength: delegationLength,
		next:             parsed.Masked().Addr(),
	}, nil
}

func (p *PrefixDelegationPool) Allocate() (netip.Prefix, error) {
	if !p.next.IsValid() {
		return netip.Prefix{}, fmt.Errorf("prefix pool exhausted")
	}
	out := netip.PrefixFrom(p.next, p.DelegationLength).Masked()
	step := uint(128 - p.DelegationLength)
	next, ok := addPowerOfTwo(p.next, step)
	if !ok || !p.Prefix.Contains(next) {
		p.next = netip.Addr{}
	} else {
		p.next = next
	}
	return out, nil
}

func addPowerOfTwo(addr netip.Addr, shift uint) (netip.Addr, bool) {
	bytes := addr.As16()
	index := int(15 - shift/8)
	carry := byte(1 << (shift % 8))
	for i := index; i >= 0; i-- {
		sum := uint16(bytes[i]) + uint16(carry)
		bytes[i] = byte(sum)
		carry = byte(sum >> 8)
		if carry == 0 {
			return netip.AddrFrom16(bytes), true
		}
	}
	return netip.Addr{}, false
}
