package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const treeUsers = "users"

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrBootstrapUnavailable = errors.New("admin bootstrap is unavailable")

func (s *Service) EnsureAdmin(ctx context.Context, username string, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	username = normalizeUsername(username)
	if username == "" {
		username = "admin"
	}

	if _, err := s.GetUser(ctx, username); err == nil {
		return nil
	}
	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return err
	}
	if hasUsers {
		return nil
	}

	hash := strings.TrimSpace(passwordHash)
	if hash == "" {
		return errors.New("admin password hash is required")
	}

	return s.upsertUser(ctx, User{
		Username:     username,
		Role:         DefaultRoleAdmin,
		PasswordHash: hash,
	})
}

func (s *Service) BootstrapAdmin(ctx context.Context, username string, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	username = normalizeUsername(username)
	if username == "" {
		username = "admin"
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password is required")
	}
	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return err
	}
	if hasUsers {
		return ErrBootstrapUnavailable
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.upsertUser(ctx, User{
		Username:     username,
		Role:         DefaultRoleAdmin,
		PasswordHash: string(hash),
	})
}

func (s *Service) AuthenticateLocal(ctx context.Context, username string, password string) (Identity, error) {
	username = normalizeUsername(username)
	if err := s.lockouts.Check(ctx, username); err != nil {
		return Identity{}, err
	}
	user, err := s.GetUser(ctx, username)
	if err != nil {
		if lockErr := s.lockouts.RecordFailure(ctx, username); lockErr != nil {
			return Identity{}, lockErr
		}
		return Identity{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if lockErr := s.lockouts.RecordFailure(ctx, username); lockErr != nil {
			return Identity{}, lockErr
		}
		return Identity{}, ErrInvalidCredentials
	}
	s.lockouts.Reset(ctx, username)
	return Identity{
		Username: user.Username,
		Role:     user.Role,
		AuthType: "session",
	}, nil
}

func (s *Service) SetPassword(ctx context.Context, username string, newPassword string) error {
	username = normalizeUsername(username)
	if username == "" || strings.TrimSpace(newPassword) == "" {
		return errors.New("username and new password are required")
	}
	user, err := s.GetUser(ctx, username)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now().UTC()
	return s.upsertUser(ctx, user)
}

func (s *Service) GetUser(_ context.Context, username string) (User, error) {
	key := []byte(normalizeUsername(username))
	raw, err := s.store.Get(treeUsers, key)
	if err != nil {
		return User{}, err
	}
	var out User
	if err := json.Unmarshal(raw, &out); err != nil {
		return User{}, err
	}
	return out, nil
}

func (s *Service) HasUsers(_ context.Context) (bool, error) {
	found := false
	err := s.store.Iterate(treeUsers, nil, nil, func(_, _ []byte) bool {
		found = true
		return false
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return found, nil
}

func (s *Service) upsertUser(_ context.Context, user User) error {
	user.Username = normalizeUsername(user.Username)
	user.Role = sanitizeRole(strings.TrimSpace(user.Role))
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now

	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return s.store.Put(treeUsers, []byte(user.Username), raw)
}

func normalizeUsername(in string) string {
	return strings.ToLower(strings.TrimSpace(in))
}

func isNotFound(err error) bool {
	return errors.Is(err, storage.ErrNotFound)
}
