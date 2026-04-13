package ipam

import (
	"encoding/binary"
	"math"
	"net/netip"
)

func Overlaps(a, b netip.Prefix) bool {
	a = a.Masked()
	b = b.Masked()
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func Contains(parent, child netip.Prefix) bool {
	parent = parent.Masked()
	child = child.Masked()
	return parent.Bits() <= child.Bits() && parent.Contains(child.Addr())
}

func AddressCount(prefix netip.Prefix) uint64 {
	bits := prefix.Addr().BitLen() - prefix.Bits()
	if bits >= 64 {
		return ^uint64(0)
	}
	return 1 << bits
}

func NthAddress(prefix netip.Prefix, n uint64) (netip.Addr, bool) {
	addr := prefix.Masked().Addr()
	if !addr.Is4() {
		return netip.Addr{}, false
	}
	base := addr.As4()
	value := uint64(base[0])<<24 | uint64(base[1])<<16 | uint64(base[2])<<8 | uint64(base[3])
	value += n
	if n >= AddressCount(prefix) {
		return netip.Addr{}, false
	}
	if value > math.MaxUint32 {
		return netip.Addr{}, false
	}
	var out [4]byte
	// #nosec G115 -- value is bounded to uint32 range above.
	binary.BigEndian.PutUint32(out[:], uint32(value))
	return netip.AddrFrom4(out), true
}
