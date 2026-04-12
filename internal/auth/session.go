package auth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

var ErrSessionNotFound = errors.New("session not found")

const treeSessions = "sessions"

type sessionEntry struct {
	Identity  Identity
	ExpiresAt time.Time
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry
	ttl      time.Duration
	store    *storage.Engine
}

func NewSessionManager(store *storage.Engine, ttl time.Duration) *SessionManager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	manager := &SessionManager{
		sessions: make(map[string]sessionEntry, 128),
		ttl:      ttl,
		store:    store,
	}
	manager.load()
	return manager
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
	if err := m.persistSession(id, sessionEntry{
		Identity:  identity,
		ExpiresAt: expiry,
	}); err != nil {
		delete(m.sessions, id)
		return "", time.Time{}, err
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
		m.deleteSession(id)
		return Identity{}, ErrSessionNotFound
	}
	return entry.Identity, nil
}

func (m *SessionManager) Revoke(_ context.Context, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	m.deleteSession(id)
}

func (m *SessionManager) RevokeByUsername(_ context.Context, username string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	revoked := 0
	for id, entry := range m.sessions {
		if entry.Identity.Username == username {
			delete(m.sessions, id)
			m.deleteSession(id)
			revoked++
		}
	}
	return revoked
}

func (m *SessionManager) load() {
	if m.store == nil {
		return
	}

	now := time.Now().UTC()
	_ = m.store.Iterate(treeSessions, nil, nil, func(key, value []byte) bool {
		var entry sessionEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return true
		}
		if now.After(entry.ExpiresAt) {
			_ = m.store.Delete(treeSessions, key)
			return true
		}
		m.sessions[string(key)] = entry
		return true
	})
}

func (m *SessionManager) persistSession(id string, entry sessionEntry) error {
	if m.store == nil {
		return nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return m.store.Put(treeSessions, []byte(id), raw)
}

func (m *SessionManager) deleteSession(id string) {
	if m.store == nil {
		return
	}
	_ = m.store.Delete(treeSessions, []byte(id))
}
