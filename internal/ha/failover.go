package ha

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/metrics"
)

type Status struct {
	Node                string        `json:"node"`
	Mode                string        `json:"mode"`
	Priority            int           `json:"priority"`
	Role                Role          `json:"role"`
	Peer                PeerState     `json:"peer"`
	PeerNode            string        `json:"peer_node,omitempty"`
	LastHeartbeatAt     time.Time     `json:"last_heartbeat_at,omitempty"`
	HeartbeatLatency    time.Duration `json:"heartbeat_latency,omitempty"`
	SyncLag             time.Duration `json:"sync_lag,omitempty"`
	FailoverCount       int64         `json:"failover_count"`
	ListenAddr          string        `json:"listen_addr"`
	PeerAddr            string        `json:"peer_addr"`
	Fenced              bool          `json:"fenced"`
	FencingReason       string        `json:"fencing_reason,omitempty"`
	WitnessPath         string        `json:"witness_path,omitempty"`
	WitnessOwner        string        `json:"witness_owner,omitempty"`
	ManualStepDownUntil time.Time     `json:"manual_step_down_until,omitempty"`
}

type Manager struct {
	node        string
	mode        string
	priority    int
	peerAddr    string
	listenAddr  string
	interval    time.Duration
	timeout     time.Duration
	leaseSync   bool
	secret      string
	witnessPath string
	witnessHold time.Duration
	store       lease.Store
	broker      *events.Broker
	registry    *metrics.Registry

	mu                  sync.RWMutex
	role                Role
	peer                PeerState
	peerNode            string
	lastHeartbeatAt     time.Time
	heartbeatLatency    time.Duration
	syncLag             time.Duration
	failoverCount       int64
	snapshotDone        bool
	fenced              bool
	fencingReason       string
	witnessOwner        string
	manualStepDownUntil time.Time
	listener            net.Listener
	metricsFailovers    int64
}

type Config struct {
	Node              string
	Mode              string
	Priority          int
	PeerAddress       string
	HeartbeatInterval time.Duration
	FailoverTimeout   time.Duration
	LeaseSync         bool
	SharedSecret      string
	WitnessPath       string
	WitnessHold       time.Duration
}

func NewManager(cfg Config, store lease.Store, broker *events.Broker, registry *metrics.Registry) *Manager {
	interval := cfg.HeartbeatInterval
	if interval <= 0 {
		interval = time.Second
	}
	timeout := cfg.FailoverTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	priority := cfg.Priority
	if priority <= 0 {
		priority = 100
	}
	witnessHold := cfg.WitnessHold
	if witnessHold <= 0 {
		witnessHold = maxDuration(timeout*2, 15*time.Second)
	}
	node := strings.TrimSpace(cfg.Node)
	if node == "" {
		node = "monsoon"
	}
	return &Manager{
		node:        node,
		mode:        normalizeMode(cfg.Mode),
		priority:    priority,
		peerAddr:    strings.TrimSpace(cfg.PeerAddress),
		listenAddr:  deriveListenAddr(cfg.PeerAddress),
		interval:    interval,
		timeout:     timeout,
		leaseSync:   cfg.LeaseSync,
		secret:      cfg.SharedSecret,
		witnessPath: strings.TrimSpace(cfg.WitnessPath),
		witnessHold: witnessHold,
		store:       store,
		broker:      broker,
		registry:    registry,
		role:        RolePrimary,
		peer:        PeerStateUnknown,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", m.listenAddr)
	if err != nil {
		return err
	}
	m.listener = ln
	go m.acceptLoop(ctx)
	go m.heartbeatLoop(ctx)
	go m.watchdogLoop(ctx)
	if m.leaseSync && m.broker != nil {
		go m.leaseSyncLoop(ctx)
	}
	m.refreshWitnessState()
	m.refreshMetrics()
	return nil
}

func (m *Manager) Shutdown() error {
	if m.listener != nil {
		return m.listener.Close()
	}
	return nil
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Status{
		Node:                m.node,
		Mode:                m.mode,
		Priority:            m.priority,
		Role:                m.role,
		Peer:                m.peer,
		PeerNode:            m.peerNode,
		LastHeartbeatAt:     m.lastHeartbeatAt,
		HeartbeatLatency:    m.heartbeatLatency,
		SyncLag:             m.syncLag,
		FailoverCount:       m.failoverCount,
		ListenAddr:          m.listenAddr,
		PeerAddr:            m.peerAddr,
		Fenced:              m.fenced,
		FencingReason:       m.fencingReason,
		WitnessPath:         m.witnessPath,
		WitnessOwner:        m.witnessOwner,
		ManualStepDownUntil: m.manualStepDownUntil,
	}
}

func (m *Manager) TriggerManualFailover(reason string) (Status, error) {
	m.mu.Lock()
	if strings.TrimSpace(m.peerAddr) == "" {
		m.mu.Unlock()
		return Status{}, errors.New("ha peer is not configured")
	}
	if m.peer != PeerStateConnected {
		m.mu.Unlock()
		return Status{}, errors.New("ha peer is not connected")
	}
	if m.role != RolePrimary {
		m.mu.Unlock()
		return Status{}, errors.New("manual failover can only be triggered from the current primary node")
	}
	until := time.Now().UTC().Add(maxDuration(m.timeout*2, time.Hour))
	m.manualStepDownUntil = until
	m.role = RoleSecondary
	status := Status{
		Node:                m.node,
		Mode:                m.mode,
		Priority:            m.priority,
		Role:                m.role,
		Peer:                m.peer,
		PeerNode:            m.peerNode,
		LastHeartbeatAt:     m.lastHeartbeatAt,
		HeartbeatLatency:    m.heartbeatLatency,
		SyncLag:             m.syncLag,
		FailoverCount:       m.failoverCount,
		ListenAddr:          m.listenAddr,
		PeerAddr:            m.peerAddr,
		WitnessPath:         m.witnessPath,
		WitnessOwner:        m.witnessOwner,
		ManualStepDownUntil: m.manualStepDownUntil,
	}
	m.mu.Unlock()

	m.publishHAEvent("ha.failover_requested", map[string]any{
		"node":                   m.node,
		"peer_node":              status.PeerNode,
		"reason":                 strings.TrimSpace(reason),
		"manual_step_down_until": until,
	})
	m.refreshMetrics()
	return status, nil
}

func (m *Manager) acceptLoop(ctx context.Context) {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("ha accept error: %v", err)
			continue
		}
		go m.handleConn(ctx, conn)
	}
}

