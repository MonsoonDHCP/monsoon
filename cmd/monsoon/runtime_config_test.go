package main

import (
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
)

func TestRuntimeSettingsApplyHotReloadableFields(t *testing.T) {
	cfg := config.DefaultConfig()
	settings := newRuntimeSettings(cfg)

	if got := settings.RESTAuthRateLimit(); got != cfg.API.REST.AuthRateLimit {
		t.Fatalf("expected initial auth rate limit %d, got %d", cfg.API.REST.AuthRateLimit, got)
	}
	if !settings.AuthEnforced() {
		t.Fatalf("expected local auth to start enforced")
	}

	next := cfg.Clone()
	next.API.REST.CORSOrigins = []string{"https://app.example"}
	next.API.REST.TrustedProxies = []string{"10.0.0.0/8"}
	next.API.REST.RateLimit = 42
	next.API.REST.AuthRateLimit = 3
	next.Auth.Enabled = false
	next.Auth.Session.Secure = false
	settings.Apply(next)

	if got := settings.RESTCORSOrigins(); len(got) != 1 || got[0] != "https://app.example" {
		t.Fatalf("expected updated cors origins, got %#v", got)
	}
	if got := settings.RESTTrustedProxies(); len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Fatalf("expected updated trusted proxies, got %#v", got)
	}
	if got := settings.RESTRateLimit(); got != 42 {
		t.Fatalf("expected updated rate limit 42, got %d", got)
	}
	if got := settings.RESTAuthRateLimit(); got != 3 {
		t.Fatalf("expected updated auth rate limit 3, got %d", got)
	}
	if settings.AuthEnforced() {
		t.Fatalf("expected auth enforcement to reflect reload")
	}
	if settings.AuthSecureCookie() {
		t.Fatalf("expected secure cookie flag to reflect reload")
	}
}

func TestRestartRequiredConfigChangesSeparatesHotReloadableFields(t *testing.T) {
	before := config.DefaultConfig()
	after := before.Clone()
	after.API.REST.CORSOrigins = []string{"https://app.example"}
	after.API.REST.TrustedProxies = []string{"10.0.0.0/8"}
	after.API.REST.RateLimit = 50
	after.API.REST.AuthRateLimit = 2
	after.Auth.Enabled = false
	after.Auth.Session.Secure = false

	if changes := restartRequiredConfigChanges(before, after); len(changes) != 0 {
		t.Fatalf("expected hot-reloadable changes to avoid restart list, got %#v", changes)
	}

	after = before.Clone()
	after.API.REST.Listen = ":9999"
	after.Auth.Session.Duration.Duration *= 2
	after.Webhooks = []config.WebhookConfig{{Name: "ops", URL: "https://hooks.example"}}

	changes := restartRequiredConfigChanges(before, after)
	assertHasChange(t, changes, "api.rest.listen")
	assertHasChange(t, changes, "auth.session.duration")
	assertHasChange(t, changes, "webhooks")
}

func TestRuntimeReloadStateSnapshotTracksPendingRestart(t *testing.T) {
	state := newRuntimeReloadState()
	now := time.Now().UTC().Round(time.Second)
	state.Mark(now, []string{"api.rest.listen"})

	snapshot, ok := state.Snapshot().(map[string]any)
	if !ok {
		t.Fatalf("expected map snapshot")
	}
	if pending, _ := snapshot["restart_required_pending"].(bool); !pending {
		t.Fatalf("expected pending restart flag, got %#v", snapshot)
	}
	items, ok := snapshot["restart_required"].([]string)
	if !ok || len(items) != 1 || items[0] != "api.rest.listen" {
		t.Fatalf("unexpected restart-required list: %#v", snapshot["restart_required"])
	}

	state.Mark(now.Add(time.Minute), nil)
	snapshot, _ = state.Snapshot().(map[string]any)
	if pending, _ := snapshot["restart_required_pending"].(bool); pending {
		t.Fatalf("expected pending restart to clear, got %#v", snapshot)
	}
}

func assertHasChange(t *testing.T, changes []string, want string) {
	t.Helper()
	for _, item := range changes {
		if item == want {
			return
		}
	}
	t.Fatalf("expected changes to contain %q, got %#v", want, changes)
}
