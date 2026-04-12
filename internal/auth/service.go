package auth

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

type ServiceOptions struct {
	AuthType          string
	CookieName        string
	SessionDuration   time.Duration
	MaxFailedAttempts int
	LockoutDuration   time.Duration
}

type Service struct {
	store      *storage.Engine
	sessions   *SessionManager
	lockouts   *lockoutTracker
	authType   string
	cookieName string
	mu         sync.Mutex
}

func NewService(store *storage.Engine, options ServiceOptions) *Service {
	cookieName := options.CookieName
	if cookieName == "" {
		cookieName = "monsoon_session"
	}
	authType := strings.ToLower(strings.TrimSpace(options.AuthType))
	if authType == "" {
		authType = "local"
	}
	return &Service{
		store:      store,
		sessions:   NewSessionManager(store, options.SessionDuration),
		lockouts:   newLockoutTracker(options.MaxFailedAttempts, options.LockoutDuration),
		authType:   authType,
		cookieName: cookieName,
	}
}

func (s *Service) CookieName() string {
	return s.cookieName
}

func (s *Service) SupportsLocalAuth() bool {
	return strings.EqualFold(s.authType, "local")
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
