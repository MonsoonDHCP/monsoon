package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
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

	if cfg.Auth.Enabled && strings.TrimSpace(cfg.Auth.Type) == "" {
		errs = append(errs, "auth.type is required when auth.enabled=true")
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
