package ipam

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestContainsAndAddressCount(t *testing.T) {
	parent := netip.MustParsePrefix("10.0.0.0/24")
	child := netip.MustParsePrefix("10.0.0.0/25")
	outside := netip.MustParsePrefix("10.0.1.0/24")
	if !Contains(parent, child) {
		t.Fatalf("expected parent to contain child")
	}
	if Contains(parent, outside) {
		t.Fatalf("unexpected containment")
	}
	if count := AddressCount(parent); count != 256 {
		t.Fatalf("address count = %d, want 256", count)
	}
	if count := AddressCount(netip.MustParsePrefix("::/0")); count != ^uint64(0) {
		t.Fatalf("ipv6 address count should saturate, got %d", count)
	}
}

func TestValidateSubnetErrorsAndHelpers(t *testing.T) {
	engine := NewEngine(nil, nil)
	if _, err := engine.validateSubnet(UpsertSubnetInput{CIDR: "bad"}); err == nil {
		t.Fatalf("expected invalid cidr error")
	}
	if _, err := engine.validateSubnet(UpsertSubnetInput{
		CIDR:    "10.0.0.0/24",
		Gateway: "10.0.1.1",
	}); err == nil {
		t.Fatalf("expected gateway outside subnet error")
	}
	if _, err := engine.validateSubnet(UpsertSubnetInput{
		CIDR:       "10.0.0.0/24",
		DHCPEnable: true,
		PoolStart:  "10.0.1.10",
		PoolEnd:    "10.0.0.20",
	}); err == nil {
		t.Fatalf("expected pool outside subnet error")
	}
	if _, err := engine.validateSubnet(UpsertSubnetInput{
		CIDR:       "10.0.0.0/24",
		DHCPEnable: true,
		PoolStart:  "10.0.0.20",
		PoolEnd:    "10.0.0.10",
	}); err == nil {
		t.Fatalf("expected pool ordering error")
	}
	if _, err := engine.validateSubnet(UpsertSubnetInput{
		CIDR: "10.0.0.0/24",
		VLAN: 5000,
	}); err == nil {
		t.Fatalf("expected vlan validation error")
	}

	if got := normalizeDNS([]string{" 1.1.1.1 ", "", "8.8.8.8"}); len(got) != 2 || got[0] != "1.1.1.1" || got[1] != "8.8.8.8" {
		t.Fatalf("unexpected normalized dns: %#v", got)
	}
	if _, _, ok := poolRange(Subnet{}); ok {
		t.Fatalf("empty pool range should be invalid")
	}
}

func TestAddressAndReservationHelpers(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "addresses", "reservations", "leases"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	engine := NewEngine(eng, leaseStore)
	_, err = engine.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.40.0.0/24",
		Name:       "Helpers",
		Gateway:    "10.40.0.1",
		DHCPEnable: true,
		PoolStart:  "10.40.0.10",
		PoolEnd:    "10.40.0.12",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert subnet: %v", err)
	}

	record, err := engine.ValidateAddress(context.Background(), UpsertAddressInput{
		IP:       "10.40.0.11",
		State:    IPStateReserved,
		MAC:      "aa:bb:cc:dd:ee:ff",
		Hostname: "printer",
	})
	if err != nil {
		t.Fatalf("validate address: %v", err)
	}
	if record.SubnetCIDR != "10.40.0.0/24" || record.MAC != "AA:BB:CC:DD:EE:FF" || record.Source != "manual" {
		t.Fatalf("unexpected validated address: %+v", record)
	}
	if _, err := engine.ValidateAddress(context.Background(), UpsertAddressInput{IP: "10.41.0.10"}); err == nil {
		t.Fatalf("expected address outside configured subnet to fail")
	}
	if _, err := engine.ValidateAddress(context.Background(), UpsertAddressInput{IP: "10.40.0.10", State: IPState("weird")}); err == nil {
		t.Fatalf("expected invalid address state error")
	}

	stored, err := engine.UpsertAddress(context.Background(), UpsertAddressInput{
		IP:         "10.40.0.11",
		SubnetCIDR: "10.40.0.0/24",
		State:      IPStateReserved,
		MAC:        "aa:bb:cc:dd:ee:ff",
		Hostname:   "printer",
		Source:     "manual",
	})
	if err != nil {
		t.Fatalf("upsert address: %v", err)
	}
	if got, err := engine.GetStoredAddress(context.Background(), "10.40.0.11"); err != nil || got.IP != stored.IP {
		t.Fatalf("get stored address = %+v, err = %v", got, err)
	}
	if items, err := engine.ListStoredAddresses(context.Background()); err != nil || len(items) != 1 {
		t.Fatalf("list stored addresses = %#v, err = %v", items, err)
	}
	if err := engine.DeleteStoredAddress(context.Background(), "10.40.0.11"); err != nil {
		t.Fatalf("delete stored address: %v", err)
	}
	if _, err := engine.GetStoredAddress(context.Background(), "10.40.0.11"); err == nil {
		t.Fatalf("expected deleted address lookup to fail")
	}

	if _, err := engine.UpsertReservation(context.Background(), UpsertReservationInput{
		MAC:      "AA:BB:CC:DD:EE:01",
		IP:       "10.40.0.12",
		Hostname: "cam-1",
	}); err != nil {
		t.Fatalf("upsert reservation: %v", err)
	}
	if _, err := engine.ValidateReservation(context.Background(), UpsertReservationInput{
		MAC: "AA:BB:CC:DD:EE:02",
		IP:  "10.40.0.12",
	}); err == nil {
		t.Fatalf("expected conflicting reservation ip to fail")
	}
	if _, err := engine.ValidateReservation(context.Background(), UpsertReservationInput{
		MAC:        "AA:BB:CC:DD:EE:03",
		IP:         "10.41.0.12",
		SubnetCIDR: "10.40.0.0/24",
	}); err == nil {
		t.Fatalf("expected reservation outside subnet to fail")
	}
}

