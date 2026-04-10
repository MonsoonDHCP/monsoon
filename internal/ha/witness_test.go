package ha

import (
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
