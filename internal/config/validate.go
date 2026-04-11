package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

func Validate(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	var errs []string
	if strings.TrimSpace(cfg.Server.DataDir) == "" {
		errs = append(errs, "server.data_dir is required")
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, strings.ToLower(cfg.Server.LogLevel)) {
		errs = append(errs, "server.log_level must be one of debug, info, warn, error")
	}
	if !slices.Contains([]string{"json", "text"}, strings.ToLower(cfg.Server.LogFormat)) {
		errs = append(errs, "server.log_format must be one of json, text")
	}

	if cfg.DHCP.DefaultLeaseTime.Duration <= 0 {
		errs = append(errs, "dhcp.default_lease_time must be > 0")
	}
	if cfg.DHCP.MaxLeaseTime.Duration <= 0 {
		errs = append(errs, "dhcp.max_lease_time must be > 0")
	}
	if cfg.DHCP.MaxLeaseTime.Duration < cfg.DHCP.DefaultLeaseTime.Duration {
		errs = append(errs, "dhcp.max_lease_time must be >= dhcp.default_lease_time")
	}

	if err := validateListenAddr(cfg.API.REST.Listen); err != nil {
		errs = append(errs, "api.rest.listen: "+err.Error())
	}
	if err := validateListenAddr(cfg.API.GRPC.Listen); err != nil {
		errs = append(errs, "api.grpc.listen: "+err.Error())
	}
	if cfg.API.REST.RateLimit <= 0 {
		errs = append(errs, "api.rest.rate_limit must be > 0")
	}
	if cfg.API.REST.AuthRateLimit <= 0 {
		errs = append(errs, "api.rest.auth_rate_limit must be > 0")
	}
	for idx, proxy := range cfg.API.REST.TrustedProxies {
		if err := validateTrustedProxy(proxy); err != nil {
			errs = append(errs, fmt.Sprintf("api.rest.trusted_proxies[%d]: %v", idx, err))
		}
	}
	validateTLSPair("api.rest", cfg.API.REST.TLSCertFile, cfg.API.REST.TLSKeyFile, &errs)
	validateTLSPair("api.grpc", cfg.API.GRPC.TLSCertFile, cfg.API.GRPC.TLSKeyFile, &errs)
	validateTLSPair("api.mcp", cfg.API.MCP.TLSCertFile, cfg.API.MCP.TLSKeyFile, &errs)
	if cfg.Auth.Enabled {
		for _, origin := range cfg.API.REST.CORSOrigins {
			if strings.TrimSpace(origin) == "*" {
				errs = append(errs, "api.rest.cors_origins must not contain * when auth.enabled=true")
				break
			}
		}
	}
	for idx, hook := range cfg.Webhooks {
		if strings.TrimSpace(hook.Name) == "" {
			errs = append(errs, fmt.Sprintf("webhooks[%d].name is required", idx))
		}
		if strings.TrimSpace(hook.URL) == "" {
			errs = append(errs, fmt.Sprintf("webhooks[%d].url is required", idx))
		} else if parsed, err := url.Parse(strings.TrimSpace(hook.URL)); err != nil || parsed.Scheme == "" || parsed.Host == "" {
			errs = append(errs, fmt.Sprintf("webhooks[%d].url is invalid", idx))
		}
		format := strings.ToLower(strings.TrimSpace(hook.Format))
		if format == "" {
			format = "json"
		}
		if !slices.Contains([]string{"json", "slack"}, format) {
			errs = append(errs, fmt.Sprintf("webhooks[%d].format must be json or slack", idx))
		}
		if len(hook.Events) == 0 {
			errs = append(errs, fmt.Sprintf("webhooks[%d].events must not be empty", idx))
		}
		if hook.Retry.MaxAttempts < 0 {
			errs = append(errs, fmt.Sprintf("webhooks[%d].retry.max_attempts must be >= 0", idx))
		}
		if backoff := strings.TrimSpace(strings.ToLower(hook.Retry.Backoff)); backoff != "" && backoff != "exponential" && backoff != "fixed" {
			errs = append(errs, fmt.Sprintf("webhooks[%d].retry.backoff must be exponential or fixed", idx))
		}
	}

	if cfg.Auth.Enabled && strings.TrimSpace(cfg.Auth.Type) == "" {
		errs = append(errs, "auth.type is required when auth.enabled=true")
	}
	if cfg.Auth.Enabled && strings.EqualFold(strings.TrimSpace(cfg.Auth.Type), "local") {
		if cfg.Auth.Local.MaxFailedAttempts <= 0 {
			errs = append(errs, "auth.local.max_failed_attempts must be > 0")
		}
		if cfg.Auth.Local.LockoutDuration.Duration <= 0 {
			errs = append(errs, "auth.local.lockout_duration must be > 0")
		}
	}
	if cfg.HA.Enabled {
		if strings.TrimSpace(cfg.HA.PeerAddress) == "" {
			errs = append(errs, "ha.peer_address is required when ha.enabled=true")
		} else if err := validateListenAddr(cfg.HA.PeerAddress); err != nil {
			errs = append(errs, "ha.peer_address: "+err.Error())
		}
		if cfg.HA.Priority <= 0 {
			errs = append(errs, "ha.priority must be > 0")
		}
		if cfg.HA.WitnessHoldTime.Duration < 0 {
			errs = append(errs, "ha.witness_hold_time must be >= 0")
		}
	}

	prefixes := make([]netip.Prefix, 0, len(cfg.Subnets))
	for idx, s := range cfg.Subnets {
		prefix, err := netip.ParsePrefix(s.CIDR)
		if err != nil {
			errs = append(errs, fmt.Sprintf("subnets[%d].cidr invalid: %v", idx, err))
			continue
		}

		for pidx, existing := range prefixes {
			if prefixesOverlap(prefix, existing) {
				errs = append(errs, fmt.Sprintf("subnets[%d].cidr overlaps subnets[%d].cidr", idx, pidx))
			}
		}
		prefixes = append(prefixes, prefix)

		if s.Gateway != "" {
			gw, err := netip.ParseAddr(s.Gateway)
			if err != nil {
				errs = append(errs, fmt.Sprintf("subnets[%d].gateway invalid: %v", idx, err))
			} else if !prefix.Contains(gw) {
				errs = append(errs, fmt.Sprintf("subnets[%d].gateway not in cidr", idx))
			}
		}

		for ridx, r := range s.Reservations {
			if _, err := net.ParseMAC(r.MAC); err != nil {
				errs = append(errs, fmt.Sprintf("subnets[%d].reservations[%d].mac invalid: %v", idx, ridx, err))
			}
			ip, err := netip.ParseAddr(r.IP)
			if err != nil {
				errs = append(errs, fmt.Sprintf("subnets[%d].reservations[%d].ip invalid: %v", idx, ridx, err))
			} else if !prefix.Contains(ip) {
				errs = append(errs, fmt.Sprintf("subnets[%d].reservations[%d].ip outside subnet", idx, ridx))
			}
		}

		if s.DHCP.Enabled {
			start, startErr := netip.ParseAddr(s.DHCP.PoolStart)
			end, endErr := netip.ParseAddr(s.DHCP.PoolEnd)
			if startErr != nil || endErr != nil {
				errs = append(errs, fmt.Sprintf("subnets[%d].dhcp pool_start/pool_end invalid", idx))
			} else {
				if !prefix.Contains(start) || !prefix.Contains(end) {
					errs = append(errs, fmt.Sprintf("subnets[%d].dhcp pool range outside subnet", idx))
				}
				if start.Compare(end) > 0 {
					errs = append(errs, fmt.Sprintf("subnets[%d].dhcp pool_start must be <= pool_end", idx))
				}
			}
			if s.DHCP.LeaseTime.Duration < 0 || s.DHCP.MaxLeaseTime.Duration < 0 {
				errs = append(errs, fmt.Sprintf("subnets[%d].dhcp lease durations must be >= 0", idx))
			}
			if s.DHCP.MaxLeaseTime.Duration > 0 && s.DHCP.LeaseTime.Duration > 0 && s.DHCP.MaxLeaseTime.Duration < s.DHCP.LeaseTime.Duration {
				errs = append(errs, fmt.Sprintf("subnets[%d].dhcp.max_lease_time must be >= lease_time", idx))
			}
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func prefixesOverlap(a, b netip.Prefix) bool {
	a = a.Masked()
	b = b.Masked()
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func validateListenAddr(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return errors.New("address is empty")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			port = strings.TrimPrefix(addr, ":")
		} else {
			return err
		}
	}
	if host != "" {
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		}
		if ip := net.ParseIP(host); ip == nil {
			return fmt.Errorf("invalid host %q", host)
		}
	}
	p, err := net.LookupPort("tcp", port)
	if err != nil {
		return err
	}
	if p < 1 || p > 65535 {
		return fmt.Errorf("invalid port %d", p)
	}
	return nil
}

func validateTLSPair(prefix, certFile, keyFile string, errs *[]string) {
	certSet := strings.TrimSpace(certFile) != ""
	keySet := strings.TrimSpace(keyFile) != ""
	if certSet != keySet {
		*errs = append(*errs, prefix+".tls_cert_file and "+prefix+".tls_key_file must either both be set or both be empty")
	}
}

func validateTrustedProxy(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("must not be empty")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	if _, err := netip.ParsePrefix(value); err == nil {
		return nil
	}
	return fmt.Errorf("must be an IP address or CIDR")
}
