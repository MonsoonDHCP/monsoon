package dhcpv6

import (
	"context"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestDUIDHelpersAndErrors(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	if _, err := ParseDUID([]byte{0x00}); err == nil {
		t.Fatalf("expected short duid error")
	}
	if _, err := ParseDUID([]byte{0x00, 0x09}); err == nil {
		t.Fatalf("expected unsupported duid error")
	}

	ll := GenerateDUIDLL(1, mac)
	parsed, err := ParseDUID(ll)
	if err != nil || parsed.Type != DUIDTypeLL || parsed.HardwareType != 1 || parsed.LinkLayerAddr.String() != mac.String() {
		t.Fatalf("parse DUID-LL = %+v err=%v", parsed, err)
	}

	en := []byte{0, 2, 0, 0, 16, 146, 1, 2, 3}
	parsed, err = ParseDUID(en)
	if err != nil || parsed.Type != DUIDTypeEN || parsed.EnterpriseNumber != 4242 {
		t.Fatalf("parse DUID-EN = %+v err=%v", parsed, err)
	}

	var uuid [16]byte
	copy(uuid[:], []byte("1234567890abcdef"))
	uuidRaw := GenerateDUIDUUID(uuid)
	parsed, err = ParseDUID(uuidRaw)
	if err != nil || parsed.Type != DUIDTypeUUID || parsed.UUID != uuid {
		t.Fatalf("parse DUID-UUID = %+v err=%v", parsed, err)
	}

	if got := duidTime(time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)); got != 0 {
		t.Fatalf("duidTime before epoch = %d, want 0", got)
	}
}

func TestPoolManagerSelectionAvailabilityAndResults(t *testing.T) {
	engine, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{
		"leases",
		"leases_by_mac",
		"leases_by_expiry",
		"leases_by_subnet",
		"leases_by_client",
	})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer engine.Close()

	leaseStore := lease.NewStore(engine)
	now := time.Now().UTC()
	for _, item := range []lease.Lease{
		{
			IP:         "2001:db8::10",
			ClientID:   []byte("client-a"),
			State:      lease.StateBound,
			SubnetID:   "2001:db8::/64",
			StartTime:  now,
			Duration:   time.Hour,
			ExpiryTime: now.Add(time.Hour),
		},
		{
			IP:         "2001:db8::11",
			ClientID:   []byte("client-b"),
			State:      lease.StateReleased,
			SubnetID:   "2001:db8::/64",
			StartTime:  now,
			Duration:   time.Hour,
			ExpiryTime: now.Add(time.Hour),
		},
	} {
		if err := leaseStore.Upsert(context.Background(), item); err != nil {
			t.Fatalf("seed lease %s: %v", item.IP, err)
		}
	}

	manager, err := NewPoolManager([]config.SubnetConfig{
		{
			CIDR:    "2001:db8::/64",
			Gateway: "2001:db8::1",
			DNS:     []string{"2001:4860:4860::8888"},
			DHCP: config.SubnetDHCPConfig{
				Enabled:   true,
				PoolStart: "2001:db8::10",
				PoolEnd:   "2001:db8::12",
				LeaseTime: config.Duration{Duration: 2 * time.Hour},
			},
		},
		{
			CIDR:    "2001:db8:1::/64",
			Gateway: "2001:db8:1::1",
			DNS:     []string{"2001:4860:4860::8844"},
			DHCP: config.SubnetDHCPConfig{
				Enabled:   true,
				PoolStart: "2001:db8:1::20",
				PoolEnd:   "2001:db8:1::21",
			},
		},
	}, time.Hour, leaseStore)
	if err != nil {
		t.Fatalf("new pool manager: %v", err)
	}

	pool, err := manager.selectPool(AllocationRequest{RequestedIP: net.ParseIP("2001:db8:1::20")})
	if err != nil || pool.id != "2001:db8:1::/64" {
		t.Fatalf("selectPool(requested) = %+v err=%v", pool, err)
	}
	pool, err = manager.selectPool(AllocationRequest{RelayAddr: net.ParseIP("2001:db8::abcd")})
	if err != nil || pool.id != "2001:db8::/64" {
		t.Fatalf("selectPool(relay) = %+v err=%v", pool, err)
	}

	if available, _ := manager.availableForClient(context.Background(), netip.MustParseAddr("2001:db8::10"), []byte("other")); available {
		t.Fatalf("bound address should not be available to other clients")
	}
	if available, _ := manager.availableForClient(context.Background(), netip.MustParseAddr("2001:db8::10"), []byte("client-a")); !available {
		t.Fatalf("bound address should be available to current client")
	}
	if available, _ := manager.availableForClient(context.Background(), netip.MustParseAddr("2001:db8::11"), []byte("other")); !available {
		t.Fatalf("released address should be reusable")
	}

	result, err := manager.Allocate(context.Background(), AllocationRequest{
		ClientDUID:  []byte("client-a"),
		RequestedIP: net.ParseIP("2001:db8::10"),
		IAID:        77,
	})
	if err != nil || result.IP.String() != "2001:db8::10" || result.IAID != 77 {
		t.Fatalf("allocate existing = %+v err=%v", result, err)
	}
	if result.PreferredLifetime == 0 || result.ValidLifetime == 0 || len(result.DNS) != 1 {
		t.Fatalf("unexpected allocation lifetimes/dns: %+v", result)
	}

	if _, err := NewPoolManager([]config.SubnetConfig{{CIDR: "10.0.0.0/24", DHCP: config.SubnetDHCPConfig{Enabled: true}}}, time.Hour, nil); err == nil {
		t.Fatalf("expected missing IPv6 pool configuration error")
	}
}

