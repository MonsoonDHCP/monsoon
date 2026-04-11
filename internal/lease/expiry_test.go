package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sweeperStoreStub struct {
	leases     []Lease
	upserts    map[string]Lease
	upsertErrs map[string]error
}

func (s *sweeperStoreStub) Upsert(_ context.Context, l Lease) error {
	if err := s.upsertErrs[l.IP]; err != nil {
		return err
	}
	if s.upserts == nil {
		s.upserts = map[string]Lease{}
	}
	s.upserts[l.IP] = l
	return nil
}

func (s *sweeperStoreStub) GetByIP(_ context.Context, ip string) (Lease, error) {
	for _, l := range s.leases {
		if l.IP == ip {
			return l, nil
		}
	}
	return Lease{}, errors.New("not found")
}

func (s *sweeperStoreStub) GetByMAC(_ context.Context, mac string) ([]Lease, error) {
	return nil, nil
}

func (s *sweeperStoreStub) GetByClientID(_ context.Context, clientID []byte) ([]Lease, error) {
	return nil, nil
}

func (s *sweeperStoreStub) ListBySubnet(_ context.Context, subnetID string) ([]Lease, error) {
	return nil, nil
}

func (s *sweeperStoreStub) ListExpiringBefore(_ context.Context, t time.Time) ([]Lease, error) {
	return append([]Lease(nil), s.leases...), nil
}

func (s *sweeperStoreStub) Delete(_ context.Context, ip string) error {
	return nil
}

func (s *sweeperStoreStub) ListAll(_ context.Context) ([]Lease, error) {
	return append([]Lease(nil), s.leases...), nil
}

func TestNewSweeperDefaults(t *testing.T) {
	s := NewSweeper(&sweeperStoreStub{}, 0, 0, nil)
	if s.interval != 30*time.Second {
		t.Fatalf("interval = %v, want 30s", s.interval)
	}
	if s.quarantine != 15*time.Minute {
		t.Fatalf("quarantine = %v, want 15m", s.quarantine)
	}
}

func TestSweeperTickTransitionsAndErrors(t *testing.T) {
	now := time.Now().UTC()
	store := &sweeperStoreStub{
		leases: []Lease{
			{IP: "10.0.0.10", State: StateBound},
			{IP: "10.0.0.11", State: StateExpired},
			{IP: "10.0.0.12", State: StateExpired, QuarantineUntil: now.Add(-time.Minute)},
			{IP: "10.0.0.13", State: StateQuarantined, QuarantineUntil: now.Add(-time.Minute)},
			{IP: "10.0.0.14", State: StateOffered},
		},
		upsertErrs: map[string]error{
			"10.0.0.14": errors.New("boom"),
		},
	}
	changed := map[string]Lease{}
	s := NewSweeper(store, time.Second, 2*time.Minute, func(l Lease) {
		changed[l.IP] = l
	})

	s.tick()

	if got := store.upserts["10.0.0.10"]; got.State != StateExpired || got.QuarantineUntil.IsZero() {
		t.Fatalf("bound lease transition mismatch: %+v", got)
	}
	if got := store.upserts["10.0.0.11"]; got.State != StateQuarantined || got.QuarantineUntil.IsZero() {
		t.Fatalf("expired lease transition mismatch: %+v", got)
	}
	if got := store.upserts["10.0.0.12"]; got.State != StateFree {
		t.Fatalf("expired lease should become free: %+v", got)
	}
	if got := store.upserts["10.0.0.13"]; got.State != StateFree {
		t.Fatalf("quarantined lease should become free: %+v", got)
	}
	if _, ok := store.upserts["10.0.0.14"]; ok {
		t.Fatalf("failed upsert should not be recorded")
	}
	if _, ok := changed["10.0.0.14"]; ok {
		t.Fatalf("onChange should not fire for failed upsert")
	}
	if len(changed) != 4 {
		t.Fatalf("changed callbacks = %d, want 4", len(changed))
	}
}
