package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestManagerReloadAndCallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monsoon.yaml")
	if err := WriteDefault(path, true); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	manager, err := NewManager(path)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	callbackCount := 0
	manager.RegisterOnReload(func(cfg *Config) {
		callbackCount++
		if cfg.Server.Hostname != "reloaded-host" {
			t.Fatalf("unexpected callback config hostname %q", cfg.Server.Hostname)
		}
	})
	manager.RegisterOnReload(nil)

	cfg := manager.Get()
	cfg.Server.Hostname = "reloaded-host"
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal reloaded config: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write reloaded config: %v", err)
	}

	if err := manager.Reload(); err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if callbackCount != 1 {
		t.Fatalf("expected one reload callback, got %d", callbackCount)
	}

	reloaded := manager.Get()
	if reloaded.Server.Hostname != "reloaded-host" {
		t.Fatalf("expected reloaded hostname, got %q", reloaded.Server.Hostname)
	}
	reloaded.Server.Hostname = "mutated-copy"
	if manager.Get().Server.Hostname != "reloaded-host" {
		t.Fatal("expected Get to return a clone")
	}
}
