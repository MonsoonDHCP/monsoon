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

func TestHandleRequestRenewReleaseDeclineInformAndValidation(t *testing.T) {
	handler, leaseStore := newTestHandler(t)
	var events []string
	handler.SetOnLeaseEvent(func(eventType string, item lease.Lease) {
		events = append(events, eventType+":"+item.IP+":"+string(item.State))
	})

	request := testDiscoverPacket(false)
	request.Options = Options{}
	request.Options.SetMessageType(MessageRequest)
	request.Options[OptionClientIdentifier] = []byte{1, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	request.Options.SetString(OptionHostName, "request-client")
	request.Options.SetIPv4(OptionRequestedIP, net.IPv4(10, 0, 10, 10))

	resp, err := handler.Handle(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("handle request failed: %v", err)
	}
	if msgType, ok := resp.Options.MessageType(); !ok || msgType != MessageAck {
		t.Fatalf("expected ACK, got %v ok=%v", msgType, ok)
	}
	if len(events) != 1 || events[0] != "lease.created:10.0.10.10:bound" {
		t.Fatalf("unexpected create events: %#v", events)
	}

	resp, err = handler.Handle(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("handle request renew failed: %v", err)
	}
	if msgType, ok := resp.Options.MessageType(); !ok || msgType != MessageAck {
		t.Fatalf("expected renew ACK, got %v ok=%v", msgType, ok)
	}
	if len(events) != 2 || events[1] != "lease.renewed:10.0.10.10:bound" {
		t.Fatalf("unexpected renew events: %#v", events)
	}

	releaseReq := testDiscoverPacket(false)
	releaseReq.Options = Options{}
	releaseReq.Options.SetMessageType(MessageRelease)
	releaseReq.CIAddr = net.IPv4(10, 0, 10, 10)
	if resp, err := handler.Handle(context.Background(), releaseReq, nil); err != nil || resp != nil {
		t.Fatalf("release should return nil response, got resp=%v err=%v", resp, err)
	}
	released, err := leaseStore.GetByIP(context.Background(), "10.0.10.10")
	if err != nil || released.State != lease.StateReleased {
		t.Fatalf("released lease = %+v err=%v", released, err)
	}

	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:       "10.0.10.12",
		MAC:      "AA:BB:CC:DD:EE:12",
		State:    lease.StateBound,
		SubnetID: "10.0.10.0/24",
	}); err != nil {
		t.Fatalf("seed lease for decline: %v", err)
	}
	declineReq := testDiscoverPacket(false)
	declineReq.Options = Options{}
	declineReq.Options.SetMessageType(MessageDecline)
	declineReq.Options.SetIPv4(OptionRequestedIP, net.IPv4(10, 0, 10, 12))
	if resp, err := handler.Handle(context.Background(), declineReq, nil); err != nil || resp != nil {
		t.Fatalf("decline should return nil response, got resp=%v err=%v", resp, err)
	}
	declined, err := leaseStore.GetByIP(context.Background(), "10.0.10.12")
	if err != nil || declined.State != lease.StateDeclined || declined.QuarantineUntil.IsZero() {
		t.Fatalf("declined lease = %+v err=%v", declined, err)
	}

	informReq := testDiscoverPacket(false)
	informReq.Options = Options{}
	informReq.Options.SetMessageType(MessageInform)
	informResp, err := handler.Handle(context.Background(), informReq, nil)
	if err != nil {
		t.Fatalf("inform failed: %v", err)
	}
	if msgType, ok := informResp.Options.MessageType(); !ok || msgType != MessageAck || informResp.YIAddr != nil {
		t.Fatalf("unexpected inform response: %+v", informResp)
	}

	if _, err := handler.Handle(context.Background(), Packet{Options: Options{}}, nil); err == nil {
		t.Fatalf("expected missing message type error")
	}
	if len(events) != 4 ||
		events[2] != "lease.released:10.0.10.10:released" ||
		events[3] != "lease.conflict:10.0.10.12:declined" {
		t.Fatalf("unexpected lifecycle events: %#v", events)
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
