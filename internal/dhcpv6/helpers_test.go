package dhcpv6

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestDUIDEncodings(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	ll := testDUIDLL(1, mac)
	if len(ll) != 4+len(mac) {
		t.Fatalf("unexpected DUID-LL length %d", len(ll))
	}
	if got := binary.BigEndian.Uint16(ll[0:2]); got != 3 {
		t.Fatalf("unexpected DUID-LL type %d", got)
	}
	if got := binary.BigEndian.Uint16(ll[2:4]); got != 1 {
		t.Fatalf("unexpected DUID-LL hardware type %d", got)
	}
	if got := net.HardwareAddr(ll[4:]).String(); got != mac.String() {
		t.Fatalf("unexpected DUID-LL MAC %s", got)
	}

	en := []byte{0, 2, 0, 0, 16, 146, 1, 2, 3}
	if got := binary.BigEndian.Uint16(en[0:2]); got != 2 {
		t.Fatalf("unexpected DUID-EN type %d", got)
	}
	if got := binary.BigEndian.Uint32(en[2:6]); got != 4242 {
		t.Fatalf("unexpected DUID-EN enterprise number %d", got)
	}

	var uuid [16]byte
	copy(uuid[:], []byte("1234567890abcdef"))
	uuidRaw := testDUIDUUID(uuid)
	if got := binary.BigEndian.Uint16(uuidRaw[0:2]); got != 4 {
		t.Fatalf("unexpected DUID-UUID type %d", got)
	}
	if got := uuidRaw[2:18]; string(got) != string(uuid[:]) {
		t.Fatalf("unexpected DUID-UUID payload %x", got)
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

func TestRelayAndCopyHelpers(t *testing.T) {
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

	buf := make([]byte, 16)
	copyIPv6(buf, nil)
	if net.IP(buf).String() != net.IPv6zero.String() {
		t.Fatalf("copyIPv6 should zero-fill invalid addresses, got %s", net.IP(buf))
	}
}

func testDUIDLL(hardwareType uint16, mac net.HardwareAddr) []byte {
	buf := make([]byte, 4+len(mac))
	binary.BigEndian.PutUint16(buf[0:2], 3)
	binary.BigEndian.PutUint16(buf[2:4], hardwareType)
	copy(buf[4:], mac)
	return buf
}

func testDUIDLLT(hardwareType uint16, timestamp uint32, mac net.HardwareAddr) []byte {
	buf := make([]byte, 8+len(mac))
	binary.BigEndian.PutUint16(buf[0:2], 1)
	binary.BigEndian.PutUint16(buf[2:4], hardwareType)
	binary.BigEndian.PutUint32(buf[4:8], timestamp)
	copy(buf[8:], mac)
	return buf
}

func testDUIDUUID(uuid [16]byte) []byte {
	buf := make([]byte, 18)
	binary.BigEndian.PutUint16(buf[0:2], 4)
	copy(buf[2:18], uuid[:])
	return buf
}
