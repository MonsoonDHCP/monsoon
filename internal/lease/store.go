package lease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

const (
	treeLeases        = "leases"
	treeLeaseByMAC    = "leases_by_mac"
	treeLeaseByExpiry = "leases_by_expiry"
	treeLeaseBySubnet = "leases_by_subnet"
	treeLeaseByClient = "leases_by_client"
	indexSep          = "\x1f"
)

type Store interface {
	Upsert(ctx context.Context, lease Lease) error
	GetByIP(ctx context.Context, ip string) (Lease, error)
	GetByMAC(ctx context.Context, mac string) ([]Lease, error)
	GetByClientID(ctx context.Context, clientID []byte) ([]Lease, error)
	ListBySubnet(ctx context.Context, subnetID string) ([]Lease, error)
	ListExpiringBefore(ctx context.Context, t time.Time) ([]Lease, error)
	Delete(ctx context.Context, ip string) error
	ListAll(ctx context.Context) ([]Lease, error)
}

type EngineStore struct {
	eng *storage.Engine
}

func NewStore(eng *storage.Engine) *EngineStore {
	return &EngineStore{eng: eng}
}

func (s *EngineStore) Upsert(ctx context.Context, lease Lease) error {
	if strings.TrimSpace(lease.IP) == "" {
		return errors.New("lease.ip is required")
	}
	now := time.Now().UTC()
	lease.NormalizeDefaults(now, 12*time.Hour)

	raw, err := json.Marshal(lease)
	if err != nil {
		return err
	}

	old, err := s.GetByIP(ctx, lease.IP)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	return s.eng.Tx(func(tx *storage.Tx) error {
		pk := []byte(lease.IP)
		tx.Put(treeLeases, pk, raw)

		if err == nil {
			s.removeSecondaryIndexes(tx, old)
		}
		s.addSecondaryIndexes(tx, lease)
		return nil
	})
}

func (s *EngineStore) GetByIP(_ context.Context, ip string) (Lease, error) {
	raw, err := s.eng.Get(treeLeases, []byte(ip))
	if err != nil {
		return Lease{}, err
	}
	var l Lease
	if err := json.Unmarshal(raw, &l); err != nil {
		return Lease{}, err
	}
	return l, nil
}

func (s *EngineStore) GetByMAC(ctx context.Context, mac string) ([]Lease, error) {
	return s.fetchBySecondaryPrefix(ctx, treeLeaseByMAC, []byte(normalizeMAC(mac)+indexSep))
}

func (s *EngineStore) GetByClientID(ctx context.Context, clientID []byte) ([]Lease, error) {
	return s.fetchBySecondaryPrefix(ctx, treeLeaseByClient, []byte(string(clientID)+indexSep))
}

func (s *EngineStore) ListBySubnet(ctx context.Context, subnetID string) ([]Lease, error) {
	return s.fetchBySecondaryPrefix(ctx, treeLeaseBySubnet, []byte(subnetID+indexSep))
}

func (s *EngineStore) ListExpiringBefore(ctx context.Context, t time.Time) ([]Lease, error) {
	end := []byte(fmt.Sprintf("%020d", t.Unix()))
	ips := make([]string, 0, 128)
	err := s.eng.Iterate(treeLeaseByExpiry, nil, end, func(_, v []byte) bool {
		ips = append(ips, string(v))
		return true
	})
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	return s.fetchByIPs(ctx, ips)
}

func (s *EngineStore) Delete(ctx context.Context, ip string) error {
	lease, err := s.GetByIP(ctx, ip)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}
	return s.eng.Tx(func(tx *storage.Tx) error {
		tx.Delete(treeLeases, []byte(ip))
		s.removeSecondaryIndexes(tx, lease)
		return nil
	})
}

func (s *EngineStore) ListAll(_ context.Context) ([]Lease, error) {
	out := make([]Lease, 0, 256)
	err := s.eng.Iterate(treeLeases, nil, nil, func(_, v []byte) bool {
		var l Lease
		if json.Unmarshal(v, &l) == nil {
			out = append(out, l)
		}
		return true
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return out, nil
		}
		return nil, err
	}
	return out, nil
}

func (s *EngineStore) addSecondaryIndexes(tx *storage.Tx, l Lease) {
	if l.MAC != "" {
		tx.Put(treeLeaseByMAC, []byte(normalizeMAC(l.MAC)+indexSep+l.IP), []byte(l.IP))
	}
	if len(l.ClientID) > 0 {
		tx.Put(treeLeaseByClient, []byte(string(l.ClientID)+indexSep+l.IP), []byte(l.IP))
	}
	if l.SubnetID != "" {
		tx.Put(treeLeaseBySubnet, []byte(l.SubnetID+indexSep+l.IP), []byte(l.IP))
	}
	if !l.ExpiryTime.IsZero() {
		tx.Put(treeLeaseByExpiry, []byte(fmt.Sprintf("%020d%s%s", l.ExpiryTime.Unix(), indexSep, l.IP)), []byte(l.IP))
	}
}

func (s *EngineStore) removeSecondaryIndexes(tx *storage.Tx, l Lease) {
	if l.MAC != "" {
		tx.Delete(treeLeaseByMAC, []byte(normalizeMAC(l.MAC)+indexSep+l.IP))
	}
	if len(l.ClientID) > 0 {
		tx.Delete(treeLeaseByClient, []byte(string(l.ClientID)+indexSep+l.IP))
	}
	if l.SubnetID != "" {
		tx.Delete(treeLeaseBySubnet, []byte(l.SubnetID+indexSep+l.IP))
	}
	if !l.ExpiryTime.IsZero() {
		tx.Delete(treeLeaseByExpiry, []byte(fmt.Sprintf("%020d%s%s", l.ExpiryTime.Unix(), indexSep, l.IP)))
	}
}

func (s *EngineStore) fetchBySecondaryPrefix(ctx context.Context, tree string, prefix []byte) ([]Lease, error) {
	ips := make([]string, 0, 32)
	err := s.eng.Iterate(tree, prefix, nil, func(k, v []byte) bool {
		if len(prefix) > 0 && string(k[:min(len(k), len(prefix))]) != string(prefix) {
			return false
		}
		ips = append(ips, string(v))
		return true
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return []Lease{}, nil
		}
		return nil, err
	}
	return s.fetchByIPs(ctx, ips)
}

func (s *EngineStore) fetchByIPs(ctx context.Context, ips []string) ([]Lease, error) {
	out := make([]Lease, 0, len(ips))
	for _, ip := range ips {
		l, err := s.GetByIP(ctx, ip)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func normalizeMAC(mac string) string {
	return strings.ToUpper(strings.TrimSpace(mac))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
