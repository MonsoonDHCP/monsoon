package main

import (
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
)

func TestMergeConfigPayloadPreservesExistingValues(t *testing.T) {
	current := config.DefaultConfig()
	current.API.REST.Listen = ":18067"
	current.Auth.Enabled = true
	current.Auth.Session.Duration = config.Duration{Duration: 48 * time.Hour}
	current.Backup.Auto.Retention = 14

	next, err := mergeConfigPayload(current, map[string]any{
		"api": map[string]any{
			"rest": map[string]any{
				"rate_limit": 250,
			},
		},
	})
	if err != nil {
		t.Fatalf("merge config payload: %v", err)
	}

	if next.API.REST.Listen != ":18067" {
		t.Fatalf("expected listen address to be preserved, got %q", next.API.REST.Listen)
	}
	if next.API.REST.RateLimit != 250 {
		t.Fatalf("expected rate limit override, got %d", next.API.REST.RateLimit)
	}
	if next.Auth.Session.Duration.Duration != 48*time.Hour {
		t.Fatalf("expected session duration to be preserved, got %v", next.Auth.Session.Duration.Duration)
	}
	if next.Backup.Auto.Retention != 14 {
		t.Fatalf("expected backup retention to be preserved, got %d", next.Backup.Auto.Retention)
	}
}

func TestMergeConfigPayloadReplacesNestedValues(t *testing.T) {
	current := config.DefaultConfig()
	current.API.REST.CORSOrigins = []string{"https://old.example"}

	next, err := mergeConfigPayload(current, map[string]any{
		"api": map[string]any{
			"rest": map[string]any{
				"cors_origins": []any{"https://app.example", "https://ops.example"},
			},
		},
		"auth": map[string]any{
			"enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("merge config payload: %v", err)
	}

	if next.Auth.Enabled {
		t.Fatalf("expected auth to be disabled after merge")
	}
	if len(next.API.REST.CORSOrigins) != 2 || next.API.REST.CORSOrigins[0] != "https://app.example" {
		t.Fatalf("unexpected cors origins: %#v", next.API.REST.CORSOrigins)
	}
}

func TestMergeConfigPayloadRejectsUnknownFields(t *testing.T) {
	current := config.DefaultConfig()

	_, err := mergeConfigPayload(current, map[string]any{
		"api": map[string]any{
			"rest": map[string]any{
				"non_existing_field": true,
			},
		},
	})
	if err == nil {
		t.Fatal("expected merge config payload to reject unknown fields")
	}
}
