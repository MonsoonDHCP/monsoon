package ipam

import (
	"net/netip"
	"testing"
)

func TestOverlaps(t *testing.T) {
	a := netip.MustParsePrefix("10.0.0.0/24")
	b := netip.MustParsePrefix("10.0.0.128/25")
	c := netip.MustParsePrefix("10.0.1.0/24")
	if !Overlaps(a, b) {
		t.Fatalf("expected overlap")
	}
	if Overlaps(a, c) {
		t.Fatalf("expected no overlap")
	}
}

func TestNthAddress(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/24")
	addr, ok := NthAddress(p, 10)
	if !ok {
		t.Fatalf("expected ok")
	}
	if addr.String() != "10.0.0.10" {
		t.Fatalf("address mismatch: %s", addr)
	}
}
