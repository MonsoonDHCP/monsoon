package dhcpv6

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestPacketEncodeDecodeRoundTrip(t *testing.T) {
	pkt := Packet{
		MessageType:   MessageReply,
		TransactionID: [3]byte{0xaa, 0xbb, 0xcc},
		Options: Options{
			{Code: OptionClientID, Value: []byte{0, 4, 1, 2, 3, 4}},
			{Code: OptionRapidCommit, Value: nil},
		},
	}
	raw, err := pkt.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := DecodePacket(raw)
	if err != nil {
		t.Fatalf("DecodePacket() error = %v", err)
	}
	if got.MessageType != pkt.MessageType || got.TransactionID != pkt.TransactionID {
		t.Fatalf("decoded packet mismatch: %+v", got)
	}
	if !got.Options.HasRapidCommit() {
		t.Fatalf("expected rapid commit option")
	}
}

func TestRelayPacketRoundTrip(t *testing.T) {
	inner := Packet{
		MessageType:   MessageSolicit,
		TransactionID: [3]byte{1, 2, 3},
		Options:       Options{{Code: OptionClientID, Value: []byte{0, 4, 1, 1, 1, 1}}},
	}
	innerRaw, _ := inner.Encode()
	pkt := Packet{
		MessageType: MessageRelayForward,
		HopCount:    1,
		LinkAddress: net.ParseIP("2001:db8::1"),
		PeerAddress: net.ParseIP("fe80::abcd"),
		Options:     Options{{Code: OptionRelayMessage, Value: innerRaw}},
	}
	raw, err := pkt.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := DecodePacket(raw)
	if err != nil {
		t.Fatalf("DecodePacket() error = %v", err)
	}
	enc, ok, err := got.Encapsulated()
	if err != nil || !ok {
		t.Fatalf("Encapsulated() = %v, %v", ok, err)
	}
	if enc.MessageType != MessageSolicit {
		t.Fatalf("unexpected inner type %d", enc.MessageType)
	}
}

func TestOptionsAndDUIDHelpers(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	duid := GenerateDUIDLLT(1, mac, time.Unix(1710000000, 0))
	parsed, err := ParseDUID(duid)
	if err != nil {
		t.Fatalf("ParseDUID() error = %v", err)
	}
	if parsed.Type != DUIDTypeLLT || !bytes.Equal(parsed.LinkLayerAddr, mac) {
		t.Fatalf("unexpected parsed duid: %+v", parsed)
	}

	opts := Options{}
	opts.SetClientID(duid)
	opts.SetServerID(GenerateDUIDLL(1, mac))
	opts.SetRapidCommit()
	opts.SetDNSServers([]net.IP{net.ParseIP("2001:4860:4860::8888")})
	opts.SetDomainList([]string{"example.internal"})
	opts.AddIANA(IANA{
		IAID: 1234,
		T1:   1800,
		T2:   3200,
		Options: Options{
			{Code: OptionIAAddr, Value: IAAddress{
				Address:           net.ParseIP("2001:db8::10"),
				PreferredLifetime: 1800,
				ValidLifetime:     3600,
			}.Encode()},
		},
	})
	raw, err := opts.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := DecodeOptions(raw)
	if err != nil {
		t.Fatalf("DecodeOptions() error = %v", err)
	}
	if !decoded.HasRapidCommit() {
		t.Fatalf("expected rapid commit")
	}
	if got := decoded.DomainList(); len(got) != 1 || got[0] != "example.internal" {
		t.Fatalf("unexpected domain list: %#v", got)
	}
	if got := decoded.DNSServers(); len(got) != 1 || !got[0].Equal(net.ParseIP("2001:4860:4860::8888")) {
		t.Fatalf("unexpected dns servers: %#v", got)
	}
	if got := decoded.IANAs(); len(got) != 1 || got[0].IAID != 1234 {
		t.Fatalf("unexpected iana values: %#v", got)
	}
}
