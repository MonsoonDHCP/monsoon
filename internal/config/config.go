package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultConfigPath = "/etc/monsoon/monsoon.yaml"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	DHCP      DHCPConfig      `yaml:"dhcp"`
	Subnets   []SubnetConfig  `yaml:"subnets"`
	IPAM      IPAMConfig      `yaml:"ipam"`
	API       APIConfig       `yaml:"api"`
	Dashboard Dashboard       `yaml:"dashboard"`
	Auth      AuthConfig      `yaml:"auth"`
	HA        HAConfig        `yaml:"ha"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	Backup    BackupConfig    `yaml:"backup"`
	Webhooks  []WebhookConfig `yaml:"webhooks"`
}

type ServerConfig struct {
	Hostname  string `yaml:"hostname"`
	DataDir   string `yaml:"data_dir"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

type DHCPConfig struct {
	V4               DHCPV4Config `yaml:"v4"`
	V6               DHCPV6Config `yaml:"v6"`
	DefaultLeaseTime Duration     `yaml:"default_lease_time"`
	MaxLeaseTime     Duration     `yaml:"max_lease_time"`
	DDNS             DDNSConfig   `yaml:"ddns"`
}

type DHCPV4Config struct {
	Enabled       bool   `yaml:"enabled"`
	Listen        string `yaml:"listen"`
	Interface     string `yaml:"interface"`
	Authoritative bool   `yaml:"authoritative"`
}

type DHCPV6Config struct {
	Enabled   bool   `yaml:"enabled"`
	Listen    string `yaml:"listen"`
	Interface string `yaml:"interface"`
}

type DDNSConfig struct {
	Enabled       bool   `yaml:"enabled"`
	ForwardZone   string `yaml:"forward_zone"`
	ReverseZone   string `yaml:"reverse_zone"`
	DNSServer     string `yaml:"dns_server"`
	TSIGKey       string `yaml:"tsig_key"`
	TSIGSecret    string `yaml:"tsig_secret"`
	TSIGAlgorithm string `yaml:"tsig_algorithm"`
}

type SubnetConfig struct {
	CIDR         string              `yaml:"cidr"`
	Name         string              `yaml:"name"`
	VLAN         int                 `yaml:"vlan"`
	Gateway      string              `yaml:"gateway"`
	DNS          []string            `yaml:"dns"`
	DHCP         SubnetDHCPConfig    `yaml:"dhcp"`
	Reservations []ReservationConfig `yaml:"reservations"`
}

type SubnetDHCPConfig struct {
	Enabled      bool              `yaml:"enabled"`
	PoolStart    string            `yaml:"pool_start"`
	PoolEnd      string            `yaml:"pool_end"`
	LeaseTime    Duration          `yaml:"lease_time"`
	MaxLeaseTime Duration          `yaml:"max_lease_time"`
	Options      SubnetOptions     `yaml:"options"`
	Classify     map[string]string `yaml:"classifications"`
}

type SubnetOptions struct {
	Template string `yaml:"template"`
}

type ReservationConfig struct {
	MAC      string `yaml:"mac"`
	IP       string `yaml:"ip"`
	Hostname string `yaml:"hostname"`
}

type IPAMConfig struct {
	Discovery DiscoveryConfig `yaml:"discovery"`
}

type DiscoveryConfig struct {
	Enabled            bool     `yaml:"enabled"`
	DefaultInterval    Duration `yaml:"default_interval"`
	Methods            []string `yaml:"methods"`
	ConflictDetection  bool     `yaml:"conflict_detection"`
	RogueDHCPDetection bool     `yaml:"rogue_dhcp_detection"`
	QuarantinePeriod   Duration `yaml:"quarantine_period"`
	AbandonedThreshold Duration `yaml:"abandoned_threshold"`
}

type APIConfig struct {
	REST      RESTConfig      `yaml:"rest"`
	GRPC      GRPCConfig      `yaml:"grpc"`
	WebSocket WebSocketConfig `yaml:"websocket"`
	MCP       MCPConfig       `yaml:"mcp"`
}

type RESTConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Listen         string   `yaml:"listen"`
	CORSOrigins    []string `yaml:"cors_origins"`
	TrustedProxies []string `yaml:"trusted_proxies"`
	RateLimit      int      `yaml:"rate_limit"`
	AuthRateLimit  int      `yaml:"auth_rate_limit"`
	TLSCertFile    string   `yaml:"tls_cert_file"`
	TLSKeyFile     string   `yaml:"tls_key_file"`
}

type GRPCConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Listen      string `yaml:"listen"`
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
}

type WebSocketConfig struct {
	Enabled bool `yaml:"enabled"`
}

type MCPConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Listen      string `yaml:"listen"`
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
}

type Dashboard struct {
	Enabled  bool   `yaml:"enabled"`
	BasePath string `yaml:"base_path"`
}

type AuthConfig struct {
	Enabled   bool              `yaml:"enabled"`
	Type      string            `yaml:"type"`
	Local     LocalAuthConfig   `yaml:"local"`
	LDAP      LDAPAuthConfig    `yaml:"ldap"`
	APITokens APITokensConfig   `yaml:"api_tokens"`
	Session   SessionAuthConfig `yaml:"session"`
}

type LocalAuthConfig struct {
	AdminUsername     string   `yaml:"admin_username"`
	AdminPasswordHash string   `yaml:"admin_password_hash"`
	MaxFailedAttempts int      `yaml:"max_failed_attempts"`
	LockoutDuration   Duration `yaml:"lockout_duration"`
}

