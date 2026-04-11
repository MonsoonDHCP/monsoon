package config

import "testing"

func TestValidateRejectsWildcardCORSWhenAuthEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.API.REST.CORSOrigins = []string{"*"}

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected wildcard cors to be rejected when auth is enabled")
	}
}

func TestValidateAllowsExplicitCORSOriginsWhenAuthEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.API.REST.CORSOrigins = []string{"http://localhost:5173"}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected explicit origin to be accepted, got %v", err)
	}
}

func TestValidateRejectsNonPositiveAuthRateLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.REST.AuthRateLimit = 0

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected non-positive auth rate limit to be rejected")
	}
}

func TestValidateRejectsNonPositiveLocalAuthLockoutConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Auth.Type = "local"
	cfg.Auth.Local.MaxFailedAttempts = 0
	cfg.Auth.Local.LockoutDuration = Duration{}

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected invalid local auth lockout config to be rejected")
	}
}

func TestValidateRejectsPartialRESTTLSConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.REST.TLSCertFile = "server.crt"

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected partial rest tls config to be rejected")
	}
}

func TestValidateAllowsCompleteRESTTLSConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.REST.TLSCertFile = "server.crt"
	cfg.API.REST.TLSKeyFile = "server.key"

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected complete rest tls config to be accepted, got %v", err)
	}
}

func TestValidateRejectsPartialGRPCTLSConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.GRPC.TLSCertFile = "server.crt"

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected partial grpc tls config to be rejected")
	}
}

func TestValidateRejectsPartialMCPTLSConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.MCP.TLSKeyFile = "server.key"

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected partial mcp tls config to be rejected")
	}
}

func TestValidateRejectsInvalidTrustedProxy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.REST.TrustedProxies = []string{"not-a-proxy"}

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected invalid trusted proxy entry to be rejected")
	}
}

func TestValidateAllowsTrustedProxyCIDRsAndIPs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.API.REST.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8"}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected trusted proxy entries to be accepted, got %v", err)
	}
}
