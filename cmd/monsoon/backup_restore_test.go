package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestResolveRestorePathRestrictsToBackupDirectory(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backups")
	if got, err := resolveRestorePath(backupDir, rest.RestoreRequest{Name: "safe.snapshot"}); err != nil || filepath.Base(got) != "safe.snapshot" {
		t.Fatalf("resolve by name = %q, err=%v", got, err)
	}
	if _, err := resolveRestorePath(backupDir, rest.RestoreRequest{Name: "..\\escape.snapshot"}); err == nil {
		t.Fatalf("expected path separators in name to be rejected")
	}
	outside := filepath.Join(t.TempDir(), "outside.snapshot")
	if _, err := resolveRestorePath(backupDir, rest.RestoreRequest{Path: outside}); err == nil {
		t.Fatalf("expected outside path to be rejected")
	}
}

func TestRestoreBackupFromRequestRestoresSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	engine, err := storage.OpenEngine(filepath.Join(dataDir, "storage"), []string{"leases"})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer engine.Close()

	if err := engine.Put("leases", []byte("stale"), []byte("value")); err != nil {
		t.Fatalf("seed stale data: %v", err)
	}

	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	snapshotPath := filepath.Join(backupDir, "restore.snapshot")
	trees := map[string]*storage.BTree{
		"leases": func() *storage.BTree {
			tree := storage.NewBTree()
			tree.Set([]byte("ip-1"), []byte("lease-1"))
			return tree
		}(),
	}
	if err := storage.WriteSnapshot(snapshotPath, trees); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dataDir
	cfg.Backup.Auto.Path = backupDir
	restored, err := restoreBackupFromRequest(context.Background(), engine, cfg, "", rest.RestoreRequest{Name: "restore.snapshot"})
	if err != nil {
		t.Fatalf("restore backup: %v", err)
	}
	if restored.Name != "restore.snapshot" {
		t.Fatalf("unexpected restore metadata: %+v", restored)
	}
	if _, err := engine.Get("leases", []byte("stale")); err == nil {
		t.Fatalf("expected stale key to be removed after restore")
	}
	if value, err := engine.Get("leases", []byte("ip-1")); err != nil || string(value) != "lease-1" {
		t.Fatalf("expected restored key, got %q err=%v", value, err)
	}
}
