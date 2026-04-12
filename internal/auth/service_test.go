package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestLocalAuthAndSessionFlow(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{CookieName: "test_session", SessionDuration: time.Hour})
	if err := svc.BootstrapAdmin(context.Background(), "admin", "admin"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	identity, err := svc.AuthenticateLocal(context.Background(), "admin", "admin")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if identity.Role != DefaultRoleAdmin {
		t.Fatalf("unexpected role: %s", identity.Role)
	}

	sessionID, _, err := svc.CreateSession(context.Background(), identity)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	validated, err := svc.ValidateSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("validate session: %v", err)
	}
	if validated.Username != "admin" {
		t.Fatalf("unexpected username: %s", validated.Username)
	}
}

func TestBootstrapAdminOnlyAllowedOnce(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{})
	if err := svc.BootstrapAdmin(context.Background(), "admin", "first-pass"); err != nil {
		t.Fatalf("first bootstrap failed: %v", err)
	}
	if err := svc.BootstrapAdmin(context.Background(), "admin2", "second-pass"); err != ErrBootstrapUnavailable {
		t.Fatalf("expected ErrBootstrapUnavailable, got %v", err)
	}
}

func TestEnsureAdminDoesNotCreateExtraAdminWhenUsersAlreadyExist(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{})
	if err := svc.BootstrapAdmin(context.Background(), "admin", "first-pass"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	if err := svc.EnsureAdmin(context.Background(), "shadow-admin", "$2a$10$abcdefghijklmnopqrstuv"); err != nil {
		t.Fatalf("EnsureAdmin() error = %v", err)
	}
	if _, err := svc.GetUser(context.Background(), "shadow-admin"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected EnsureAdmin not to create extra admin, got err=%v", err)
	}
}

func TestBootstrapAdminIsSerialized(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{})
	var wg sync.WaitGroup
	results := make(chan error, 2)

	for _, username := range []string{"admin-a", "admin-b"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			results <- svc.BootstrapAdmin(context.Background(), name, "secret-pass")
		}(username)
	}
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBootstrapUnavailable):
			conflicts++
		default:
			t.Fatalf("unexpected bootstrap error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one success and one conflict, got successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestRevokeSessionsForUserRemovesOnlyMatchingSessions(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{SessionDuration: time.Hour})

	adminSession, _, err := svc.CreateSession(context.Background(), Identity{Username: "admin", Role: DefaultRoleAdmin})
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	secondAdminSession, _, err := svc.CreateSession(context.Background(), Identity{Username: "admin", Role: DefaultRoleAdmin})
	if err != nil {
		t.Fatalf("create second admin session: %v", err)
	}
	operatorSession, _, err := svc.CreateSession(context.Background(), Identity{Username: "operator", Role: DefaultRoleOperator})
	if err != nil {
		t.Fatalf("create operator session: %v", err)
	}

	if revoked := svc.RevokeSessionsForUser(context.Background(), "admin"); revoked != 2 {
		t.Fatalf("expected 2 revoked admin sessions, got %d", revoked)
	}
	if _, err := svc.ValidateSession(context.Background(), adminSession); err == nil {
		t.Fatalf("expected first admin session to be revoked")
	}
	if _, err := svc.ValidateSession(context.Background(), secondAdminSession); err == nil {
		t.Fatalf("expected second admin session to be revoked")
	}
	if _, err := svc.ValidateSession(context.Background(), operatorSession); err != nil {
		t.Fatalf("expected operator session to remain valid, got %v", err)
	}
}

func TestAuthenticateLocalLocksAfterRepeatedFailures(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{
		MaxFailedAttempts: 2,
		LockoutDuration:   time.Minute,
	})
	if err := svc.BootstrapAdmin(context.Background(), "admin", "correct-pass"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	if _, err := svc.AuthenticateLocal(context.Background(), "admin", "wrong-pass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials on first failure, got %v", err)
	}
	if _, err := svc.AuthenticateLocal(context.Background(), "admin", "wrong-pass"); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("expected account to be locked on second failure, got %v", err)
	}
	if _, err := svc.AuthenticateLocal(context.Background(), "admin", "correct-pass"); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("expected account to remain locked, got %v", err)
	}
}

func TestTokenLifecycle(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{})

	publicToken, secret, err := svc.CreateToken(context.Background(), "automation", DefaultRoleOperator, nil, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if publicToken.ID == "" || secret == "" {
		t.Fatalf("expected token data")
	}

	identity, err := svc.AuthenticateBearer(context.Background(), secret)
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}
	if identity.Role != DefaultRoleOperator {
		t.Fatalf("unexpected token role: %s", identity.Role)
	}

	if err := svc.RevokeToken(context.Background(), publicToken.ID); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if _, err := svc.AuthenticateBearer(context.Background(), secret); err == nil {
		t.Fatalf("expected revoked token to fail")
	}
}

func TestSessionsPersistAcrossServiceRestart(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{treeUsers, treeTokens, treeTokensByHash, treeSessions})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	svc := NewService(eng, ServiceOptions{SessionDuration: time.Hour})
	sessionID, _, err := svc.CreateSession(context.Background(), Identity{Username: "admin", Role: DefaultRoleAdmin})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	reloaded := NewService(eng, ServiceOptions{SessionDuration: time.Hour})
	identity, err := reloaded.ValidateSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("validate persisted session: %v", err)
	}
	if identity.Username != "admin" || identity.Role != DefaultRoleAdmin {
		t.Fatalf("unexpected persisted identity: %+v", identity)
	}
}

func TestServiceSupportsLocalAuthByConfiguredMode(t *testing.T) {
	localSvc := NewService(nil, ServiceOptions{})
	if !localSvc.SupportsLocalAuth() {
		t.Fatalf("expected default auth type to support local auth")
	}

	ldapSvc := NewService(nil, ServiceOptions{AuthType: "ldap"})
	if ldapSvc.SupportsLocalAuth() {
		t.Fatalf("expected ldap auth type to disable local auth support")
	}
	if ldapSvc.AuthType() != "ldap" {
		t.Fatalf("unexpected auth type: %q", ldapSvc.AuthType())
	}
}
