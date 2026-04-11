package main

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
)

type runtimeSettings struct {
	mu                 sync.RWMutex
	restCORSOrigins    []string
	restTrustedProxies []string
	restRateLimit      int
	restAuthRateLimit  int
	authEnforced       bool
	authSecureCookie   bool
}

func newRuntimeSettings(cfg *config.Config) *runtimeSettings {
	settings := &runtimeSettings{}
	settings.Apply(cfg)
	return settings
}

func (s *runtimeSettings) Apply(cfg *config.Config) {
	if cfg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restCORSOrigins = append([]string(nil), cfg.API.REST.CORSOrigins...)
	s.restTrustedProxies = append([]string(nil), cfg.API.REST.TrustedProxies...)
	s.restRateLimit = cfg.API.REST.RateLimit
	s.restAuthRateLimit = cfg.API.REST.AuthRateLimit
	s.authEnforced = runtimeAuthEnforced(cfg)
	s.authSecureCookie = cfg.Auth.Session.Secure
}

func (s *runtimeSettings) RESTCORSOrigins() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.restCORSOrigins...)
}

func (s *runtimeSettings) RESTTrustedProxies() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.restTrustedProxies...)
}

func (s *runtimeSettings) RESTRateLimit() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restRateLimit
}

func (s *runtimeSettings) RESTAuthRateLimit() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restAuthRateLimit
}

func (s *runtimeSettings) AuthEnforced() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authEnforced
}

func (s *runtimeSettings) AuthSecureCookie() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authSecureCookie
}

func runtimeAuthEnforced(cfg *config.Config) bool {
	return cfg != nil &&
		cfg.Auth.Enabled &&
		strings.EqualFold(cfg.Auth.Type, "local")
}

func applyRuntimeOverrides(cfg *config.Config, dataDirFlag string, debug bool) *config.Config {
	if cfg == nil {
		return nil
	}
	next := cfg.Clone()
	if dataDirFlag != "" {
		next.Server.DataDir = dataDirFlag
	}
	if debug {
		next.Server.LogLevel = "debug"
	}
	return next
}

func restartRequiredConfigChanges(before, after *config.Config) []string {
	if before == nil || after == nil {
		return nil
	}

	changes := make([]string, 0, 16)
	appendIfChanged := func(label string, left, right any) {
		if !reloadValuesEqual(left, right) {
			changes = append(changes, label)
		}
	}

	appendIfChanged("server.hostname", before.Server.Hostname, after.Server.Hostname)
	appendIfChanged("server.data_dir", before.Server.DataDir, after.Server.DataDir)
	appendIfChanged("dhcp.v4", before.DHCP.V4, after.DHCP.V4)
	appendIfChanged("dhcp.v6", before.DHCP.V6, after.DHCP.V6)
	appendIfChanged("dhcp.default_lease_time", before.DHCP.DefaultLeaseTime, after.DHCP.DefaultLeaseTime)
	appendIfChanged("dhcp.max_lease_time", before.DHCP.MaxLeaseTime, after.DHCP.MaxLeaseTime)
	appendIfChanged("dhcp.ddns", before.DHCP.DDNS, after.DHCP.DDNS)
	appendIfChanged("subnets", before.Subnets, after.Subnets)
	appendIfChanged("ipam.discovery", before.IPAM.Discovery, after.IPAM.Discovery)
	appendIfChanged("api.rest.listen", before.API.REST.Listen, after.API.REST.Listen)
	appendIfChanged("api.rest.tls", []string{before.API.REST.TLSCertFile, before.API.REST.TLSKeyFile}, []string{after.API.REST.TLSCertFile, after.API.REST.TLSKeyFile})
	appendIfChanged("api.grpc", before.API.GRPC, after.API.GRPC)
	appendIfChanged("api.websocket", before.API.WebSocket, after.API.WebSocket)
	appendIfChanged("api.mcp", before.API.MCP, after.API.MCP)
	appendIfChanged("dashboard", before.Dashboard, after.Dashboard)
	appendIfChanged("auth.type", before.Auth.Type, after.Auth.Type)
	appendIfChanged("auth.local", before.Auth.Local, after.Auth.Local)
	appendIfChanged("auth.ldap", before.Auth.LDAP, after.Auth.LDAP)
	appendIfChanged("auth.api_tokens", before.Auth.APITokens, after.Auth.APITokens)
	appendIfChanged("auth.session.cookie_name", before.Auth.Session.CookieName, after.Auth.Session.CookieName)
	appendIfChanged("auth.session.duration", before.Auth.Session.Duration, after.Auth.Session.Duration)
	appendIfChanged("ha", before.HA, after.HA)
	appendIfChanged("metrics.prometheus.path", before.Metrics.Prometheus.Path, after.Metrics.Prometheus.Path)
	appendIfChanged("webhooks", before.Webhooks, after.Webhooks)

	return changes
}

func reloadValuesEqual(left, right any) bool {
	if reflect.DeepEqual(left, right) {
		return true
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if !leftValue.IsValid() || !rightValue.IsValid() {
		return !leftValue.IsValid() && !rightValue.IsValid()
	}
	if leftValue.Kind() == reflect.Slice && rightValue.Kind() == reflect.Slice {
		return leftValue.Len() == 0 && rightValue.Len() == 0
	}
	return false
}

type runtimeReloadState struct {
	mu              sync.RWMutex
	lastReloadAt    time.Time
	restartRequired []string
}

func newRuntimeReloadState() *runtimeReloadState {
	return &runtimeReloadState{}
}

func (s *runtimeReloadState) Mark(now time.Time, restartRequired []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReloadAt = now.UTC()
	s.restartRequired = append([]string(nil), restartRequired...)
}

func (s *runtimeReloadState) Snapshot() any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lastReloadAt any
	if !s.lastReloadAt.IsZero() {
		lastReloadAt = s.lastReloadAt
	}

	return map[string]any{
		"last_reloaded_at":         lastReloadAt,
		"hot_reloadable":           hotReloadableConfigKeys(),
		"restart_required":         append([]string(nil), s.restartRequired...),
		"restart_required_pending": len(s.restartRequired) > 0,
	}
}

func hotReloadableConfigKeys() []string {
	return []string{
		"api.rest.cors_origins",
		"api.rest.trusted_proxies",
		"api.rest.rate_limit",
		"api.rest.auth_rate_limit",
		"auth.enabled",
		"auth.session.secure",
	}
}
