package ha

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/metrics"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestHAManagersHeartbeatSyncAndFailover(t *testing.T) {
	portA := freePort(t)
	portB := freePort(t)

	storeA := openLeaseEngine(t, filepath.Join(t.TempDir(), "a"))
	defer func() { _ = storeA.Close() }()
	storeB := openLeaseEngine(t, filepath.Join(t.TempDir(), "b"))
	defer func() { _ = storeB.Close() }()

	leaseStoreA := lease.NewStore(storeA)
	leaseStoreB := lease.NewStore(storeB)

	now := time.Now().UTC()
	if err := leaseStoreA.Upsert(context.Background(), lease.Lease{
		IP:         "10.10.10.10",
		MAC:        "AA:BB:CC:DD:EE:FF",
		State:      lease.StateBound,
		SubnetID:   "10.10.10.0/24",
		StartTime:  now,
		Duration:   time.Hour,
		ExpiryTime: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed lease: %v", err)
	}
	if err := leaseStoreB.Upsert(context.Background(), lease.Lease{
		IP:         "10.10.10.200",
		MAC:        "FF:EE:DD:CC:BB:AA",
		State:      lease.StateBound,
		SubnetID:   "10.10.10.0/24",
		StartTime:  now,
		Duration:   time.Hour,
		ExpiryTime: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed stale lease on secondary: %v", err)
	}

	brokerA := events.NewBroker(16)
	brokerB := events.NewBroker(16)
	regA := metrics.NewRegistry()
	regB := metrics.NewRegistry()

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	managerA := NewManager(Config{
		Node:              "alpha",
		Mode:              "active-passive",
		PeerAddress:       "127.0.0.1:" + portB,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreA, brokerA, regA)
	managerB := NewManager(Config{
		Node:              "beta",
		Mode:              "active-passive",
		PeerAddress:       "127.0.0.1:" + portA,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreB, brokerB, regB)
	managerA.listenAddr = "127.0.0.1:" + portA
	managerB.listenAddr = "127.0.0.1:" + portB

	if err := managerA.Start(ctxA); err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	if err := managerB.Start(ctxB); err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	defer func() { _ = managerA.Shutdown() }()
	defer func() { _ = managerB.Shutdown() }()

	waitFor(t, 4*time.Second, func() bool {
		a := managerA.Status()
		b := managerB.Status()
		return a.Peer == PeerStateConnected && b.Peer == PeerStateConnected &&
			a.Role == RolePrimary && b.Role == RoleSecondary
	})

	waitFor(t, 4*time.Second, func() bool {
		_, err := leaseStoreB.GetByIP(context.Background(), "10.10.10.10")
		return err == nil
	})
	waitFor(t, 4*time.Second, func() bool {
		_, err := leaseStoreB.GetByIP(context.Background(), "10.10.10.200")
		return err != nil
	})
	waitFor(t, 4*time.Second, func() bool {
		managerA.mu.RLock()
		primarySeq := managerA.syncSequence
		managerA.mu.RUnlock()
		managerB.mu.RLock()
		defer managerB.mu.RUnlock()
		return managerB.snapshotDone && managerB.appliedSequence == primarySeq
	})

	updated := lease.Lease{
		IP:         "10.10.10.20",
		MAC:        "11:22:33:44:55:66",
		State:      lease.StateBound,
		SubnetID:   "10.10.10.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   2 * time.Hour,
		ExpiryTime: time.Now().UTC().Add(2 * time.Hour),
	}
	if err := leaseStoreA.Upsert(context.Background(), updated); err != nil {
		t.Fatalf("upsert lease A: %v", err)
	}

	waitFor(t, 4*time.Second, func() bool {
		item, err := leaseStoreB.GetByIP(context.Background(), updated.IP)
		if err != nil || item.MAC != updated.MAC {
			return false
		}
		managerA.mu.RLock()
		primarySeq := managerA.syncSequence
		managerA.mu.RUnlock()
		managerB.mu.RLock()
		defer managerB.mu.RUnlock()
		return managerB.appliedSequence == primarySeq
	})

	status, err := managerA.TriggerManualFailover("maintenance")
	if err != nil {
		t.Fatalf("TriggerManualFailover() error = %v", err)
	}
	if status.Role != RoleSecondary {
		t.Fatalf("expected alpha to enter secondary role after manual failover, got %s", status.Role)
	}

	waitFor(t, 4*time.Second, func() bool {
		a := managerA.Status()
		b := managerB.Status()
		return !a.ManualStepDownUntil.IsZero() && a.Role == RoleSecondary && b.Role == RolePrimary
	})

	cancelA()
	_ = managerA.Shutdown()

	waitFor(t, 4*time.Second, func() bool {
		b := managerB.Status()
		return b.Role == RolePrimary && b.Peer == PeerStateDisconnected
	})

	exported := regB.Export()
	if exported == "" {
		t.Fatalf("expected HA metrics to be exported")
	}
}

func TestHASyncUsesLeaseMutationWatchers(t *testing.T) {
	portA := freePort(t)
	portB := freePort(t)

	storeA := openLeaseEngine(t, filepath.Join(t.TempDir(), "a"))
	defer func() { _ = storeA.Close() }()
	storeB := openLeaseEngine(t, filepath.Join(t.TempDir(), "b"))
	defer func() { _ = storeB.Close() }()

	leaseStoreA := lease.NewStore(storeA)
	leaseStoreB := lease.NewStore(storeB)
	brokerA := events.NewBroker(4)
	brokerB := events.NewBroker(4)

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	managerA := NewManager(Config{
		Node:              "alpha",
		Mode:              "active-passive",
		PeerAddress:       "127.0.0.1:" + portB,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreA, brokerA, nil)
	managerB := NewManager(Config{
		Node:              "beta",
		Mode:              "active-passive",
		PeerAddress:       "127.0.0.1:" + portA,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreB, brokerB, nil)
	managerA.listenAddr = "127.0.0.1:" + portA
	managerB.listenAddr = "127.0.0.1:" + portB

	if err := managerA.Start(ctxA); err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	if err := managerB.Start(ctxB); err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	defer func() { _ = managerA.Shutdown() }()
	defer func() { _ = managerB.Shutdown() }()

	waitFor(t, 4*time.Second, func() bool {
		a := managerA.Status()
		b := managerB.Status()
		return a.Role == RolePrimary && b.Role == RoleSecondary && a.Peer == PeerStateConnected && b.Peer == PeerStateConnected
	})

	item := lease.Lease{
		IP:         "10.20.30.40",
		MAC:        "00:11:22:33:44:55",
		State:      lease.StateBound,
		SubnetID:   "10.20.30.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	if err := leaseStoreA.Upsert(context.Background(), item); err != nil {
		t.Fatalf("leaseStoreA.Upsert() error = %v", err)
	}

	waitFor(t, 4*time.Second, func() bool {
		got, err := leaseStoreB.GetByIP(context.Background(), item.IP)
		return err == nil && got.MAC == item.MAC
	})
}

func TestHAIncrementalSequenceClassification(t *testing.T) {
	manager := NewManager(Config{Node: "alpha"}, nil, nil, nil)

	if apply, resync := manager.classifyIncrementalSequence(1); !apply || resync {
		t.Fatalf("expected first sequence to apply without resync, got apply=%v resync=%v", apply, resync)
	}
	manager.setAppliedSequence(1)

	if apply, resync := manager.classifyIncrementalSequence(1); apply || resync {
		t.Fatalf("expected duplicate sequence to be ignored, got apply=%v resync=%v", apply, resync)
	}
	if apply, resync := manager.classifyIncrementalSequence(3); apply || !resync {
		t.Fatalf("expected sequence gap to request resync, got apply=%v resync=%v", apply, resync)
	}
	if manager.snapshotApplied() {
		t.Fatal("expected snapshot state to be invalidated after a sequence gap")
	}
	if manager.appliedSequence != 0 {
		t.Fatalf("expected applied sequence to reset after gap, got %d", manager.appliedSequence)
	}

	manager.markSnapshotDone(7)
	if !manager.snapshotApplied() {
		t.Fatal("expected snapshot state to be restored after resync")
	}
	if manager.appliedSequence != 7 {
		t.Fatalf("expected snapshot sequence to advance applied sequence, got %d", manager.appliedSequence)
	}
	if apply, resync := manager.classifyIncrementalSequence(8); !apply || resync {
		t.Fatalf("expected next in-order incremental update to apply, got apply=%v resync=%v", apply, resync)
	}
}

func TestHAAllocationScopeModes(t *testing.T) {
	manager := NewManager(Config{Node: "alpha", Mode: "active-passive"}, nil, nil, nil)
	manager.role = RolePrimary
	start := uint32(10)
	end := uint32(15)

	scopeStart, scopeEnd, ok := manager.AllocationScope("10.0.0.0/24", start, end)
	if !ok || scopeStart != start || scopeEnd != end {
		t.Fatalf("expected active-passive primary to own full scope, got %d-%d ok=%v", scopeStart, scopeEnd, ok)
	}

	manager.role = RoleSecondary
	if _, _, ok := manager.AllocationScope("10.0.0.0/24", start, end); ok {
		t.Fatal("expected active-passive secondary not to allocate")
	}

	manager.mode = "load-sharing"
	manager.peer = PeerStateConnected
	manager.role = RolePrimary
	scopeStart, scopeEnd, ok = manager.AllocationScope("10.0.0.0/24", start, end)
	if !ok || scopeStart != 10 || scopeEnd != 12 {
		t.Fatalf("expected load-sharing primary lower half, got %d-%d ok=%v", scopeStart, scopeEnd, ok)
	}

	manager.role = RoleSecondary
	scopeStart, scopeEnd, ok = manager.AllocationScope("10.0.0.0/24", start, end)
	if !ok || scopeStart != 13 || scopeEnd != 15 {
		t.Fatalf("expected load-sharing secondary upper half, got %d-%d ok=%v", scopeStart, scopeEnd, ok)
	}

	manager.peer = PeerStateDisconnected
	scopeStart, scopeEnd, ok = manager.AllocationScope("10.0.0.0/24", start, end)
	if !ok || scopeStart != start || scopeEnd != end {
		t.Fatalf("expected disconnected load-sharing node to take full scope, got %d-%d ok=%v", scopeStart, scopeEnd, ok)
	}

	manager.fenced = true
	if _, _, ok := manager.AllocationScope("10.0.0.0/24", start, end); ok {
		t.Fatal("expected fenced node not to allocate")
	}
}

func TestSplitScopeHandlesHighUint32Ranges(t *testing.T) {
	start := ^uint32(0) - 9
	end := ^uint32(0)

	lowerStart, lowerEnd, upperStart, upperEnd := splitScope(start, end)
	if lowerStart != start {
		t.Fatalf("unexpected lowerStart: got=%d want=%d", lowerStart, start)
	}
	if lowerEnd < lowerStart || lowerEnd > end {
		t.Fatalf("unexpected lower range: %d-%d", lowerStart, lowerEnd)
	}
	if upperStart <= lowerEnd {
		t.Fatalf("expected upperStart > lowerEnd, got upperStart=%d lowerEnd=%d", upperStart, lowerEnd)
	}
	if upperEnd != end {
		t.Fatalf("unexpected upperEnd: got=%d want=%d", upperEnd, end)
	}
}

func TestHALoadSharingReplicatesFromBothPeers(t *testing.T) {
	portA := freePort(t)
	portB := freePort(t)

	storeA := openLeaseEngine(t, filepath.Join(t.TempDir(), "a"))
	defer func() { _ = storeA.Close() }()
	storeB := openLeaseEngine(t, filepath.Join(t.TempDir(), "b"))
	defer func() { _ = storeB.Close() }()

	leaseStoreA := lease.NewStore(storeA)
	leaseStoreB := lease.NewStore(storeB)

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	managerA := NewManager(Config{
		Node:              "alpha",
		Mode:              "load-sharing",
		Priority:          10,
		PeerAddress:       "127.0.0.1:" + portB,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreA, events.NewBroker(4), nil)
	managerB := NewManager(Config{
		Node:              "beta",
		Mode:              "load-sharing",
		Priority:          20,
		PeerAddress:       "127.0.0.1:" + portA,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreB, events.NewBroker(4), nil)
	managerA.listenAddr = "127.0.0.1:" + portA
	managerB.listenAddr = "127.0.0.1:" + portB

	if err := managerA.Start(ctxA); err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	if err := managerB.Start(ctxB); err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	defer func() { _ = managerA.Shutdown() }()
	defer func() { _ = managerB.Shutdown() }()

	waitFor(t, 4*time.Second, func() bool {
		return managerA.Status().Peer == PeerStateConnected && managerB.Status().Peer == PeerStateConnected
	})

	itemA := lease.Lease{
		IP:         "10.30.0.10",
		MAC:        "AA:AA:AA:AA:AA:10",
		State:      lease.StateBound,
		SubnetID:   "10.30.0.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	itemB := lease.Lease{
		IP:         "10.30.0.130",
		MAC:        "BB:BB:BB:BB:BB:130",
		State:      lease.StateBound,
		SubnetID:   "10.30.0.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	if err := leaseStoreA.Upsert(context.Background(), itemA); err != nil {
		t.Fatalf("leaseStoreA.Upsert() error = %v", err)
	}
	if err := leaseStoreB.Upsert(context.Background(), itemB); err != nil {
		t.Fatalf("leaseStoreB.Upsert() error = %v", err)
	}

	waitFor(t, 4*time.Second, func() bool {
		_, errA := leaseStoreB.GetByIP(context.Background(), itemA.IP)
		_, errB := leaseStoreA.GetByIP(context.Background(), itemB.IP)
		return errA == nil && errB == nil
	})
}

func TestHALoadSharingInitialSyncMergesExistingLeases(t *testing.T) {
	portA := freePort(t)
	portB := freePort(t)

	storeA := openLeaseEngine(t, filepath.Join(t.TempDir(), "a"))
	defer func() { _ = storeA.Close() }()
	storeB := openLeaseEngine(t, filepath.Join(t.TempDir(), "b"))
	defer func() { _ = storeB.Close() }()

	leaseStoreA := lease.NewStore(storeA)
	leaseStoreB := lease.NewStore(storeB)

	itemA := lease.Lease{
		IP:         "10.40.0.10",
		MAC:        "AA:AA:AA:AA:AA:10",
		State:      lease.StateBound,
		SubnetID:   "10.40.0.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	itemB := lease.Lease{
		IP:         "10.40.0.130",
		MAC:        "BB:BB:BB:BB:BB:130",
		State:      lease.StateBound,
		SubnetID:   "10.40.0.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	if err := leaseStoreA.Upsert(context.Background(), itemA); err != nil {
		t.Fatalf("leaseStoreA.Upsert() error = %v", err)
	}
	if err := leaseStoreB.Upsert(context.Background(), itemB); err != nil {
		t.Fatalf("leaseStoreB.Upsert() error = %v", err)
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	managerA := NewManager(Config{
		Node:              "alpha",
		Mode:              "load-sharing",
		Priority:          10,
		PeerAddress:       "127.0.0.1:" + portB,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreA, events.NewBroker(4), nil)
	managerB := NewManager(Config{
		Node:              "beta",
		Mode:              "load-sharing",
		Priority:          20,
		PeerAddress:       "127.0.0.1:" + portA,
		HeartbeatInterval: 100 * time.Millisecond,
		FailoverTimeout:   350 * time.Millisecond,
		LeaseSync:         true,
		SharedSecret:      "shared",
	}, leaseStoreB, events.NewBroker(4), nil)
	managerA.listenAddr = "127.0.0.1:" + portA
	managerB.listenAddr = "127.0.0.1:" + portB

	if err := managerA.Start(ctxA); err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	if err := managerB.Start(ctxB); err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	defer func() { _ = managerA.Shutdown() }()
	defer func() { _ = managerB.Shutdown() }()

	waitFor(t, 4*time.Second, func() bool {
		return managerA.Status().Peer == PeerStateConnected && managerB.Status().Peer == PeerStateConnected
	})

	waitFor(t, 4*time.Second, func() bool {
		_, errAA := leaseStoreA.GetByIP(context.Background(), itemA.IP)
		_, errAB := leaseStoreA.GetByIP(context.Background(), itemB.IP)
		_, errBA := leaseStoreB.GetByIP(context.Background(), itemA.IP)
		_, errBB := leaseStoreB.GetByIP(context.Background(), itemB.IP)
		return errAA == nil && errAB == nil && errBA == nil && errBB == nil
	})
}

func TestHALoadSharingDoesNotEchoReplicatedLeaseUpsert(t *testing.T) {
	store := openLeaseEngine(t, filepath.Join(t.TempDir(), "load-sharing"))
	defer func() { _ = store.Close() }()
	leaseStore := lease.NewStore(store)

	peerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen peer: %v", err)
	}
	defer peerListener.Close()

	manager := NewManager(Config{
		Node:         "beta",
		Mode:         "load-sharing",
		PeerAddress:  peerListener.Addr().String(),
		LeaseSync:    true,
		SharedSecret: "shared",
	}, leaseStore, nil, nil)
	manager.role = RoleSecondary
	manager.peer = PeerStateConnected

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		manager.leaseSyncLoop(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("leaseSyncLoop did not stop")
		}
	}()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go manager.handleConn(ctx, serverConn)

	item := lease.Lease{
		IP:         "10.50.0.10",
		MAC:        "CC:DD:EE:FF:00:11",
		State:      lease.StateBound,
		SubnetID:   "10.50.0.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	if err := writeSyncMessage(clientConn, syncMessage{
		Type:      "lease_upsert",
		Node:      "alpha",
		Secret:    "shared",
		Timestamp: time.Now().UTC(),
		Sequence:  1,
		Lease:     &item,
	}); err != nil {
		t.Fatalf("writeSyncMessage() error = %v", err)
	}
	resp, err := readSyncMessage(clientConn)
	if err != nil {
		t.Fatalf("readSyncMessage() error = %v", err)
	}
	if resp.Type != "ack" {
		t.Fatalf("expected ack response, got %#v", resp)
	}

	_ = peerListener.(*net.TCPListener).SetDeadline(time.Now().Add(400 * time.Millisecond))
	conn, err := peerListener.Accept()
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected replicated lease update not to echo back to peer")
	}
	if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("unexpected accept error %v", err)
	}
}

func TestHASendSyncPushesSnapshotWhenPeerRequestsResync(t *testing.T) {
	store := openLeaseEngine(t, filepath.Join(t.TempDir(), "primary"))
	defer func() { _ = store.Close() }()
	leaseStore := lease.NewStore(store)

	item := lease.Lease{
		IP:         "10.0.0.10",
		MAC:        "AA:BB:CC:DD:EE:FF",
		State:      lease.StateBound,
		SubnetID:   "10.0.0.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}
	if err := leaseStore.Upsert(context.Background(), item); err != nil {
		t.Fatalf("seed lease: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	manager := NewManager(Config{
		Node:         "alpha",
		PeerAddress:  ln.Addr().String(),
		SharedSecret: "shared",
	}, leaseStore, nil, nil)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		msg, err := readSyncMessage(conn)
		if err != nil {
			_ = conn.Close()
			serverErr <- err
			return
		}
		if msg.Type != "lease_upsert" || msg.Sequence != 2 || msg.Lease == nil || msg.Lease.IP != item.IP {
			_ = conn.Close()
			serverErr <- errString("unexpected lease_upsert payload")
			return
		}
		if err := writeSyncMessage(conn, syncMessage{Type: "resync_required", Secret: "shared", Timestamp: time.Now().UTC(), Sequence: msg.Sequence}); err != nil {
			_ = conn.Close()
			serverErr <- err
			return
		}
		_ = conn.Close()

		conn, err = ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		msg, err = readSyncMessage(conn)
		if err != nil {
			_ = conn.Close()
			serverErr <- err
			return
		}
		if msg.Type != "snapshot_push" || msg.Sequence != 2 || len(msg.Leases) != 1 || msg.Leases[0].IP != item.IP {
			_ = conn.Close()
			serverErr <- errString("unexpected snapshot_push payload")
			return
		}
		if err := writeSyncMessage(conn, syncMessage{Type: "ack", Secret: "shared", Timestamp: time.Now().UTC(), Sequence: msg.Sequence}); err != nil {
			_ = conn.Close()
			serverErr <- err
			return
		}
		_ = conn.Close()
		serverErr <- nil
	}()

	if err := manager.pushLeaseUpdate(context.Background(), item); err != nil {
		t.Fatalf("pushLeaseUpdate: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow: %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func openLeaseEngine(t *testing.T, dir string) *storage.Engine {
	t.Helper()
	engine, err := storage.OpenEngine(dir, []string{
		"leases",
		"leases_by_mac",
		"leases_by_expiry",
		"leases_by_subnet",
		"leases_by_client",
	})
	if err != nil {
		t.Fatalf("OpenEngine() error = %v", err)
	}
	return engine
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort listen: %v", err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("freePort split: %v", err)
	}
	return port
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
