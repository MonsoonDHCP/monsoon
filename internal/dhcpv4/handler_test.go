package dhcpv4

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestHandleDiscoverWithRapidCommitReturnsAck(t *testing.T) {
	handler, leaseStore := newTestHandler(t)

	var events []string
	handler.SetOnLeaseEvent(func(eventType string, item lease.Lease) {
		events = append(events, eventType+":"+item.IP+":"+string(item.State))
	})

	req := testDiscoverPacket(true)
	resp, err := handler.Handle(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("handle discover failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response packet")
	}
	if msgType, ok := resp.Options.MessageType(); !ok || msgType != MessageAck {
		t.Fatalf("expected ACK, got %v ok=%v", msgType, ok)
	}
	if !HasRapidCommit(resp.Options) {
		t.Fatalf("expected rapid commit option in ACK")
	}
	if got := resp.YIAddr.String(); got != "10.0.10.10" {
		t.Fatalf("unexpected yiaddr %s", got)
	}

	stored, err := leaseStore.GetByIP(context.Background(), resp.YIAddr.String())
	if err != nil {
		t.Fatalf("stored lease missing: %v", err)
	}
	if stored.State != lease.StateBound {
		t.Fatalf("expected bound lease, got %s", stored.State)
	}
	if stored.SubnetID != "10.0.10.0/24" {
		t.Fatalf("unexpected subnet id %s", stored.SubnetID)
	}
	if len(events) != 1 || events[0] != "lease.created:10.0.10.10:bound" {
		t.Fatalf("unexpected lease events: %#v", events)
	}
}

func TestHandleDiscoverWithoutRapidCommitReturnsOffer(t *testing.T) {
	handler, leaseStore := newTestHandler(t)

	req := testDiscoverPacket(false)
	resp, err := handler.Handle(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("handle discover failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response packet")
	}
	if msgType, ok := resp.Options.MessageType(); !ok || msgType != MessageOffer {
		t.Fatalf("expected OFFER, got %v ok=%v", msgType, ok)
	}
	if HasRapidCommit(resp.Options) {
		t.Fatalf("did not expect rapid commit option in OFFER")
	}
	if got := resp.YIAddr.String(); got != "10.0.10.10" {
		t.Fatalf("unexpected yiaddr %s", got)
	}

	stored, err := leaseStore.GetByIP(context.Background(), resp.YIAddr.String())
	if err != nil {
		t.Fatalf("stored lease missing: %v", err)
	}
	if stored.State != lease.StateOffered {
		t.Fatalf("expected offered lease, got %s", stored.State)
	}
}

func TestRapidCommitOptionHelpers(t *testing.T) {
	opts := Options{}
	if HasRapidCommit(opts) {
		t.Fatalf("empty options should not have rapid commit")
	}
	EnableRapidCommit(opts)
	if !HasRapidCommit(opts) {
		t.Fatalf("expected rapid commit after enabling")
	}
	if raw, ok := opts[OptionRapidCommit]; !ok || len(raw) != 0 {
		t.Fatalf("expected zero-length rapid commit option, got %#v", raw)
	}
}

func newTestHandler(t *testing.T) (*Handler, lease.Store) {
	t.Helper()

	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{
		"leases",
		"leases_by_mac",
		"leases_by_expiry",
		"leases_by_subnet",
		"leases_by_client",
	})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	leaseStore := lease.NewStore(eng)
	subnets := []config.SubnetConfig{{
		CIDR:    "10.0.10.0/24",
		Name:    "rapid",
		Gateway: "10.0.10.1",
		DNS:     []string{"10.0.10.2"},
		DHCP: config.SubnetDHCPConfig{
			Enabled:   true,
			PoolStart: "10.0.10.10",
			PoolEnd:   "10.0.10.20",
			LeaseTime: config.Duration{Duration: 2 * time.Hour},
		},
	}}
	pools, err := NewPoolManager(subnets, 2*time.Hour, leaseStore)
	if err != nil {
		t.Fatalf("new pool manager: %v", err)
	}

	handler := NewHandler(leaseStore, pools, net.IPv4(10, 0, 10, 1), 2*time.Hour, 4*time.Hour)
	return handler, leaseStore
}

func testDiscoverPacket(rapid bool) Packet {
	opts := Options{}
	opts.SetMessageType(MessageDiscover)
	opts[OptionClientIdentifier] = []byte{1, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	opts.SetString(OptionHostName, "rapid-client")
	if rapid {
		EnableRapidCommit(opts)
	}
	req := Packet{
		Op:      1,
		HType:   1,
		HLen:    6,
		XID:     0x12345678,
		Flags:   0x8000,
		CIAddr:  net.IPv4zero,
		YIAddr:  net.IPv4zero,
		SIAddr:  net.IPv4zero,
		GIAddr:  net.IPv4zero,
		Options: opts,
	}
	copy(req.CHAddr[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})
	return req
}
