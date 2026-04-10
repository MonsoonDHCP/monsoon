package dhcpv6

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestHandleSolicitRequestRenewReleaseAndInfo(t *testing.T) {
	store, handler, serverDUID := newTestHandler(t)
	defer func() { _ = store.Close() }()

	clientDUID := GenerateDUIDLL(1, mustMAC("00:11:22:33:44:55"))
	req := Packet{
		MessageType:   MessageSolicit,
		TransactionID: [3]byte{1, 2, 3},
		Options: Options{
			{Code: OptionClientID, Value: clientDUID},
			{Code: OptionRapidCommit, Value: nil},
			{Code: OptionIANA, Value: IANA{
				IAID: 77,
				Options: Options{
					{Code: OptionIAAddr, Value: IAAddress{Address: net.ParseIP("2001:db8::100")}.Encode()},
				},
			}.Encode()},
		},
	}
	resp, err := handler.Handle(context.Background(), req, &net.UDPAddr{IP: net.ParseIP("fe80::10")})
	if err != nil {
		t.Fatalf("Handle(solicit) error = %v", err)
	}
	if resp == nil || resp.MessageType != MessageReply {
		t.Fatalf("unexpected solicit response: %+v", resp)
	}
	ianas := resp.Options.IANAs()
	if len(ianas) != 1 {
		t.Fatalf("expected one IANA, got %d", len(ianas))
	}
	addr, err := DecodeIAAddress(ianas[0].Options[0].Value)
	if err != nil {
		t.Fatalf("DecodeIAAddress() error = %v", err)
	}
	if addr.Address.String() != "2001:db8::100" {
		t.Fatalf("unexpected allocated address %s", addr.Address)
	}
	leaseRecord, err := handler.store.GetByIP(context.Background(), "2001:db8::100")
	if err != nil {
		t.Fatalf("GetByIP() error = %v", err)
	}
	if leaseRecord.State != lease.StateBound {
		t.Fatalf("expected bound lease, got %s", leaseRecord.State)
	}

	request := Packet{
		MessageType:   MessageRequest,
		TransactionID: [3]byte{1, 2, 4},
		Options: Options{
			{Code: OptionClientID, Value: clientDUID},
			{Code: OptionServerID, Value: serverDUID},
			{Code: OptionIANA, Value: IANA{
				IAID: 77,
				Options: Options{
					{Code: OptionIAAddr, Value: IAAddress{Address: net.ParseIP("2001:db8::100")}.Encode()},
				},
			}.Encode()},
		},
	}
	resp, err = handler.Handle(context.Background(), request, &net.UDPAddr{IP: net.ParseIP("fe80::10")})
	if err != nil {
		t.Fatalf("Handle(request) error = %v", err)
	}
	if resp == nil || resp.MessageType != MessageReply {
		t.Fatalf("unexpected request response: %+v", resp)
	}

	renew := Packet{
		MessageType:   MessageRenew,
		TransactionID: [3]byte{1, 2, 5},
		Options: Options{
			{Code: OptionClientID, Value: clientDUID},
			{Code: OptionServerID, Value: serverDUID},
			{Code: OptionIANA, Value: IANA{IAID: 77}.Encode()},
		},
	}
	resp, err = handler.Handle(context.Background(), renew, &net.UDPAddr{IP: net.ParseIP("fe80::10")})
	if err != nil {
		t.Fatalf("Handle(renew) error = %v", err)
	}
	if resp == nil || resp.MessageType != MessageReply {
		t.Fatalf("unexpected renew response: %+v", resp)
	}

	releaseReq := Packet{
		MessageType:   MessageRelease,
		TransactionID: [3]byte{1, 2, 6},
		Options: Options{
			{Code: OptionClientID, Value: clientDUID},
			{Code: OptionServerID, Value: serverDUID},
		},
	}
	resp, err = handler.Handle(context.Background(), releaseReq, &net.UDPAddr{IP: net.ParseIP("fe80::10")})
	if err != nil {
		t.Fatalf("Handle(release) error = %v", err)
	}
	if resp == nil || resp.MessageType != MessageReply {
		t.Fatalf("unexpected release response: %+v", resp)
	}
	released, err := handler.store.GetByIP(context.Background(), "2001:db8::100")
	if err != nil {
		t.Fatalf("GetByIP() released error = %v", err)
	}
	if released.State != lease.StateReleased {
		t.Fatalf("expected released state, got %s", released.State)
	}

	info := Packet{
		MessageType:   MessageInformationRequest,
		TransactionID: [3]byte{9, 9, 9},
		Options:       Options{{Code: OptionClientID, Value: clientDUID}},
	}
	resp, err = handler.Handle(context.Background(), info, &net.UDPAddr{IP: net.ParseIP("fe80::10")})
	if err != nil {
		t.Fatalf("Handle(info) error = %v", err)
	}
	if resp == nil || len(resp.Options.DomainList()) != 1 || resp.Options.DomainList()[0] != "lab.internal" {
		t.Fatalf("unexpected info-request response: %+v", resp)
	}
}

