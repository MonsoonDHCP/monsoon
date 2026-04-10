package auth

import (
	"context"
	"path/filepath"
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
	if err := svc.EnsureAdmin(context.Background(), "admin", ""); err != nil {
		t.Fatalf("ensure admin: %v", err)
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
