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
