package config

import (
	"reflect"
	"testing"
	"time"
)

func TestApplyEnvOverridesCoversSupportedTypes(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("MONSOON_SERVER_HOSTNAME", "env-host")
	t.Setenv("MONSOON_API_REST_ENABLED", "false")
	t.Setenv("MONSOON_API_REST_RATE_LIMIT", "77")
	t.Setenv("MONSOON_DHCP_DEFAULT_LEASE_TIME", "30m")
	t.Setenv("MONSOON_API_REST_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")

	if err := ApplyEnvOverrides(cfg); err != nil {
		t.Fatalf("apply env overrides: %v", err)
	}
	if cfg.Server.Hostname != "env-host" {
		t.Fatalf("unexpected hostname %q", cfg.Server.Hostname)
	}
	if cfg.API.REST.Enabled {
		t.Fatal("expected bool override to apply")
	}
	if cfg.API.REST.RateLimit != 77 {
		t.Fatalf("unexpected int override %d", cfg.API.REST.RateLimit)
	}
	if cfg.DHCP.DefaultLeaseTime.Duration != 30*time.Minute {
		t.Fatalf("unexpected duration override %v", cfg.DHCP.DefaultLeaseTime.Duration)
	}
	if !reflect.DeepEqual(cfg.API.REST.TrustedProxies, []string{"127.0.0.1", "10.0.0.0/8"}) {
		t.Fatalf("unexpected slice override %#v", cfg.API.REST.TrustedProxies)
	}
}

func TestSetValueAndToSnakeHandleErrors(t *testing.T) {
	var unsupported struct {
		Value map[string]string
	}
	field := reflect.ValueOf(&unsupported).Elem().FieldByName("Value")
	if err := setValue(field, "x"); err == nil {
		t.Fatal("expected unsupported kind to fail")
	}

	cfg := DefaultConfig()
	boolField := reflect.ValueOf(cfg).Elem().FieldByName("Dashboard").FieldByName("Enabled")
	if err := setValue(boolField, "not-bool"); err == nil {
		t.Fatal("expected invalid bool to fail")
	}

	if got := toSnake("APIREST"); got != "a_p_i_r_e_s_t" {
		t.Fatalf("unexpected snake case conversion %q", got)
	}
}
