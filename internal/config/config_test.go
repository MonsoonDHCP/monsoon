package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDurationMarshalAndUnmarshalYAML(t *testing.T) {
	value := Duration{}
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: "15m"}
	if err := value.UnmarshalYAML(node); err != nil {
		t.Fatalf("unmarshal duration: %v", err)
	}
	if got := value.Duration.String(); got != "15m0s" {
		t.Fatalf("unexpected parsed duration %q", got)
	}
	marshaled, err := value.MarshalYAML()
	if err != nil {
		t.Fatalf("marshal duration: %v", err)
	}
	if marshaled != "15m0s" {
		t.Fatalf("unexpected marshaled duration %#v", marshaled)
	}
}

func TestDurationUnmarshalRejectsInvalidValue(t *testing.T) {
	value := Duration{}
	if err := value.UnmarshalYAML(&yaml.Node{Kind: yaml.MappingNode, Value: "bad"}); err == nil {
		t.Fatal("expected non-scalar duration to fail")
	}
	if err := value.UnmarshalYAML(&yaml.Node{Kind: yaml.ScalarNode, Value: "nope"}); err == nil {
		t.Fatal("expected invalid duration string to fail")
	}
}

func TestCloneAndNewDefaultsReturnIndependentConfigs(t *testing.T) {
	cfg := DefaultConfig()
	clone := cfg.Clone()
	clone.Server.Hostname = "changed"
	if cfg.Server.Hostname == clone.Server.Hostname {
		t.Fatal("expected clone mutation not to affect original")
	}
}

func TestLoadMissingFileUsesDefaultsAndEnvOverrides(t *testing.T) {
	t.Setenv("MONSOON_SERVER_HOSTNAME", "env-host")
	t.Setenv("MONSOON_API_REST_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load missing config: %v", err)
	}
	if cfg.Server.Hostname != "env-host" {
		t.Fatalf("expected env hostname override, got %q", cfg.Server.Hostname)
	}
	if len(cfg.API.REST.TrustedProxies) != 2 {
		t.Fatalf("expected trusted proxies override, got %#v", cfg.API.REST.TrustedProxies)
	}
}

func TestLoadReadsYAMLAndValidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monsoon.yaml")
	body := `
server:
  hostname: yaml-host
  data_dir: /tmp/monsoon
  log_level: info
  log_format: json
api:
  rest:
    enabled: true
    listen: :8067
    rate_limit: 100
    auth_rate_limit: 5
  grpc:
    enabled: true
    listen: :9067
  websocket:
    enabled: true
  mcp:
    enabled: true
    listen: :7067
dhcp:
  default_lease_time: 12h
  max_lease_time: 24h
auth:
  enabled: true
  type: local
  local:
    admin_username: admin
    max_failed_attempts: 5
    lockout_duration: 15m
  session:
    duration: 24h
    cookie_name: monsoon_session
    secure: true
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Hostname != "yaml-host" {
		t.Fatalf("unexpected hostname %q", cfg.Server.Hostname)
	}
}

func TestWriteDefaultRespectsOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monsoon.yaml")
	if err := WriteDefault(path, false); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}
	if err := WriteDefault(path, false); err == nil {
		t.Fatal("expected overwrite=false to reject existing config")
	}
	if err := WriteDefault(path, true); err != nil {
		t.Fatalf("expected overwrite=true to succeed, got %v", err)
	}
}