func (m *Manager) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var envelope struct {
		Type   string `json:"type"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}
	if err := validateSecret(m.secret, envelope.Secret); err != nil {
		return
	}
	switch envelope.Type {
	case "ping":
		var msg heartbeatMessage
		if json.Unmarshal(raw, &msg) != nil {
			return
		}
		m.recordHeartbeat(msg.Node, 0)
		_ = writeWireMessage(conn, heartbeatMessage{
			Type:      "pong",
			Node:      m.node,
			Role:      m.currentRole(),
			Mode:      m.mode,
			Priority:  m.priority,
			Draining:  m.isManualStepDownActive(),
			Timestamp: time.Now().UTC(),
			Secret:    m.secret,
		})
	case "snapshot_request", "lease_upsert", "lease_delete":
		var msg syncMessage
		if json.Unmarshal(raw, &msg) != nil {
			return
		}
		switch msg.Type {
		case "snapshot_request":
			leases := []lease.Lease{}
			if m.store != nil {
				leases, _ = m.store.ListAll(ctx)
			}
			_ = writeSyncMessage(conn, syncMessage{
				Type:      "snapshot",
				Node:      m.node,
				Secret:    m.secret,
				Timestamp: time.Now().UTC(),
				Leases:    leases,
			})
		case "lease_upsert":
			if m.store != nil && msg.Lease != nil {
				_ = m.store.Upsert(ctx, *msg.Lease)
				m.updateSyncLag(time.Since(msg.Timestamp))
			}
			_ = writeSyncMessage(conn, syncMessage{Type: "ack", Secret: m.secret, Timestamp: time.Now().UTC()})
		case "lease_delete":
			if m.store != nil && strings.TrimSpace(msg.DeleteIP) != "" {
				_ = m.store.Delete(ctx, msg.DeleteIP)
				m.updateSyncLag(time.Since(msg.Timestamp))
			}
			_ = writeSyncMessage(conn, syncMessage{Type: "ack", Secret: m.secret, Timestamp: time.Now().UTC()})
		}
	}
}

func (m *Manager) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if strings.TrimSpace(m.peerAddr) == "" {
				continue
			}
			resp, latency, err := dialAndExchange(ctx, m.peerAddr, heartbeatMessage{
				Type:      "ping",
				Node:      m.node,
				Role:      m.currentRole(),
				Mode:      m.mode,
				Priority:  m.priority,
				Draining:  m.isManualStepDownActive(),
				Timestamp: time.Now().UTC(),
				Secret:    m.secret,
			})
			if err != nil {
				m.markPeerDisconnected()
				continue
			}
			if err := validateSecret(m.secret, resp.Secret); err != nil {
				m.markPeerDisconnected()
				continue
			}
			m.recordHeartbeat(resp.Node, latency)
			m.applyElection(resp.Node, resp.Priority, resp.Draining)
			m.refreshWitnessState()
			if m.leaseSync && m.currentRole() == RoleSecondary && !m.snapshotApplied() {
				if err := m.requestSnapshot(ctx); err != nil {
					log.Printf("ha snapshot request failed: %v", err)
				}
			}
		}
	}
}

func (m *Manager) watchdogLoop(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			last := m.lastHeartbeatAt
			role := m.role
			if !last.IsZero() && time.Since(last) > m.timeout {
				m.peer = PeerStateDisconnected
				m.peerNode = ""
				m.snapshotDone = false
				m.manualStepDownUntil = time.Time{}
				if role != RolePrimary {
					if m.canPromoteLocked(time.Now().UTC()) {
						m.role = RolePrimary
						m.failoverCount++
						m.publishHAEvent("ha.role_changed", map[string]any{
							"node":   m.node,
							"role":   m.role,
							"peer":   m.peer,
							"reason": "watchdog_promote",
						})
					}
				}
			}
			m.mu.Unlock()
			m.refreshWitnessState()
			m.refreshMetrics()
		}
	}
}

func (m *Manager) leaseSyncLoop(ctx context.Context) {
	_, ch, unsubscribe := m.broker.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if !strings.HasPrefix(evt.Type, "lease.") {
				continue
			}
			if m.currentRole() != RolePrimary || m.currentPeerState() != PeerStateConnected {
				continue
			}
			ip := ""
			if evt.Data != nil {
				if value, ok := evt.Data["ip"].(string); ok {
					ip = value
				}
			}
			if ip == "" || m.store == nil {
				continue
			}
			item, err := m.store.GetByIP(ctx, ip)
			if err == nil {
				_ = m.pushLeaseUpdate(ctx, item)
				continue
			}
			_ = m.pushLeaseDelete(ctx, ip)
		}
	}
}

func (m *Manager) currentRole() Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.role
}

func (m *Manager) isManualStepDownActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.manualStepDownUntil.IsZero() && time.Now().UTC().Before(m.manualStepDownUntil)
}

func (m *Manager) currentPeerState() PeerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peer
}

func (m *Manager) snapshotApplied() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotDone
}

func (m *Manager) markSnapshotDone() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshotDone = true
}

func (m *Manager) recordHeartbeat(peerNode string, latency time.Duration) {
	m.mu.Lock()
	m.peer = PeerStateConnected
	m.peerNode = strings.TrimSpace(peerNode)
	m.lastHeartbeatAt = time.Now().UTC()
	if latency > 0 {
		m.heartbeatLatency = latency
	}
	m.mu.Unlock()
	m.refreshMetrics()
}

func (m *Manager) markPeerDisconnected() {
	m.mu.Lock()
	m.peer = PeerStateDisconnected
	m.peerNode = ""
	m.snapshotDone = false
	m.manualStepDownUntil = time.Time{}
	m.mu.Unlock()
	m.refreshMetrics()
}

func (m *Manager) applyElection(peerNode string, peerPriority int, peerDraining bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if !m.manualStepDownUntil.IsZero() && !now.Before(m.manualStepDownUntil) {
		m.manualStepDownUntil = time.Time{}
	}
	next := electRole(m.node, peerNode, m.mode, true, m.manualStepDownUntil, now, peerDraining, m.priority, peerPriority)
	if m.role != next {
		m.role = next
		m.publishHAEvent("ha.role_changed", map[string]any{
			"node":      m.node,
			"role":      m.role,
			"peer":      m.peer,
			"peer_node": strings.TrimSpace(peerNode),
		})
	}
}

func (m *Manager) updateSyncLag(lag time.Duration) {
	m.mu.Lock()
	m.syncLag = lag
	m.mu.Unlock()
	m.refreshMetrics()
}

func (m *Manager) refreshMetrics() {
	if m.registry == nil {
		return
	}
	status := m.Status()
	m.registry.SetGauge("monsoon_ha_heartbeat_latency_seconds", nil, status.HeartbeatLatency.Seconds())
	m.registry.SetGauge("monsoon_ha_lease_sync_lag_seconds", nil, status.SyncLag.Seconds())
	m.registry.SetGauge("monsoon_ha_peer_state", map[string]string{"state": string(PeerStateConnected)}, boolToFloat(status.Peer == PeerStateConnected))
	m.registry.SetGauge("monsoon_ha_peer_state", map[string]string{"state": string(PeerStateDisconnected)}, boolToFloat(status.Peer == PeerStateDisconnected))
	m.registry.SetGauge("monsoon_ha_peer_state", map[string]string{"state": string(PeerStateUnknown)}, boolToFloat(status.Peer == PeerStateUnknown))
	m.mu.Lock()
	delta := status.FailoverCount - m.metricsFailovers
	if delta > 0 {
		m.registry.IncCounter("monsoon_ha_failover_total", nil, float64(delta))
		m.metricsFailovers = status.FailoverCount
	}
	m.mu.Unlock()
}

func (m *Manager) refreshWitnessState() {
	path := strings.TrimSpace(m.witnessPath)
	if path == "" {
		return
	}
	now := time.Now().UTC()
	status := m.Status()
	if status.Role != RolePrimary || status.Fenced {
		rec, owned, err := witnessOwner(path, m.witnessHold, now)
		m.mu.Lock()
		if err == nil && owned {
			m.witnessOwner = rec.Node
		} else if err == nil {
			m.witnessOwner = ""
		}
		m.mu.Unlock()
		return
	}
	rec := witnessRecord{
		Node:      m.node,
		Priority:  m.priority,
		UpdatedAt: now,
	}
	if err := writeWitnessRecord(path, rec); err != nil {
		m.mu.Lock()
		m.role = RoleSecondary
		m.fenced = true
		m.fencingReason = "witness_write_failed"
		m.witnessOwner = ""
		m.mu.Unlock()
		m.publishHAEvent("ha.fenced", map[string]any{
			"node":   m.node,
			"reason": "witness_write_failed",
			"error":  err.Error(),
		})
		m.refreshMetrics()
		return
	}
	m.mu.Lock()
	m.witnessOwner = m.node
	m.fenced = false
	m.fencingReason = ""
	m.mu.Unlock()
}

func (m *Manager) canPromoteLocked(now time.Time) bool {
	m.fenced = false
	m.fencingReason = ""
	path := strings.TrimSpace(m.witnessPath)
	if path == "" {
		m.witnessOwner = ""
		return true
	}
	rec, owned, err := witnessOwner(path, m.witnessHold, now)
	if err != nil {
		m.fenced = true
		m.fencingReason = "witness_unavailable"
		m.witnessOwner = ""
		m.publishHAEvent("ha.fenced", map[string]any{
			"node":   m.node,
			"reason": m.fencingReason,
			"error":  err.Error(),
		})
		return false
	}
	if owned && rec.Node != "" && !strings.EqualFold(rec.Node, m.node) {
		m.fenced = true
		m.fencingReason = "witness_owned_by_peer"
		m.witnessOwner = rec.Node
		m.publishHAEvent("ha.fenced", map[string]any{
			"node":         m.node,
			"reason":       m.fencingReason,
			"witness_node": rec.Node,
			"updated_at":   rec.UpdatedAt,
		})
		return false
	}
	if err := writeWitnessRecord(path, witnessRecord{
		Node:      m.node,
		Priority:  m.priority,
		UpdatedAt: now,
	}); err != nil {
		m.fenced = true
		m.fencingReason = "witness_claim_failed"
		m.witnessOwner = ""
		m.publishHAEvent("ha.fenced", map[string]any{
			"node":   m.node,
			"reason": m.fencingReason,
			"error":  err.Error(),
		})
		return false
	}
	m.witnessOwner = m.node
	return true
}

func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func (m *Manager) publishHAEvent(eventType string, data map[string]any) {
	if m.broker == nil {
		return
	}
	m.broker.Publish(events.Event{
		Type: eventType,
		Data: data,
	})
}

func maxDuration(a, b time.Duration) time.Duration {
	if a >= b {
		return a
	}
	return b
}
