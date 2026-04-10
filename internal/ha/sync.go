package ha

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/monsoondhcp/monsoon/internal/lease"
)

type syncMessage struct {
	Type      string        `json:"type"`
	Node      string        `json:"node,omitempty"`
	Secret    string        `json:"secret,omitempty"`
	Timestamp time.Time     `json:"timestamp,omitempty"`
	Leases    []lease.Lease `json:"leases,omitempty"`
	Lease     *lease.Lease  `json:"lease,omitempty"`
	DeleteIP  string        `json:"delete_ip,omitempty"`
}

func writeSyncMessage(conn net.Conn, msg syncMessage) error {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = conn.Write(raw)
	return err
}

func readSyncMessage(conn net.Conn) (syncMessage, error) {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return syncMessage{}, err
	}
	var msg syncMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return syncMessage{}, err
	}
	return msg, nil
}

func (m *Manager) requestSnapshot(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.peerAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := writeSyncMessage(conn, syncMessage{
		Type:      "snapshot_request",
		Node:      m.node,
		Secret:    m.secret,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return err
	}
	resp, err := readSyncMessage(conn)
	if err != nil {
		return err
	}
	if err := validateSecret(m.secret, resp.Secret); err != nil {
		return err
	}
	if resp.Type != "snapshot" {
		return nil
	}
	for _, item := range resp.Leases {
		if err := m.store.Upsert(ctx, item); err != nil {
			return err
		}
	}
	m.updateSyncLag(time.Since(resp.Timestamp))
	m.markSnapshotDone()
	return nil
}

func (m *Manager) pushLeaseUpdate(ctx context.Context, item lease.Lease) error {
	if m.store == nil {
		return nil
	}
	return m.sendSync(ctx, syncMessage{
		Type:      "lease_upsert",
		Node:      m.node,
		Secret:    m.secret,
		Timestamp: time.Now().UTC(),
		Lease:     &item,
	})
}

func (m *Manager) pushLeaseDelete(ctx context.Context, ip string) error {
	return m.sendSync(ctx, syncMessage{
		Type:      "lease_delete",
		Node:      m.node,
		Secret:    m.secret,
		Timestamp: time.Now().UTC(),
		DeleteIP:  ip,
	})
}

func (m *Manager) sendSync(ctx context.Context, msg syncMessage) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.peerAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := writeSyncMessage(conn, msg); err != nil {
		return err
	}
	resp, err := readSyncMessage(conn)
	if err != nil {
		return err
	}
	if err := validateSecret(m.secret, resp.Secret); err != nil {
		return err
	}
	return nil
}
