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

func TestResponseTargetPreferenceOrder(t *testing.T) {
	remote := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 68}
	resp := Packet{YIAddr: net.IPv4(10, 0, 0, 10)}

	target := ResponseTarget(Packet{GIAddr: net.IPv4(10, 0, 0, 1)}, remote, resp)
	if target.IP.String() != "10.0.0.1" || target.Port != 67 {
		t.Fatalf("expected relay target, got %v", target)
	}

	target = ResponseTarget(Packet{CIAddr: net.IPv4(10, 0, 0, 2)}, remote, resp)
	if target.IP.String() != "10.0.0.2" || target.Port != 68 {
		t.Fatalf("expected client target, got %v", target)
	}

	target = ResponseTarget(Packet{}, remote, resp)
	if target.IP.String() != "10.0.0.10" || target.Port != 68 {
		t.Fatalf("expected yiaddr target, got %v", target)
	}

	target = ResponseTarget(Packet{}, remote, Packet{})
	if target != remote {
		t.Fatalf("expected remote fallback, got %v want %v", target, remote)
	}
}

func TestClassifierAndRelayHelpers(t *testing.T) {
	classifier := NewClassifier([]ClassRule{
		{Name: "phones", VendorClass: "phone*", Pool: "voice"},
		{Name: "ops", MACPrefix: "AA:BB", UserClass: "ops", CircuitID: "edge-*", Pool: "ops"},
	})
	if got := classifier.Match(MatchContext{VendorClass: "Phone-X"}); got != "voice" {
		t.Fatalf("vendor class match failed: %q", got)
	}
	if got := classifier.Match(MatchContext{MACPrefix: "aa:bb:cc:dd", UserClass: "OPS", CircuitID: "EDGE-01"}); got != "ops" {
		t.Fatalf("compound classifier match failed: %q", got)
	}
	if got := classifier.Match(MatchContext{VendorClass: "unknown"}); got != "" {
		t.Fatalf("unexpected classifier match: %q", got)
	}
	if !wildcardMatch("*", "anything") || !hasPrefixFold("AABB", "aa") || !equalFold("Hello", "hello") {
		t.Fatalf("string helper matching failed")
	}

	raw := BuildRelayAgentInfo(" edge-1 ", " remote-7 ")
	info := ParseRelayAgentInfo(raw)
	if info.CircuitID != "edge-1" || info.RemoteID != "remote-7" {
		t.Fatalf("unexpected relay parse result: %+v", info)
	}
	truncated := ParseRelayAgentInfo([]byte{1, 5, 'a'})
	if truncated.CircuitID != "" || len(truncated.Raw) != 3 {
		t.Fatalf("unexpected truncated relay parse result: %+v", truncated)
	}
}

func TestPoolManagerReuseRequestedIPAndRelaySelection(t *testing.T) {
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
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	now := time.Now().UTC()
	for _, item := range []lease.Lease{
		{
			IP:         "10.60.0.10",
			MAC:        "AA:BB:CC:DD:EE:01",
			State:      lease.StateBound,
			SubnetID:   "10.60.0.0/24",
			StartTime:  now,
			Duration:   time.Hour,
			ExpiryTime: now.Add(time.Hour),
		},
		{
			IP:         "10.70.0.10",
			MAC:        "AA:BB:CC:DD:EE:02",
			State:      lease.StateDeclined,
			SubnetID:   "10.70.0.0/24",
			StartTime:  now,
			Duration:   time.Hour,
			ExpiryTime: now.Add(time.Hour),
		},
	} {
		if err := leaseStore.Upsert(context.Background(), item); err != nil {
			t.Fatalf("seed lease %s: %v", item.IP, err)
		}
	}

	pm, err := NewPoolManager([]config.SubnetConfig{
		{
			CIDR:    "10.60.0.0/24",
			Gateway: "10.60.0.1",
			DNS:     []string{"10.60.0.2", "bad"},
			DHCP: config.SubnetDHCPConfig{
				Enabled:   true,
				PoolStart: "10.60.0.10",
				PoolEnd:   "10.60.0.12",
			},
		},
		{
			CIDR:    "10.70.0.0/24",
			Gateway: "10.70.0.1",
			DNS:     []string{"10.70.0.2"},
			DHCP: config.SubnetDHCPConfig{
				Enabled:   true,
				PoolStart: "10.70.0.10",
				PoolEnd:   "10.70.0.12",
			},
		},
	}, 2*time.Hour, leaseStore)
	if err != nil {
		t.Fatalf("new pool manager: %v", err)
	}

	reused, err := pm.Allocate(context.Background(), AllocationRequest{MAC: "AA:BB:CC:DD:EE:01"})
	if err != nil || reused.IP.String() != "10.60.0.10" {
		t.Fatalf("expected existing lease reuse, got %+v err=%v", reused, err)
	}

	specific, err := pm.Allocate(context.Background(), AllocationRequest{RequestedIP: net.IPv4(10, 60, 0, 11)})
	if err != nil || specific.IP.String() != "10.60.0.11" {
		t.Fatalf("expected requested address allocation, got %+v err=%v", specific, err)
	}
	pm.Release(net.IPv4(10, 60, 0, 11), "10.60.0.0/24")
	specific, err = pm.Allocate(context.Background(), AllocationRequest{RequestedIP: net.IPv4(10, 60, 0, 11)})
	if err != nil || specific.IP.String() != "10.60.0.11" {
		t.Fatalf("expected released address reuse, got %+v err=%v", specific, err)
	}

	candidates := pm.selectCandidatePools(AllocationRequest{RelayAddr: net.IPv4(10, 70, 0, 1)})
	if len(candidates) != 1 || candidates[0].id != "10.70.0.0/24" {
		t.Fatalf("unexpected relay candidates: %#v", candidates)
	}
	relayed, err := pm.Allocate(context.Background(), AllocationRequest{RelayAddr: net.IPv4(10, 70, 0, 1)})
	if err != nil || relayed.IP.String() != "10.70.0.11" {
		t.Fatalf("expected relay subnet allocation, got %+v err=%v", relayed, err)
	}

	if OptionName(200) != "unknown" {
		t.Fatalf("unknown option name mismatch")
	}
	if got := parseIPs([]string{"10.0.0.1", "bad", "10.0.0.2"}); len(got) != 2 {
		t.Fatalf("expected only valid ipv4 entries, got %#v", got)
	}
	if back := uint32ToIPv4(ipv4ToUint32([4]byte{10, 1, 2, 3})); back.String() != "10.1.2.3" {
		t.Fatalf("unexpected ipv4 round trip: %s", back)
	}
}
