package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrAccountLocked = errors.New("account is temporarily locked")

type AccountLockedError struct {
	Until time.Time
}

func (e AccountLockedError) Error() string {
	return "account is temporarily locked"
}

func (e AccountLockedError) Is(target error) bool {
	return target == ErrAccountLocked
}

func (e AccountLockedError) RetryAfter(now time.Time) int {
	if e.Until.IsZero() {
		return 60
	}
	seconds := int(time.Until(e.Until).Seconds())
	if !now.IsZero() {
		seconds = int(e.Until.Sub(now).Seconds())
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

type lockoutEntry struct {
	failures    int
	lockedUntil time.Time
}

type lockoutTracker struct {
	mu                sync.Mutex
	entries           map[string]lockoutEntry
	maxFailedAttempts int
	lockoutDuration   time.Duration
}

func newLockoutTracker(maxFailedAttempts int, lockoutDuration time.Duration) *lockoutTracker {
	if maxFailedAttempts <= 0 {
		maxFailedAttempts = 5
	}
	if lockoutDuration <= 0 {
		lockoutDuration = 15 * time.Minute
	}
	return &lockoutTracker{
		entries:           make(map[string]lockoutEntry, 128),
		maxFailedAttempts: maxFailedAttempts,
		lockoutDuration:   lockoutDuration,
	}
}

func (t *lockoutTracker) Check(_ context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		return nil
	}

	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[username]
	if !ok {
		return nil
	}
	if entry.lockedUntil.IsZero() {
		return nil
	}
	if !now.Before(entry.lockedUntil) {
		delete(t.entries, username)
		return nil
	}
	return AccountLockedError{Until: entry.lockedUntil}
}

func (t *lockoutTracker) RecordFailure(_ context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		return nil
	}

	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.entries[username]
	if !entry.lockedUntil.IsZero() && now.Before(entry.lockedUntil) {
		return AccountLockedError{Until: entry.lockedUntil}
	}
	if !entry.lockedUntil.IsZero() && !now.Before(entry.lockedUntil) {
		entry = lockoutEntry{}
	}

	entry.failures++
	if entry.failures >= t.maxFailedAttempts {
		entry.lockedUntil = now.Add(t.lockoutDuration)
		t.entries[username] = entry
		return AccountLockedError{Until: entry.lockedUntil}
	}
	t.entries[username] = entry
	return nil
}

func (t *lockoutTracker) Reset(_ context.Context, username string) {
	username = normalizeUsername(username)
	if username == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, username)
}