type LDAPAuthConfig struct {
	Server       string `yaml:"server"`
	BaseDN       string `yaml:"base_dn"`
	BindDN       string `yaml:"bind_dn"`
	BindPassword string `yaml:"bind_password"`
	UserFilter   string `yaml:"user_filter"`
	GroupFilter  string `yaml:"group_filter"`
	AdminGroup   string `yaml:"admin_group"`
}

type APITokensConfig struct {
	Enabled bool `yaml:"enabled"`
}

type SessionAuthConfig struct {
	Duration   Duration `yaml:"duration"`
	CookieName string   `yaml:"cookie_name"`
	Secure     bool     `yaml:"secure"`
}

type HAConfig struct {
	Enabled           bool     `yaml:"enabled"`
	Mode              string   `yaml:"mode"`
	Priority          int      `yaml:"priority"`
	PeerAddress       string   `yaml:"peer_address"`
	HeartbeatInterval Duration `yaml:"heartbeat_interval"`
	FailoverTimeout   Duration `yaml:"failover_timeout"`
	LeaseSync         bool     `yaml:"lease_sync"`
	SharedSecret      string   `yaml:"shared_secret"`
	WitnessPath       string   `yaml:"witness_path"`
	WitnessHoldTime   Duration `yaml:"witness_hold_time"`
}

type MetricsConfig struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
}

type PrometheusConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type BackupConfig struct {
	Auto BackupAutoConfig `yaml:"auto"`
}

type BackupAutoConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Interval  Duration `yaml:"interval"`
	Retention int      `yaml:"retention"`
	Path      string   `yaml:"path"`
}

type WebhookConfig struct {
	Name    string             `yaml:"name"`
	URL     string             `yaml:"url"`
	Events  []string           `yaml:"events"`
	Format  string             `yaml:"format"`
	Headers map[string]string  `yaml:"headers"`
	Retry   WebhookRetryConfig `yaml:"retry"`
}

type WebhookRetryConfig struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"`
}

type Duration struct {
	time.Duration
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return errors.New("duration must be scalar")
	}
	if strings.TrimSpace(value.Value) == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = parsed
	return nil
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Hostname:  "monsoon-01",
			DataDir:   "/var/lib/monsoon",
			LogLevel:  "info",
			LogFormat: "json",
		},
		DHCP: DHCPConfig{
			V4: DHCPV4Config{
				Enabled:       true,
				Listen:        "0.0.0.0:67",
				Authoritative: true,
			},
			V6: DHCPV6Config{
				Enabled: false,
				Listen:  "[::]:547",
			},
			DefaultLeaseTime: Duration{Duration: 12 * time.Hour},
			MaxLeaseTime:     Duration{Duration: 24 * time.Hour},
		},
		IPAM: IPAMConfig{
			Discovery: DiscoveryConfig{
				Enabled:            true,
				DefaultInterval:    Duration{Duration: time.Hour},
				Methods:            []string{"arp", "ping"},
				ConflictDetection:  true,
				RogueDHCPDetection: true,
				QuarantinePeriod:   Duration{Duration: 15 * time.Minute},
				AbandonedThreshold: Duration{Duration: 7 * 24 * time.Hour},
			},
		},
		API: APIConfig{
			REST: RESTConfig{
				Enabled:        true,
				Listen:         ":8067",
				CORSOrigins:    []string{},
				TrustedProxies: []string{},
				RateLimit:      100,
				AuthRateLimit:  5,
			},
			GRPC: GRPCConfig{
				Enabled: true,
				Listen:  ":9067",
			},
			WebSocket: WebSocketConfig{Enabled: true},
			MCP: MCPConfig{
				Enabled: true,
				Listen:  ":7067",
			},
		},
		Dashboard: Dashboard{Enabled: true, BasePath: "/"},
		Auth: AuthConfig{
			Enabled: true,
			Type:    "local",
			Local: LocalAuthConfig{
				AdminUsername:     "admin",
				MaxFailedAttempts: 5,
				LockoutDuration:   Duration{Duration: 15 * time.Minute},
			},
			APITokens: APITokensConfig{Enabled: true},
			Session: SessionAuthConfig{
				Duration:   Duration{Duration: 24 * time.Hour},
				CookieName: "monsoon_session",
				Secure:     true,
			},
		},
		HA: HAConfig{
			Enabled:           false,
			Mode:              "active-passive",
			Priority:          100,
			HeartbeatInterval: Duration{Duration: time.Second},
			FailoverTimeout:   Duration{Duration: 10 * time.Second},
			LeaseSync:         true,
			WitnessHoldTime:   Duration{Duration: 15 * time.Second},
		},
		Metrics: MetricsConfig{
			Prometheus: PrometheusConfig{Enabled: true, Path: "/metrics"},
		},
		Backup: BackupConfig{
			Auto: BackupAutoConfig{
				Enabled:   true,
				Interval:  Duration{Duration: 6 * time.Hour},
				Retention: 7,
				Path:      "/var/lib/monsoon/backups",
			},
		},
		Webhooks: []WebhookConfig{},
	}
}

func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	raw, _ := yaml.Marshal(c)
	out := &Config{}
	_ = yaml.Unmarshal(raw, out)
	return out
}

func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultConfigPath
	}
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := ApplyEnvOverrides(cfg); err != nil {
				return nil, err
			}
			if err := Validate(cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := ApplyEnvOverrides(cfg); err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func WriteDefault(path string, overwrite bool) error {
	if strings.TrimSpace(path) == "" {
		path = defaultConfigPath
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config already exists at %s", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	body, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		return fmt.Errorf("marshal defaults: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write defaults: %w", err)
	}
	return nil
}
