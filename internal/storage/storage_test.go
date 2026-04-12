package storage

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBTreeOperationsAndIterator(t *testing.T) {
	tree := NewBTree()
	tree.Set([]byte("b"), []byte("2"))
	tree.Set([]byte("a"), []byte("1"))
	tree.Set([]byte("c"), []byte("3"))

	if tree.Len() != 3 {
		t.Fatalf("expected len 3, got %d", tree.Len())
	}
	value, ok := tree.Get([]byte("a"))
	if !ok || string(value) != "1" {
		t.Fatalf("unexpected get result %q ok=%v", value, ok)
	}
	value[0] = 'x'
	again, _ := tree.Get([]byte("a"))
	if string(again) != "1" {
		t.Fatal("expected tree get to return a copy")
	}

	rows := tree.Snapshot()
	if len(rows) != 3 || string(rows[0][0]) != "a" || string(rows[2][0]) != "c" {
		t.Fatalf("unexpected snapshot ordering %#v", rows)
	}

	var keys []string
	tree.Iterate([]byte("b"), []byte("c"), func(k, v []byte) bool {
		keys = append(keys, string(k)+"="+string(v))
		return true
	})
	if strings.Join(keys, ",") != "b=2,c=3" {
		t.Fatalf("unexpected iterate results %#v", keys)
	}

	if !tree.Delete([]byte("b")) {
		t.Fatal("expected delete to succeed")
	}
	if tree.Delete([]byte("missing")) {
		t.Fatal("expected delete miss to return false")
	}

	rows = tree.Snapshot()
	if len(rows) != 2 || string(rows[1][0]) != "c" || string(rows[1][1]) != "3" {
		t.Fatalf("unexpected reverse snapshot tail %#v", rows)
	}
	if len(rows) == 0 || string(rows[0][0]) != "a" {
		t.Fatalf("unexpected forward snapshot head %#v", rows)
	}
}

func TestIndexManagerAndHelpers(t *testing.T) {
	indexes := NewIndexManager()
	fn := func(primaryKey, value []byte) [][]byte {
		return [][]byte{value}
	}
	if err := indexes.Create("by_value", fn); err != nil {
		t.Fatalf("create index: %v", err)
	}
	if err := indexes.Create("by_value", fn); err == nil {
		t.Fatal("expected duplicate index create to fail")
	}

	indexes.Update([]byte("pk1"), nil, []byte("group"))
	indexes.Update([]byte("pk2"), nil, []byte("group"))
	matches := indexes.Scan("by_value", []byte("group"))
	if len(matches) != 2 {
		t.Fatalf("expected two index matches, got %d", len(matches))
	}
	indexes.Update([]byte("pk1"), []byte("group"), nil)
	matches = indexes.Scan("by_value", []byte("group"))
	if len(matches) != 1 || string(matches[0]) != "pk2" {
		t.Fatalf("unexpected index scan results %#v", matches)
	}
	key := composeIndexKey([]byte("group"), []byte("pk"))
	if !bytes.Equal(key, []byte("group\x00pk")) {
		t.Fatalf("unexpected composed index key %q", key)
	}
}

