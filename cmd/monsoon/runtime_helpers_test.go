package main

import (
	"reflect"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/config"
)

func TestRuntimeHelperFunctions(t *testing.T) {
	cfg := config.NewDefaults()
	cfg.Auth.Enabled = true
	cfg.Auth.Type = "LOCAL"
	if !runtimeAuthEnforced(cfg) {
		t.Fatalf("expected local auth to be enforced")
	}
	cfg.Auth.Type = "ldap"
	if runtimeAuthEnforced(cfg) {
		t.Fatalf("expected non-local auth not to be enforced")
	}

	cfg.Server.DataDir = "original"
	cfg.Server.LogLevel = "info"
	next := applyRuntimeOverrides(cfg, "override-dir", true)
	if next == cfg {
		t.Fatalf("applyRuntimeOverrides should clone config")
	}
	if next.Server.DataDir != "override-dir" || next.Server.LogLevel != "debug" {
		t.Fatalf("unexpected runtime overrides: %+v", next.Server)
	}
	if cfg.Server.DataDir != "original" || cfg.Server.LogLevel != "info" {
		t.Fatalf("source config should remain unchanged: %+v", cfg.Server)
	}
	if applyRuntimeOverrides(nil, "", false) != nil {
		t.Fatalf("nil config should stay nil")
	}

	if !reloadValuesEqual([]string(nil), []string{}) {
		t.Fatalf("empty slices should be treated equal")
	}
	if reloadValuesEqual([]string{"a"}, []string{"b"}) {
		t.Fatalf("different slices should not be equal")
	}
	if reloadValuesEqual("a", "b") {
		t.Fatalf("different scalars should not be equal")
	}

	got := hotReloadableConfigKeys()
	want := []string{
		"api.rest.cors_origins",
		"api.rest.trusted_proxies",
		"api.rest.rate_limit",
		"api.rest.auth_rate_limit",
		"auth.enabled",
		"auth.session.secure",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hotReloadableConfigKeys = %#v, want %#v", got, want)
	}
}
