package lease

import (
	"context"
	"log"
	"time"
)

type Sweeper struct {
	store      Store
	interval   time.Duration
	quarantine time.Duration
	onChange   func(Lease)
	stopCh     chan struct{}
	doneCh     chan struct{}
}

func NewSweeper(store Store, interval, quarantine time.Duration, onChange func(Lease)) *Sweeper {
	if interval <= 0 {
		interval = time.Second * 30
	}
	if quarantine <= 0 {
		quarantine = 15 * time.Minute
	}
	return &Sweeper{
		store:      store,
		interval:   interval,
		quarantine: quarantine,
		onChange:   onChange,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

func (s *Sweeper) Start() {
	go s.loop()
}

func (s *Sweeper) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *Sweeper) loop() {
	defer close(s.doneCh)
	t := time.NewTicker(s.interval)
	defer t.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Sweeper) tick() {
	now := time.Now().UTC()
	leases, err := s.store.ListExpiringBefore(context.Background(), now)
	if err != nil {
		log.Printf("lease sweeper list failed: %v", err)
		return
	}

	for _, l := range leases {
		changed := false
		switch l.State {
		case StateBound, StateRenewing, StateOffered:
			l.State = StateExpired
			l.QuarantineUntil = now.Add(s.quarantine)
			l.UpdatedAt = now
			changed = true
		case StateExpired:
			if l.QuarantineUntil.IsZero() {
				l.QuarantineUntil = now.Add(s.quarantine)
				l.State = StateQuarantined
				l.UpdatedAt = now
				changed = true
			} else if now.After(l.QuarantineUntil) {
				l.State = StateFree
				l.UpdatedAt = now
				changed = true
			}
		case StateQuarantined:
			if !l.QuarantineUntil.IsZero() && now.After(l.QuarantineUntil) {
				l.State = StateFree
				l.UpdatedAt = now
				changed = true
			}
		}
		if changed {
			if err := s.store.Upsert(context.Background(), l); err != nil {
				log.Printf("lease sweeper upsert failed (%s): %v", l.IP, err)
				continue
			}
			if s.onChange != nil {
				s.onChange(l)
			}
		}
	}
}
