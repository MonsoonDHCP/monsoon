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
	brokerA.Publish(events.Event{Type: "lease.created", Data: map[string]any{"ip": updated.IP}})

	waitFor(t, 4*time.Second, func() bool {
		item, err := leaseStoreB.GetByIP(context.Background(), updated.IP)
		return err == nil && item.MAC == updated.MAC
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
