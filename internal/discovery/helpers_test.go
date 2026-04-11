package discovery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestNormalizeOptionsAndMethods(t *testing.T) {
	opts := NormalizeOptions(Options{
		Methods: []string{" ping ", "PING", "", "dns"},
	})
	if len(opts.Methods) != 2 || opts.Methods[0] != "ping" || opts.Methods[1] != "dns" {
		t.Fatalf("unexpected methods: %#v", opts.Methods)
	}
	if opts.TCPPorts[0] != 22 || opts.MaxConcurrency != 12 || opts.MaxTargetsPerSubnet != 32 {
		t.Fatalf("defaults not applied: %+v", opts)
	}
	if !containsMethod(opts.Methods, "PING") || !hasActiveProbe(opts.Methods) {
		t.Fatalf("expected method helpers to detect normalized methods")
	}
	if got := normalizeMethods([]string{"", " "}); len(got) != 1 || got[0] != "passive" {
		t.Fatalf("expected passive fallback, got %#v", got)
	}
}

func TestKnownHostsConflictsAndHelpers(t *testing.T) {
	leases := []lease.Lease{
		{IP: "10.0.0.10", MAC: "aa:bb:cc:dd:ee:01", Hostname: "lease-host", SubnetID: "10.0.0.0/24"},
		{IP: "10.0.0.20", MAC: "aa:bb:cc:dd:ee:02"},
	}
	reservations := []ipam.Reservation{
		{IP: "10.0.0.20", MAC: "aa:bb:cc:dd:ee:22", Hostname: "reserve-host", SubnetCIDR: "10.0.0.0/24"},
		{IP: "10.0.0.30", MAC: "aa:bb:cc:dd:ee:03", Hostname: "new-reserve", SubnetCIDR: "10.0.0.0/24"},
	}

	known := collectKnownHosts(leases, reservations)
	if known["10.0.0.10"].mac != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("lease mac not normalized: %+v", known["10.0.0.10"])
	}
	if known["10.0.0.20"].hostname != "reserve-host" || known["10.0.0.20"].mac != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("reservation should only backfill blanks: %+v", known["10.0.0.20"])
	}
	if known["10.0.0.30"].source != "reservation" {
		t.Fatalf("expected reservation-only host, got %+v", known["10.0.0.30"])
	}

	conflicts := collectConflictMap(leases, reservations)
	if len(conflicts["10.0.0.20"]) != 2 {
		t.Fatalf("expected conflict set for 10.0.0.20, got %#v", conflicts["10.0.0.20"])
	}

	prev := map[string]ObservedHost{"10.0.0.40": {MAC: "AA:AA:AA:AA:AA:AA"}}
	if state := deriveState("10.0.0.50", false, knownHost{}, probeOutcome{alive: true}, nil); state != "new" {
		t.Fatalf("unexpected state for new live host: %s", state)
	}
	if state := deriveState("10.0.0.20", true, known["10.0.0.20"], probeOutcome{}, nil); state != "known" {
		t.Fatalf("unexpected state for known passive host: %s", state)
	}
	if state := deriveState("10.0.0.40", true, knownHost{mac: "BB:BB:BB:BB:BB:BB"}, probeOutcome{alive: true}, prev); state != "changed" {
		t.Fatalf("unexpected state for changed mac: %s", state)
	}
	if state := deriveState("10.0.0.41", true, knownHost{}, probeOutcome{method: "ping"}, nil); state != "missing" {
		t.Fatalf("unexpected state for missing host: %s", state)
	}
	if pickHostname(" lease-name ", "probe-name") != " lease-name " {
		t.Fatalf("base hostname should win")
	}
	if pickHostname(" ", " probe-name ") != "probe-name" {
		t.Fatalf("probe hostname should be trimmed")
	}
	if normalizeReason(" ") != "manual" || normalizeReason("scheduled") != "scheduled" {
		t.Fatalf("unexpected normalizeReason behavior")
	}
	if compareIPString("10.0.0.2", "10.0.0.10") >= 0 {
		t.Fatalf("numeric ip ordering expected")
	}
}

func TestBuildTargetsPoolBoundsAndPersistence(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", treeDiscoveryScans, treeDiscoveryMeta})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	ipamEngine := ipam.NewEngine(eng, nil)
	subnet, err := ipamEngine.UpsertSubnet(context.Background(), ipam.UpsertSubnetInput{
		CIDR:       "10.30.0.0/24",
		Name:       "Discovery",
		DHCPEnable: true,
		PoolStart:  "10.30.0.10",
		PoolEnd:    "10.30.0.20",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert subnet: %v", err)
	}

	engine := NewEngineWithOptions(eng, nil, ipamEngine, time.Minute, Options{
		Methods:             []string{"ping"},
		MaxTargetsPerSubnet: 3,
		MaxConcurrency:      1,
	})
	start, end, ok := poolBounds(subnet)
	if !ok || start.String() != "10.30.0.10" || end.String() != "10.30.0.20" {
		t.Fatalf("unexpected pool bounds: %v %v %v", start, end, ok)
	}

	targets := engine.buildTargets([]ipam.Subnet{subnet}, map[string]knownHost{
		"10.30.0.5": {ip: "10.30.0.5"},
	})
	if len(targets) != 4 {
		t.Fatalf("targets = %v, want 4", targets)
	}
	if targets[0] != "10.30.0.5" || targets[1] != "10.30.0.10" || targets[3] != "10.30.0.12" {
		t.Fatalf("unexpected target order: %v", targets)
	}

	result := ScanResult{
		ScanID:      "scan-1",
		Status:      "completed",
		StartedAt:   time.Now().UTC().Add(-time.Second),
		CompletedAt: time.Now().UTC(),
		Hosts: []ObservedHost{
			{IP: "10.30.0.10", State: "known", Hostname: "host-a", SeenAt: time.Now().UTC()},
		},
		Conflicts: []Conflict{{IP: "10.30.0.10", MACs: []string{"AA", "BB"}}},
		RogueServers: []RogueServer{
			{IP: "10.30.0.2", Source: "dhcp", Detected: time.Now().UTC()},
		},
	}
	if err := engine.persistResult(result); err != nil {
		t.Fatalf("persist result: %v", err)
	}
	if hosts := engine.loadPreviousHosts(); hosts["10.30.0.10"].Hostname != "host-a" {
		t.Fatalf("expected persisted hosts, got %#v", hosts)
	}
	latest, err := engine.LatestResult(context.Background())
	if err != nil || latest.ScanID != "scan-1" {
		t.Fatalf("latest result = %+v, err = %v", latest, err)
	}
	if got, err := engine.GetResult(context.Background(), "scan-1"); err != nil || got.ScanID != "scan-1" {
		t.Fatalf("get result = %+v, err = %v", got, err)
	}
	if conflicts, err := engine.LatestConflicts(context.Background()); err != nil || len(conflicts) != 1 {
		t.Fatalf("latest conflicts = %#v, err = %v", conflicts, err)
	}
	if rogue, err := engine.LatestRogueServers(context.Background()); err != nil || len(rogue) != 1 {
		t.Fatalf("latest rogue servers = %#v, err = %v", rogue, err)
	}

	reloaded := NewEngine(eng, nil, nil, time.Minute)
	if reloaded.latestID != "scan-1" || reloaded.lastScanAt.IsZero() || reloaded.nextScanAt.IsZero() {
		t.Fatalf("metadata was not loaded: latest=%q last=%v next=%v", reloaded.latestID, reloaded.lastScanAt, reloaded.nextScanAt)
	}
}
