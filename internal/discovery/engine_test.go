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

func TestTriggerScanAndReadStatus(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", "leases", treeDiscoveryScans, treeDiscoveryMeta})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	if _, err := ipamEngine.UpsertSubnet(context.Background(), ipam.UpsertSubnetInput{
		CIDR:       "10.90.0.0/24",
		Name:       "Scan",
		VLAN:       90,
		Gateway:    "10.90.0.1",
		DHCPEnable: true,
		PoolStart:  "10.90.0.10",
		PoolEnd:    "10.90.0.40",
		LeaseSec:   3600,
	}); err != nil {
		t.Fatalf("upsert subnet: %v", err)
	}
	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:       "10.90.0.11",
		MAC:      "AA:BB:CC:DD:EE:90",
		Hostname: "scan-host",
		State:    lease.StateBound,
		SubnetID: "10.90.0.0/24",
	}); err != nil {
		t.Fatalf("upsert lease: %v", err)
	}

	engine := NewEngineWithOptions(eng, leaseStore, ipamEngine, time.Hour, Options{})
	scanID, err := engine.TriggerScan(context.Background(), ScanRequest{Reason: "test"})
	if err != nil {
		t.Fatalf("trigger scan: %v", err)
	}
	if scanID == "" {
		t.Fatalf("expected scan id")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		results, listErr := engine.ListResults(context.Background(), 1)
		if listErr == nil && len(results) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan result did not persist in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	status := engine.Status(context.Background())
	if !status.SensorOnline {
		t.Fatalf("expected sensor online")
	}
}
