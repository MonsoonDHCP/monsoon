package migrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestRunnerCSVApply(t *testing.T) {
	t.Parallel()

	runner, ipamEngine, leaseStore := newTestRunner(t)
	ctx := context.Background()

	subnets := writeCSV(t, "subnets.csv", "cidr,name,vlan,gateway,dns,dhcp_enabled,pool_start,pool_end,lease_time\n10.0.10.0/24,Guest,100,10.0.10.1,\"1.1.1.1,8.8.8.8\",true,10.0.10.10,10.0.10.100,4h\n")
	addresses := writeCSV(t, "addresses.csv", "ip,subnet_cidr,state,mac,hostname,source\n10.0.10.5,10.0.10.0/24,conflict,AA:BB:CC:DD:EE:01,rogue-host,import\n")
	reservations := writeCSV(t, "reservations.csv", "mac,ip,hostname,subnet_cidr\nAA:BB:CC:DD:EE:02,10.0.10.20,printer,10.0.10.0/24\n")
	leases := writeCSV(t, "leases.csv", "ip,mac,hostname,state,subnet_id,start_time,duration\n10.0.10.30,AA:BB:CC:DD:EE:03,laptop,bound,10.0.10.0/24,2026-04-10T10:00:00Z,2h\n")

	report, err := runner.Run(ctx, Options{
		Source: SourceCSV,
		CSV: CSVOptions{
			SubnetsPath:      subnets,
			AddressesPath:    addresses,
			ReservationsPath: reservations,
			LeasesPath:       leases,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Files) != 4 {
		t.Fatalf("expected 4 file reports, got %d", len(report.Files))
	}

	subnet, err := ipamEngine.GetSubnet(ctx, "10.0.10.0/24")
	if err != nil {
		t.Fatalf("GetSubnet() error = %v", err)
	}
	if subnet.Name != "Guest" {
		t.Fatalf("unexpected subnet name %q", subnet.Name)
	}

	addr, err := ipamEngine.GetStoredAddress(ctx, "10.0.10.5")
	if err != nil {
		t.Fatalf("GetStoredAddress() error = %v", err)
	}
	if addr.State != ipam.IPStateConflict {
		t.Fatalf("unexpected address state %q", addr.State)
	}

	reservation, err := ipamEngine.GetReservationByMAC(ctx, "AA:BB:CC:DD:EE:02")
	if err != nil {
		t.Fatalf("GetReservationByMAC() error = %v", err)
	}
	if reservation.IP != "10.0.10.20" {
		t.Fatalf("unexpected reservation ip %q", reservation.IP)
	}

	leaseRecord, err := leaseStore.GetByIP(ctx, "10.0.10.30")
	if err != nil {
		t.Fatalf("GetByIP() error = %v", err)
	}
	if leaseRecord.State != lease.StateBound {
		t.Fatalf("unexpected lease state %q", leaseRecord.State)
	}
}

func TestRunnerCSVDryRunAndSkip(t *testing.T) {
	t.Parallel()

	runner, ipamEngine, leaseStore := newTestRunner(t)
	ctx := context.Background()

	if _, err := ipamEngine.UpsertSubnet(ctx, ipam.UpsertSubnetInput{
		CIDR:       "10.0.20.0/24",
		Name:       "Existing",
		DHCPEnable: true,
		PoolStart:  "10.0.20.10",
		PoolEnd:    "10.0.20.200",
	}); err != nil {
		t.Fatalf("seed subnet error = %v", err)
	}
	if err := leaseStore.Upsert(ctx, lease.Lease{
		IP:        "10.0.20.50",
		MAC:       "AA:BB:CC:00:00:50",
		State:     lease.StateBound,
		SubnetID:  "10.0.20.0/24",
		StartTime: time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC),
		Duration:  time.Hour,
	}); err != nil {
		t.Fatalf("seed lease error = %v", err)
	}

	subnets := writeCSV(t, "subnets.csv", "cidr,name,pool_start,pool_end\n10.0.20.0/24,Existing,10.0.20.10,10.0.20.200\n")
	leases := writeCSV(t, "leases.csv", "ip,mac,subnet_id,state\n10.0.20.50,AA:BB:CC:00:00:50,10.0.20.0/24,bound\n")

	dryReport, dryErr := runner.Run(ctx, Options{
		Source: SourceCSV,
		DryRun: true,
		CSV:    CSVOptions{SubnetsPath: subnets},
	})
	if dryErr != nil {
		t.Fatalf("dry run error = %v", dryErr)
	}
	if dryReport.Files[0].Applied != 1 {
		t.Fatalf("expected dry run applied=1, got %d", dryReport.Files[0].Applied)
	}

	applyReport, applyErr := runner.Run(ctx, Options{
		Source:         SourceCSV,
		ConflictPolicy: ConflictSkip,
		CSV: CSVOptions{
			SubnetsPath: subnets,
			LeasesPath:  leases,
		},
	})
	if applyErr != nil {
		t.Fatalf("skip run error = %v", applyErr)
	}
	if applyReport.Files[0].Skipped != 1 {
		t.Fatalf("expected subnet skip count 1, got %d", applyReport.Files[0].Skipped)
	}
	if applyReport.Files[1].Skipped != 1 {
		t.Fatalf("expected lease skip count 1, got %d", applyReport.Files[1].Skipped)
	}
}

func newTestRunner(t *testing.T) (*Runner, *ipam.Engine, lease.Store) {
	t.Helper()

	dataDir := t.TempDir()
	eng, err := storage.OpenEngine(filepath.Join(dataDir, "storage"), []string{
		"leases",
		"subnets",
		"addresses",
		"reservations",
		"vlans",
		"audit",
		"settings",
		"users",
		"api_tokens",
		"api_tokens_by_hash",
		"discovery_scans",
		"discovery_meta",
	})
	if err != nil {
		t.Fatalf("OpenEngine() error = %v", err)
	}
	t.Cleanup(func() {
		_ = eng.Close()
	})

	leaseStore := lease.NewStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	return NewRunner(ipamEngine, leaseStore), ipamEngine, leaseStore
}

func writeCSV(t *testing.T, name string, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
	return path
}
