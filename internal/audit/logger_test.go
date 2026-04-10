package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestLogAndQuery(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeAudit})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	logger := NewLogger(eng)
	if err := logger.Log(context.Background(), Entry{
		Actor:      "admin",
		Action:     "subnet.upsert",
		ObjectType: "subnet",
		ObjectID:   "10.0.1.0/24",
		Source:     "api",
	}); err != nil {
		t.Fatalf("log failed: %v", err)
	}

	results, err := logger.Query(context.Background(), QueryFilter{
		Action: "subnet.upsert",
		Limit:  10,
		To:     time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Actor != "admin" {
		t.Fatalf("actor mismatch: %s", results[0].Actor)
	}
}