func TestSnapshotWALEngineAndHelpers(t *testing.T) {
	dir := t.TempDir()

	trees := map[string]*BTree{
		"leases": func() *BTree {
			tree := NewBTree()
			tree.Set([]byte("ip-1"), []byte("lease-1"))
			return tree
		}(),
	}
	snapshotPath := filepath.Join(dir, "snapshot.bin")
	if err := WriteSnapshot(snapshotPath, trees); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	restored, err := ReadSnapshot(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if value, ok := restored["leases"].Get([]byte("ip-1")); !ok || string(value) != "lease-1" {
		t.Fatalf("unexpected restored snapshot value %q ok=%v", value, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.snapshot"), []byte("not-a-snapshot"), 0o600); err != nil {
		t.Fatalf("write invalid snapshot: %v", err)
	}
	if _, err := ReadSnapshot(filepath.Join(dir, "bad.snapshot")); err == nil {
		t.Fatal("expected invalid snapshot to fail")
	}

	walDir := filepath.Join(dir, "wal")
	wal, err := OpenWAL(walDir)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	wal.segmentSize = 1
	if err := wal.Append(OpPut, []byte("leases\x1fip-1"), []byte("lease-1")); err != nil {
		t.Fatalf("append wal put: %v", err)
	}
	if err := wal.Append(OpDel, []byte("leases\x1fip-1"), nil); err != nil {
		t.Fatalf("append wal del: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("close wal: %v", err)
	}

	var replayed []string
	wal, err = OpenWAL(walDir)
	if err != nil {
		t.Fatalf("reopen wal: %v", err)
	}
	if err := wal.Replay(func(op OpType, key []byte, value []byte) error {
		replayed = append(replayed, string(op)+":"+string(key)+":"+string(value))
		return nil
	}); err != nil {
		t.Fatalf("replay wal: %v", err)
	}
	if len(replayed) != 2 {
		t.Fatalf("expected two wal entries, got %#v", replayed)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("close reopened wal: %v", err)
	}

	engineDir := filepath.Join(dir, "engine")
	engine, err := OpenEngine(engineDir, []string{"leases"})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	_, txCh, unsubscribe := engine.WatchTx()
	defer unsubscribe()
	if err := engine.Put("leases", []byte("ip-2"), []byte("lease-2")); err != nil {
		t.Fatalf("engine put: %v", err)
	}
	value, err := engine.Get("leases", []byte("ip-2"))
	if err != nil || string(value) != "lease-2" {
		t.Fatalf("unexpected engine get value %q err=%v", value, err)
	}
	if err := engine.Iterate("leases", nil, nil, func(key, value []byte) bool { return false }); err != nil {
		t.Fatalf("engine iterate: %v", err)
	}
	if err := engine.CreateSnapshot(); err != nil {
		t.Fatalf("engine create snapshot: %v", err)
	}
	if err := engine.Delete("leases", []byte("ip-2")); err != nil {
		t.Fatalf("engine delete: %v", err)
	}
	if err := engine.Put("leases", []byte("ip-3"), []byte("lease-3")); err != nil {
		t.Fatalf("engine put second key: %v", err)
	}
	var txEvent TxEvent
	select {
	case txEvent = <-txCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tx event")
	}
	if txEvent.Sequence != 1 || len(txEvent.Mutations) != 1 || txEvent.Mutations[0].Tree != "leases" {
		t.Fatalf("unexpected tx event %#v", txEvent)
	}
	select {
	case txEvent = <-txCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second tx event")
	}
	if txEvent.Sequence != 2 || txEvent.Mutations[0].Op != OpDel {
		t.Fatalf("unexpected second tx event %#v", txEvent)
	}
	select {
	case txEvent = <-txCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for third tx event")
	}
	if txEvent.Sequence != 3 || txEvent.Mutations[0].Op != OpPut || string(txEvent.Mutations[0].Key) != "ip-3" {
		t.Fatalf("unexpected third tx event %#v", txEvent)
	}
	if engine.CurrentSequence() != 3 {
		t.Fatalf("unexpected engine sequence %d", engine.CurrentSequence())
	}
	if _, err := engine.Get("leases", []byte("ip-2")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("engine close: %v", err)
	}
	select {
	case _, ok := <-txCh:
		if ok {
			t.Fatal("expected tx watcher channel to close with engine")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tx watcher close")
	}
	if err := engine.Tx(func(tx *Tx) error { return nil }); err == nil {
		t.Fatal("expected tx on closed engine to fail")
	}

	engine, err = OpenEngine(engineDir, []string{"leases"})
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	if _, err := engine.Get("leases", []byte("ip-2")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted key to stay deleted after replay, got %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}

	restoreSource := filepath.Join(dir, "restore.snapshot")
	restoreTrees := map[string]*BTree{
		"leases": func() *BTree {
			tree := NewBTree()
			tree.Set([]byte("ip-9"), []byte("lease-9"))
			return tree
		}(),
	}
	if err := WriteSnapshot(restoreSource, restoreTrees); err != nil {
		t.Fatalf("write restore snapshot: %v", err)
	}
	engine, err = OpenEngine(engineDir, []string{"leases"})
	if err != nil {
		t.Fatalf("reopen engine for restore: %v", err)
	}
	if err := engine.Put("leases", []byte("stale"), []byte("value")); err != nil {
		t.Fatalf("seed stale key: %v", err)
	}
	if err := engine.RestoreSnapshot(restoreSource); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
	if _, err := engine.Get("leases", []byte("stale")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected stale key to be removed after restore, got %v", err)
	}
	if value, err := engine.Get("leases", []byte("ip-9")); err != nil || string(value) != "lease-9" {
		t.Fatalf("expected restored key, got %q err=%v", value, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close restored engine: %v", err)
	}

	treeKey := makeTreeKey("leases", []byte("ip-3"))
	tree, key, err := splitTreeKey(treeKey)
	if err != nil || tree != "leases" || string(key) != "ip-3" {
		t.Fatalf("unexpected split tree key result tree=%q key=%q err=%v", tree, key, err)
	}
	if _, _, err := splitTreeKey([]byte("bad-key")); err == nil {
		t.Fatal("expected invalid tree key split to fail")
	}

	if got := errorsJoin(errors.New("one"), errors.New("two")).Error(); got != "one; two" {
		t.Fatalf("unexpected joined errors %q", got)
	}
	if errorsJoin() != nil {
		t.Fatal("expected joining zero errors to return nil")
	}

	prefixA := netip.MustParsePrefix("10.0.0.0/24")
	prefixB := netip.MustParsePrefix("10.0.0.128/25")
	if !prefixA.Contains(prefixB.Addr()) {
		t.Fatal("sanity check failed for prefixes")
	}
}