func TestPrefixDelegationRelayAndDomainHelpers(t *testing.T) {
	if _, err := NewPrefixDelegationPool("10.0.0.0/24", 28); err == nil {
		t.Fatalf("expected IPv4 PD pool rejection")
	}
	pd, err := NewPrefixDelegationPool("2001:db8::/126", 128)
	if err != nil {
		t.Fatalf("new pd pool: %v", err)
	}
	for i, expected := range []string{"2001:db8::/128", "2001:db8::1/128", "2001:db8::2/128", "2001:db8::3/128"} {
		prefix, allocErr := pd.Allocate()
		if allocErr != nil || prefix.String() != expected {
			t.Fatalf("allocation %d = %s err=%v, want %s", i, prefix, allocErr, expected)
		}
	}
	if _, err := pd.Allocate(); err == nil {
		t.Fatalf("expected pd pool exhaustion")
	}
	if _, ok := addPowerOfTwo(netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"), 0); ok {
		t.Fatalf("expected overflow to fail")
	}

	inner := Packet{MessageType: MessageReply, TransactionID: [3]byte{1, 2, 3}, Options: Options{}}
	reply := encapsulateRelayReply(Packet{
		MessageType: MessageRelayForward,
		HopCount:    2,
		LinkAddress: net.ParseIP("2001:db8::1"),
		PeerAddress: net.ParseIP("fe80::1"),
		Options:     Options{{Code: OptionInterfaceID, Value: []byte("port-1")}},
	}, inner)
	if !reply.IsRelay() || reply.HopCount != 2 {
		t.Fatalf("unexpected relay reply: %+v", reply)
	}
	if _, ok := reply.Options.Get(OptionInterfaceID); !ok {
		t.Fatalf("expected interface-id to be preserved")
	}

	encoded := encodeDomainList([]string{"lab.internal", "", "bad..name"})
	decoded := decodeDomainList(encoded)
	if len(decoded) == 0 || decoded[0] != "lab.internal" {
		t.Fatalf("unexpected decoded domains: %#v", decoded)
	}

	buf := make([]byte, 16)
	copyIPv6(buf, nil)
	if net.IP(buf).String() != net.IPv6zero.String() {
		t.Fatalf("copyIPv6 should zero-fill invalid addresses, got %s", net.IP(buf))
	}
}
