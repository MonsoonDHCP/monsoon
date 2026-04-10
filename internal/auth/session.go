package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrSessionNotFound = errors.New("session not found")

type sessionEntry struct {
	Identity  Identity
	ExpiresAt time.Time
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry
	ttl      time.Duration
}

func NewSessionManager(ttl time.Duration) *SessionManager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &SessionManager{
		sessions: make(map[string]sessionEntry, 128),
		ttl:      ttl,
	}
}

func (m *SessionManager) Create(_ context.Context, identity Identity) (string, time.Time, error) {
	id, err := randomHex(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expiry := time.Now().UTC().Add(m.ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = sessionEntry{
		Identity:  identity,
		ExpiresAt: expiry,
	}
	return id, expiry, nil
}

func (m *SessionManager) Validate(_ context.Context, id string) (Identity, error) {
	now := time.Now().UTC()

	m.mu.RLock()
	entry, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return Identity{}, ErrSessionNotFound
	}
	if now.After(entry.ExpiresAt) {
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return Identity{}, ErrSessionNotFound
	}
	return entry.Identity, nil
}

func (m *SessionManager) Revoke(_ context.Context, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

func (m *SessionManager) CleanupExpired() {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, entry := range m.sessions {
		if now.After(entry.ExpiresAt) {
			delete(m.sessions, id)
		}
	}
}