func TestHandleRelayForward(t *testing.T) {
	store, handler, _ := newTestHandler(t)
	defer func() { _ = store.Close() }()

	clientDUID := GenerateDUIDLL(1, mustMAC("00:11:22:33:44:56"))
	inner := Packet{
		MessageType:   MessageSolicit,
		TransactionID: [3]byte{2, 2, 2},
		Options: Options{
			{Code: OptionClientID, Value: clientDUID},
			{Code: OptionIANA, Value: IANA{IAID: 88}.Encode()},
		},
	}
	innerRaw, _ := inner.Encode()
	relay := Packet{
		MessageType: MessageRelayForward,
		HopCount:    1,
		LinkAddress: net.ParseIP("2001:db8::1"),
		PeerAddress: net.ParseIP("fe80::abcd"),
		Options:     Options{{Code: OptionRelayMessage, Value: innerRaw}},
	}
	resp, err := handler.Handle(context.Background(), relay, &net.UDPAddr{IP: net.ParseIP("fe80::1")})
	if err != nil {
		t.Fatalf("Handle(relay) error = %v", err)
	}
	if resp == nil || resp.MessageType != MessageRelayReply {
		t.Fatalf("unexpected relay response: %+v", resp)
	}
	innerResp, ok, err := resp.Encapsulated()
	if err != nil || !ok {
		t.Fatalf("Encapsulated() = %v, %v", ok, err)
	}
	if innerResp.MessageType != MessageAdvertise {
		t.Fatalf("unexpected inner response type %d", innerResp.MessageType)
	}
}

func newTestHandler(t *testing.T) (*storage.Engine, *Handler, []byte) {
	t.Helper()
	engine, err := storage.OpenEngine(t.TempDir(), []string{
		"leases",
		"leases_by_mac",
		"leases_by_expiry",
		"leases_by_subnet",
		"leases_by_client",
	})
	if err != nil {
		t.Fatalf("OpenEngine() error = %v", err)
	}
	leaseStore := lease.NewStore(engine)
	pools, err := NewPoolManager([]config.SubnetConfig{
		{
			CIDR:    "2001:db8::/64",
			Gateway: "2001:db8::1",
			DNS:     []string{"2001:4860:4860::8888"},
			DHCP: config.SubnetDHCPConfig{
				Enabled:   true,
				PoolStart: "2001:db8::100",
				PoolEnd:   "2001:db8::1ff",
				LeaseTime: config.Duration{Duration: time.Hour},
			},
		},
	}, time.Hour, leaseStore)
	if err != nil {
		t.Fatalf("NewPoolManager() error = %v", err)
	}
	serverDUID := GenerateDUIDLL(1, mustMAC("aa:bb:cc:dd:ee:ff"))
	handler := NewHandler(leaseStore, pools, serverDUID, time.Hour, 2*time.Hour)
	handler.SetDomainList([]string{"lab.internal"})
	return engine, handler, serverDUID
}

func mustMAC(value string) net.HardwareAddr {
	mac, _ := net.ParseMAC(value)
	return mac
}
