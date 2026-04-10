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

func (s *Service) EnsureAdmin(ctx context.Context, username string, passwordHash string) error {
	username = normalizeUsername(username)
	if username == "" {
		username = "admin"
	}

	if _, err := s.GetUser(ctx, username); err == nil {
		return nil
	}

	hash := strings.TrimSpace(passwordHash)
	if hash == "" {
		defaultHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		hash = string(defaultHash)
	}

	return s.upsertUser(ctx, User{
		Username:     username,
		Role:         DefaultRoleAdmin,
		PasswordHash: hash,
	})
}

func (s *Service) AuthenticateLocal(ctx context.Context, username string, password string) (Identity, error) {
	user, err := s.GetUser(ctx, username)
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return Identity{}, ErrInvalidCredentials
	}
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
