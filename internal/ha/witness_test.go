package ha

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerWitnessBlocksPromotionWhenPeerOwnsWitness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ha-witness.json")
	now := time.Now().UTC()
	if err := writeWitnessRecord(path, witnessRecord{
		Node:      "beta",
		Priority:  10,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("writeWitnessRecord() error = %v", err)
	}

	manager := NewManager(Config{
		Node:        "alpha",
		Priority:    100,
		WitnessPath: path,
		WitnessHold: time.Minute,
	}, nil, nil, nil)
	manager.role = RoleSecondary

	manager.mu.Lock()
	allowed := manager.canPromoteLocked(now.Add(5 * time.Second))
	fenced := manager.fenced
	reason := manager.fencingReason
	owner := manager.witnessOwner
	manager.mu.Unlock()

	if allowed {
		t.Fatalf("expected witness to block promotion")
	}
	if !fenced {
		t.Fatalf("expected manager to be fenced")
	}
	if reason != "witness_owned_by_peer" {
		t.Fatalf("unexpected fencing reason: %s", reason)
	}
	if owner != "beta" {
		t.Fatalf("unexpected witness owner: %s", owner)
	}
}

func TestManagerWitnessAllowsPromotionAfterWitnessExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ha-witness.json")
	now := time.Now().UTC()
	if err := writeWitnessRecord(path, witnessRecord{
		Node:      "beta",
		Priority:  10,
		UpdatedAt: now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("writeWitnessRecord() error = %v", err)
	}

	manager := NewManager(Config{
		Node:        "alpha",
		Priority:    20,
		WitnessPath: path,
		WitnessHold: 15 * time.Second,
	}, nil, nil, nil)
	manager.role = RoleSecondary

	manager.mu.Lock()
	allowed := manager.canPromoteLocked(now)
	owner := manager.witnessOwner
	fenced := manager.fenced
	manager.mu.Unlock()

	if !allowed {
		t.Fatalf("expected stale witness to allow promotion")
	}
	if fenced {
		t.Fatalf("expected manager not to be fenced")
	}
	if owner != "alpha" {
		t.Fatalf("expected local node to claim witness, got %s", owner)
	}
}

func TestWriteWitnessRecordReplacesExistingFileAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ha-witness.json")

	if err := writeWitnessRecord(path, witnessRecord{
		Node:      "alpha",
		Priority:  10,
		UpdatedAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("first writeWitnessRecord() error = %v", err)
	}
	if err := writeWitnessRecord(path, witnessRecord{
		Node:      "beta",
		Priority:  20,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("second writeWitnessRecord() error = %v", err)
	}

	rec, err := readWitnessRecord(path)
	if err != nil {
		t.Fatalf("readWitnessRecord() error = %v", err)
	}
	if rec.Node != "beta" {
		t.Fatalf("expected latest witness owner beta, got %s", rec.Node)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "ha-witness.json" {
			continue
		}
		t.Fatalf("unexpected temporary witness artifact left behind: %s", entry.Name())
	}
}
