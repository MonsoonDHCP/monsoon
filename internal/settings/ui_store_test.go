package settings

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestUIStoreDefaultAndPersist(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"settings"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	store := NewUIStore(eng)
	initial, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if initial.Theme != "system" {
		t.Fatalf("unexpected default theme")
	}

	err = store.Set(context.Background(), UISettings{Theme: "dark", Density: "compact", AutoRefresh: false})
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	updated, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.Theme != "dark" || updated.Density != "compact" || updated.AutoRefresh {
		t.Fatalf("unexpected updated settings: %+v", updated)
	}
}
