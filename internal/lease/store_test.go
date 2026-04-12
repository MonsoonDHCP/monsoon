package lease

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestStoreUpsertAndLookup(t *testing.T) {
	dir := t.TempDir()
	eng, err := storage.OpenEngine(filepath.Join(dir, "storage"), []string{"leases"})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close()

	store := NewStore(eng)
	l := Lease{
		IP:         "10.0.1.50",
		MAC:        "AA:BB:CC:DD:EE:FF",
		State:      StateBound,
		SubnetID:   "10.0.1.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	if err := store.Upsert(context.Background(), l); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.GetByIP(context.Background(), l.IP)
	if err != nil {
		t.Fatalf("get by ip: %v", err)
	}
	if got.MAC != l.MAC {
		t.Fatalf("mac mismatch: got %s want %s", got.MAC, l.MAC)
	}

	byMAC, err := store.GetByMAC(context.Background(), l.MAC)
	if err != nil {
		t.Fatalf("get by mac: %v", err)
	}
	if len(byMAC) != 1 || byMAC[0].IP != l.IP {
		t.Fatalf("unexpected mac index result")
	}
}

func TestStoreSecondaryIndexesAndDelete(t *testing.T) {
	dir := t.TempDir()
	eng, err := storage.OpenEngine(filepath.Join(dir, "storage"), []string{
		"leases", "leases_by_mac", "leases_by_expiry", "leases_by_subnet", "leases_by_client",
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close()

	store := NewStore(eng)
	now := time.Now().UTC()
	leaseA := Lease{
		IP:         "10.0.2.10",
		MAC:        "aa:bb:cc:dd:ee:10",
		ClientID:   []byte("client-a"),
		State:      StateBound,
		SubnetID:   "10.0.2.0/24",
		StartTime:  now,
		Duration:   time.Hour,
		ExpiryTime: now.Add(30 * time.Minute),
	}
	leaseB := Lease{
		IP:         "10.0.2.11",
		MAC:        "AA:BB:CC:DD:EE:11",
		ClientID:   []byte("client-a"),
		State:      StateRenewing,
		SubnetID:   "10.0.2.0/24",
		StartTime:  now,
		Duration:   time.Hour,
		ExpiryTime: now.Add(90 * time.Minute),
	}
	if err := store.Upsert(context.Background(), leaseA); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	if err := store.Upsert(context.Background(), leaseB); err != nil {
		t.Fatalf("upsert B: %v", err)
	}

	byClient, err := store.GetByClientID(context.Background(), []byte("client-a"))
	if err != nil || len(byClient) != 2 {
		t.Fatalf("get by client = %#v, err = %v", byClient, err)
	}
	bySubnet, err := store.ListBySubnet(context.Background(), "10.0.2.0/24")
	if err != nil || len(bySubnet) != 2 {
		t.Fatalf("list by subnet = %#v, err = %v", bySubnet, err)
	}
	expiring, err := store.ListExpiringBefore(context.Background(), now.Add(time.Hour))
	if err != nil || len(expiring) != 1 || expiring[0].IP != "10.0.2.10" {
		t.Fatalf("expiring leases = %#v, err = %v", expiring, err)
	}

	if err := store.Delete(context.Background(), "10.0.2.10"); err != nil {
		t.Fatalf("delete existing lease: %v", err)
	}
	if err := store.Delete(context.Background(), "10.0.2.250"); err != nil {
		t.Fatalf("delete missing lease should be noop: %v", err)
	}
	all, err := store.ListAll(context.Background())
	if err != nil || len(all) != 1 || all[0].IP != "10.0.2.11" {
		t.Fatalf("list all = %#v, err = %v", all, err)
	}
	byClient, err = store.GetByClientID(context.Background(), []byte("client-a"))
	if err != nil || len(byClient) != 1 || byClient[0].IP != "10.0.2.11" {
		t.Fatalf("client index after delete = %#v, err = %v", byClient, err)
	}
}

func TestStoreWatchMutations(t *testing.T) {
	dir := t.TempDir()
	eng, err := storage.OpenEngine(filepath.Join(dir, "storage"), []string{
		"leases", "leases_by_mac", "leases_by_expiry", "leases_by_subnet", "leases_by_client",
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close()

	store := NewStore(eng)
	_, ch, unsubscribe := store.WatchMutations()
	defer unsubscribe()

	now := time.Now().UTC()
	item := Lease{
		IP:         "10.0.3.10",
		MAC:        "AA:BB:CC:DD:EE:33",
		State:      StateBound,
		SubnetID:   "10.0.3.0/24",
		StartTime:  now,
		Duration:   time.Hour,
		ExpiryTime: now.Add(time.Hour),
	}
	if err := store.Upsert(context.Background(), item); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var putEvent MutationEvent
	select {
	case putEvent = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for put mutation event")
	}
	if putEvent.Op != storage.OpPut || putEvent.Sequence != 1 {
		t.Fatalf("unexpected put event %#v", putEvent)
	}
	if putEvent.Lease == nil || putEvent.Lease.IP != item.IP {
		t.Fatalf("unexpected put lease %#v", putEvent.Lease)
	}

	if err := store.Delete(context.Background(), item.IP); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var delEvent MutationEvent
	select {
	case delEvent = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete mutation event")
	}
	if delEvent.Op != storage.OpDel || delEvent.Sequence != 2 || delEvent.IP != item.IP {
		t.Fatalf("unexpected delete event %#v", delEvent)
	}
	if delEvent.Lease != nil {
		t.Fatalf("expected delete event not to carry lease payload")
	}
}

func TestStoreSilentMutationsDoNotNotifyWatchersOrAdvanceSequence(t *testing.T) {
	dir := t.TempDir()
	eng, err := storage.OpenEngine(filepath.Join(dir, "storage"), []string{
		"leases", "leases_by_mac", "leases_by_expiry", "leases_by_subnet", "leases_by_client",
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close()

	store := NewStore(eng)
	_, ch, unsubscribe := store.WatchMutations()
	defer unsubscribe()

	now := time.Now().UTC()
	item := Lease{
		IP:         "10.0.4.10",
		MAC:        "AA:BB:CC:DD:EE:44",
		State:      StateBound,
		SubnetID:   "10.0.4.0/24",
		StartTime:  now,
		Duration:   time.Hour,
		ExpiryTime: now.Add(time.Hour),
	}
	if err := store.UpsertSilent(context.Background(), item); err != nil {
		t.Fatalf("UpsertSilent() error = %v", err)
	}
	if eng.CurrentSequence() != 0 {
		t.Fatalf("expected silent upsert not to advance engine sequence, got %d", eng.CurrentSequence())
	}
	select {
	case evt := <-ch:
		t.Fatalf("expected no watcher event for silent upsert, got %#v", evt)
	case <-time.After(200 * time.Millisecond):
	}

	got, err := store.GetByIP(context.Background(), item.IP)
	if err != nil || got.MAC != item.MAC {
		t.Fatalf("GetByIP() after silent upsert = %+v, err = %v", got, err)
	}

	if err := store.DeleteSilent(context.Background(), item.IP); err != nil {
		t.Fatalf("DeleteSilent() error = %v", err)
	}
	if eng.CurrentSequence() != 0 {
		t.Fatalf("expected silent delete not to advance engine sequence, got %d", eng.CurrentSequence())
	}
	select {
	case evt := <-ch:
		t.Fatalf("expected no watcher event for silent delete, got %#v", evt)
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := store.GetByIP(context.Background(), item.IP); err == nil {
		t.Fatal("expected lease to be deleted by silent delete")
	}
}