func TestListSummariesGetAddressAndMergeHelpers(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", "leases"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	engine := NewEngine(eng, leaseStore)
	_, err = engine.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.50.0.0/24",
		Name:       "Summary",
		DHCPEnable: true,
		PoolStart:  "10.50.0.10",
		PoolEnd:    "10.50.0.12",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert subnet: %v", err)
	}
	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:       "10.50.0.10",
		MAC:      "AA:BB:CC:DD:EE:10",
		State:    lease.StateBound,
		SubnetID: "10.50.0.0/24",
	}); err != nil {
		t.Fatalf("upsert active lease: %v", err)
	}
	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:    "10.50.0.200",
		MAC:   "AA:BB:CC:DD:EE:20",
		State: lease.StateReleased,
	}); err != nil {
		t.Fatalf("upsert unassigned lease: %v", err)
	}

	summaries, err := engine.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected subnet + unassigned summaries, got %#v", summaries)
	}

	address, err := engine.GetAddress(context.Background(), "10.50.0.11")
	if err != nil {
		t.Fatalf("get address fallback: %v", err)
	}
	if address.State != IPStateAvailable || address.SubnetCIDR != "10.50.0.0/24" {
		t.Fatalf("unexpected fallback address: %+v", address)
	}
	if _, err := engine.GetAddress(context.Background(), "10.99.0.10"); err == nil {
		t.Fatalf("expected unknown address lookup to fail")
	}

	merged := mergeAddressRecord(
		AddressRecord{IP: "10.50.0.10", State: IPStateAvailable, Source: "pool"},
		AddressRecord{IP: "10.50.0.10", State: IPStateDHCP, MAC: "AA", Source: "lease"},
	)
	if merged.State != IPStateDHCP || merged.Source != "pool+lease" {
		t.Fatalf("unexpected merged record: %+v", merged)
	}
	conflict := mergeAddressRecord(
		AddressRecord{IP: "10.50.0.10", State: IPStateReserved, MAC: "AA"},
		AddressRecord{IP: "10.50.0.10", State: IPStateDHCP, MAC: "BB"},
	)
	if conflict.State != IPStateConflict {
		t.Fatalf("expected conflict state, got %+v", conflict)
	}
	if rankState(IPStateConflict) <= rankState(IPStateReserved) {
		t.Fatalf("state ranking should prioritize conflicts")
	}
	if got := inferSubnetForAddr(netip.MustParseAddr("10.50.0.20"), []Subnet{{CIDR: "10.50.0.0/24"}}); got != "10.50.0.0/24" {
		t.Fatalf("infer subnet mismatch: %s", got)
	}
	if _, ok := lookupSubnet([]Subnet{{CIDR: "10.50.0.0/24"}}, "10.50.0.0/24"); !ok {
		t.Fatalf("lookup subnet should succeed")
	}
}
