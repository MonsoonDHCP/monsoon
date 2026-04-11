package config

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestValidateHelpers(t *testing.T) {
	if err := validateListenAddr(":8067"); err != nil {
		t.Fatalf("validateListenAddr(port-only): %v", err)
	}
	if err := validateListenAddr("127.0.0.1:8067"); err != nil {
		t.Fatalf("validateListenAddr(ipv4): %v", err)
	}
	if err := validateListenAddr("[::1]:8067"); err != nil {
		t.Fatalf("validateListenAddr(ipv6): %v", err)
	}
	if err := validateListenAddr("example.com:8067"); err == nil {
		t.Fatalf("expected hostname listen addr to fail")
	}
	if err := validateListenAddr(":99999"); err == nil {
		t.Fatalf("expected invalid port to fail")
	}
	if err := validateListenAddr(""); err == nil {
		t.Fatalf("expected empty listen addr to fail")
	}

	if !prefixesOverlap(netip.MustParsePrefix("10.0.0.0/24"), netip.MustParsePrefix("10.0.0.128/25")) {
		t.Fatalf("expected overlapping prefixes")
	}
	if prefixesOverlap(netip.MustParsePrefix("10.0.0.0/24"), netip.MustParsePrefix("10.0.1.0/24")) {
		t.Fatalf("unexpected overlap")
	}

	if err := validateTrustedProxy("127.0.0.1"); err != nil {
		t.Fatalf("validateTrustedProxy(ip): %v", err)
	}
	if err := validateTrustedProxy("10.0.0.0/8"); err != nil {
		t.Fatalf("validateTrustedProxy(cidr): %v", err)
	}
	if err := validateTrustedProxy(" "); err == nil {
		t.Fatalf("expected empty trusted proxy to fail")
	}

	var errs []string
	validateTLSPair("api.rest", "cert.pem", "", &errs)
	if len(errs) != 1 || !strings.Contains(errs[0], "api.rest.tls_cert_file") {
		t.Fatalf("unexpected tls pair errors: %#v", errs)
	}
}

func TestValidateAggregatesImportantErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = ""
	cfg.Server.LogLevel = "trace"
	cfg.Server.LogFormat = "xml"
	cfg.DHCP.DefaultLeaseTime = Duration{}
	cfg.DHCP.MaxLeaseTime = Duration{Duration: time.Minute}
	cfg.API.REST.Listen = "bad"
	cfg.API.GRPC.Listen = ""
	cfg.API.REST.RateLimit = 0
	cfg.API.REST.AuthRateLimit = 0
	cfg.Webhooks = []WebhookConfig{{
		Name:   "",
		URL:    "://bad",
		Events: nil,
		Format: "yaml",
		Retry: WebhookRetryConfig{
			MaxAttempts: -1,
			Backoff:     "weird",
		},
	}}
	cfg.Auth.Enabled = true
	cfg.Auth.Type = ""
	cfg.HA.Enabled = true
	cfg.HA.PeerAddress = "bad"
	cfg.HA.Priority = 0
	cfg.HA.WitnessHoldTime = Duration{Duration: -time.Second}
	cfg.Subnets = []SubnetConfig{
		{
			CIDR:    "10.0.0.0/24",
			Gateway: "10.0.1.1",
			DHCP: SubnetDHCPConfig{
				Enabled:      true,
				PoolStart:    "10.0.0.20",
				PoolEnd:      "10.0.0.10",
				LeaseTime:    Duration{Duration: time.Hour},
				MaxLeaseTime: Duration{Duration: time.Minute},
			},
			Reservations: []ReservationConfig{{
				MAC: "bad",
				IP:  "10.1.0.10",
			}},
		},
		{
			CIDR: "10.0.0.128/25",
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatalf("expected invalid config to fail")
	}
	msg := err.Error()
	wants := []string{
		"server.data_dir is required",
		"server.log_level must be one of",
		"server.log_format must be one of",
		"dhcp.default_lease_time must be > 0",
		"api.rest.listen:",
		"api.grpc.listen:",
		"api.rest.rate_limit must be > 0",
		"api.rest.auth_rate_limit must be > 0",
		"webhooks[0].name is required",
		"webhooks[0].url is invalid",
		"webhooks[0].format must be json or slack",
		"webhooks[0].events must not be empty",
		"webhooks[0].retry.max_attempts must be >= 0",
		"webhooks[0].retry.backoff must be exponential or fixed",
		"auth.type is required when auth.enabled=true",
		"ha.peer_address:",
		"ha.priority must be > 0",
		"ha.witness_hold_time must be >= 0",
		"subnets[0].gateway not in cidr",
		"subnets[0].reservations[0].mac invalid",
		"subnets[0].reservations[0].ip outside subnet",
		"subnets[0].dhcp pool_start must be <= pool_end",
		"subnets[0].dhcp.max_lease_time must be >= lease_time",
		"subnets[1].cidr overlaps subnets[0].cidr",
	}
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected validation error to contain %q, got %q", want, msg)
		}
	}
}
