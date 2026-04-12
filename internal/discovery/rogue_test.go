package discovery

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/dhcpv4"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestRogueDetectorInspectPacket(t *testing.T) {
	detector := NewRogueDetector([]string{"10.0.0.1"}, nil)
	detector.lookupARP = func() map[string]string {
		return map[string]string{"10.0.0.250": "00:50:56:AA:BB:CC"}
	}
	detector.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

	rogue, ok := detector.inspectPacket(buildOfferPacket(t, "10.0.0.250"), &net.UDPAddr{IP: net.ParseIP("10.0.0.250"), Port: 67})
	if !ok {
		t.Fatalf("expected rogue DHCP offer to be detected")
	}
	if rogue.IP != "10.0.0.250" {
		t.Fatalf("unexpected rogue IP %q", rogue.IP)
	}
	if rogue.MAC != "00:50:56:AA:BB:CC" || rogue.Vendor != "VMware, Inc." {
		t.Fatalf("unexpected rogue enrichment %+v", rogue)
	}
	if !strings.Contains(rogue.Source, "dhcp_offer:10.0.0.250") {
		t.Fatalf("unexpected rogue source %q", rogue.Source)
	}
}

func TestRogueDetectorAllowsConfiguredServer(t *testing.T) {
	detector := NewRogueDetector([]string{"10.0.0.1"}, nil)
	if _, ok := detector.inspectPacket(buildOfferPacket(t, "10.0.0.1"), &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 67}); ok {
		t.Fatal("expected configured DHCP server to be ignored")
	}
}

func TestRecordRogueServerPersistsLatestList(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeDiscoveryScans, treeDiscoveryMeta})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	engine := NewEngineWithOptions(eng, nil, nil, time.Minute, Options{})
	if err := engine.RecordRogueServer(context.Background(), RogueServer{
		IP:       "10.0.0.250",
		MAC:      "00:50:56:AA:BB:CC",
		Source:   "dhcp_offer:10.0.0.250",
		Detected: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record rogue: %v", err)
	}
	if err := engine.RecordRogueServer(context.Background(), RogueServer{
		IP:       "10.0.0.251",
		MAC:      "B8:27:EB:00:11:22",
		Source:   "dhcp_offer:10.0.0.251",
		Detected: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record second rogue: %v", err)
	}

	rogue, err := engine.LatestRogueServers(context.Background())
	if err != nil {
		t.Fatalf("latest rogue: %v", err)
	}
	if len(rogue) != 2 || rogue[0].IP != "10.0.0.251" {
		t.Fatalf("unexpected rogue ordering %#v", rogue)
	}
	if !engine.Status(context.Background()).RogueDetected {
		t.Fatal("expected status to reflect rogue detection")
	}

	reloaded := NewEngineWithOptions(eng, nil, nil, time.Minute, Options{})
	rogue, err = reloaded.LatestRogueServers(context.Background())
	if err != nil || len(rogue) != 2 {
		t.Fatalf("reloaded rogue servers = %#v, err = %v", rogue, err)
	}
}

func buildOfferPacket(t *testing.T, serverID string) []byte {
	t.Helper()
	var chaddr [16]byte
	copy(chaddr[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})
	packet := dhcpv4.Packet{
		Op:     2,
		HType:  1,
		HLen:   6,
		XID:    0x12345678,
		YIAddr: net.IPv4(10, 0, 0, 50).To4(),
		SIAddr: net.ParseIP(serverID).To4(),
		CHAddr: chaddr,
		Options: dhcpv4.Options{
			dhcpv4.OptionMessageType:      {dhcpv4.MessageOffer},
			dhcpv4.OptionServerIdentifier: net.ParseIP(serverID).To4(),
		},
	}
	raw, err := packet.Encode()
	if err != nil {
		t.Fatalf("encode packet: %v", err)
	}
	return raw
}
