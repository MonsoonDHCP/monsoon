package auth

import (
	"context"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

type ServiceOptions struct {
	CookieName        string
	SessionDuration   time.Duration
	MaxFailedAttempts int
	LockoutDuration   time.Duration
}

type Service struct {
	store      *storage.Engine
	sessions   *SessionManager
	lockouts   *lockoutTracker
	cookieName string
}

func NewService(store *storage.Engine, options ServiceOptions) *Service {
	cookieName := options.CookieName
	if cookieName == "" {
		cookieName = "monsoon_session"
	}
	return &Service{
		store:      store,
		sessions:   NewSessionManager(options.SessionDuration),
		lockouts:   newLockoutTracker(options.MaxFailedAttempts, options.LockoutDuration),
		cookieName: cookieName,
	}
}

func (s *Service) CookieName() string {
	return s.cookieName
}

func (s *Service) CreateSession(ctx context.Context, identity Identity) (string, time.Time, error) {
	identity.AuthType = "session"
	return s.sessions.Create(ctx, identity)
}

func (s *Service) ValidateSession(ctx context.Context, id string) (Identity, error) {
	return s.sessions.Validate(ctx, id)
}

func (s *Service) RevokeSession(ctx context.Context, id string) {
	s.sessions.Revoke(ctx, id)
}

func (s *Service) RevokeSessionsForUser(ctx context.Context, username string) int {
	return s.sessions.RevokeByUsername(ctx, username)
}
