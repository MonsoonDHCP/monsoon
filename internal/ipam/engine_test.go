package ipam

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestUpsertSubnetOverlapRejected(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	ipam := NewEngine(eng, nil)
	_, err = ipam.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.0.1.0/24",
		Name:       "A",
		VLAN:       10,
		Gateway:    "10.0.1.1",
		DHCPEnable: true,
		PoolStart:  "10.0.1.50",
		PoolEnd:    "10.0.1.200",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert A failed: %v", err)
	}

	_, err = ipam.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.0.1.128/25",
		Name:       "B",
		VLAN:       20,
		Gateway:    "10.0.1.129",
		DHCPEnable: true,
		PoolStart:  "10.0.1.150",
		PoolEnd:    "10.0.1.200",
		LeaseSec:   3600,
	})
	if err == nil {
		t.Fatalf("expected overlap rejection")
	}
}

func TestUpsertAndDeleteSubnet(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	ipam := NewEngine(eng, nil)
	created, err := ipam.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.0.2.0/24",
		Name:       "Lab",
		VLAN:       20,
		Gateway:    "10.0.2.1",
		DHCPEnable: true,
		PoolStart:  "10.0.2.50",
		PoolEnd:    "10.0.2.200",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if created.CIDR != "10.0.2.0/24" {
		t.Fatalf("cidr mismatch")
	}

	if err := ipam.DeleteSubnet(context.Background(), created.CIDR); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	list, err := ipam.ListSubnets(context.Background())
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list")
	}
}

func TestReservationCRUD(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	ipam := NewEngine(eng, nil)
	_, err = ipam.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.10.0.0/24",
		Name:       "Ops",
		VLAN:       30,
		Gateway:    "10.10.0.1",
		DHCPEnable: true,
		PoolStart:  "10.10.0.10",
		PoolEnd:    "10.10.0.200",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert subnet failed: %v", err)
	}

	created, err := ipam.UpsertReservation(context.Background(), UpsertReservationInput{
		MAC:      "AA:BB:CC:DD:EE:01",
		IP:       "10.10.0.55",
		Hostname: "printer-1",
	})
	if err != nil {
		t.Fatalf("upsert reservation failed: %v", err)
	}
	if created.SubnetCIDR != "10.10.0.0/24" {
		t.Fatalf("unexpected subnet: %s", created.SubnetCIDR)
	}

	got, err := ipam.GetReservationByMAC(context.Background(), "aa:bb:cc:dd:ee:01")
	if err != nil {
		t.Fatalf("get by mac failed: %v", err)
	}
	if got.IP != "10.10.0.55" {
		t.Fatalf("unexpected ip: %s", got.IP)
	}

	if err := ipam.DeleteReservation(context.Background(), "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatalf("delete reservation failed: %v", err)
	}
}

func TestListAddressesIncludesPoolLeaseAndReservation(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", "leases"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	ipam := NewEngine(eng, leaseStore)
	_, err = ipam.UpsertSubnet(context.Background(), UpsertSubnetInput{
		CIDR:       "10.20.0.0/24",
		Name:       "Users",
		VLAN:       20,
		Gateway:    "10.20.0.1",
		DHCPEnable: true,
		PoolStart:  "10.20.0.10",
		PoolEnd:    "10.20.0.20",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("upsert subnet failed: %v", err)
	}
	if _, err := ipam.UpsertReservation(context.Background(), UpsertReservationInput{
		MAC:        "AA:BB:CC:DD:EE:20",
		IP:         "10.20.0.12",
		Hostname:   "camera-1",
		SubnetCIDR: "10.20.0.0/24",
	}); err != nil {
		t.Fatalf("upsert reservation failed: %v", err)
	}
	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:       "10.20.0.11",
		MAC:      "AA:BB:CC:DD:EE:11",
		Hostname: "laptop-1",
		State:    lease.StateBound,
		SubnetID: "10.20.0.0/24",
	}); err != nil {
		t.Fatalf("upsert lease failed: %v", err)
	}

	addresses, err := ipam.ListAddresses(context.Background(), AddressFilter{SubnetCIDR: "10.20.0.0/24"})
	if err != nil {
		t.Fatalf("list addresses failed: %v", err)
	}
	if len(addresses) < 11 {
		t.Fatalf("expected pool addresses to be expanded, got %d", len(addresses))
	}

	leaseAddress, err := ipam.GetAddress(context.Background(), "10.20.0.11")
	if err != nil {
		t.Fatalf("get address failed: %v", err)
	}
	if leaseAddress.State != IPStateDHCP {
		t.Fatalf("expected dhcp state, got %s", leaseAddress.State)
	}

	reservationAddress, err := ipam.GetAddress(context.Background(), "10.20.0.12")
	if err != nil {
		t.Fatalf("get reservation address failed: %v", err)
	}
	if reservationAddress.MAC != "AA:BB:CC:DD:EE:20" {
		t.Fatalf("reservation mac mismatch: %s", reservationAddress.MAC)
	}
}
