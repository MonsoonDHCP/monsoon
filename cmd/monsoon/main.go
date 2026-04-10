package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/dhcpv4"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/metrics"
	uisettings "github.com/monsoondhcp/monsoon/internal/settings"
	"github.com/monsoondhcp/monsoon/internal/storage"
	"gopkg.in/yaml.v3"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath   string
		dataDirFlag  string
		webDistDir   string
		showVersion  bool
		initConfig   bool
		checkConfig  bool
		exportConfig bool
		doBackup     bool
		restoreFrom  string
		doMigrate    bool
		debug        bool
	)

	flag.StringVar(&configPath, "config", "/etc/monsoon/monsoon.yaml", "Configuration file path")
	flag.StringVar(&configPath, "c", "/etc/monsoon/monsoon.yaml", "Configuration file path (shorthand)")
	flag.StringVar(&dataDirFlag, "data-dir", "", "Data directory override")
	flag.StringVar(&dataDirFlag, "d", "", "Data directory override (shorthand)")
	flag.StringVar(&webDistDir, "web-dist", "./web/dist", "Web dashboard static dist directory")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&initConfig, "init", false, "Initialize configuration file and exit")
	flag.BoolVar(&checkConfig, "check-config", false, "Validate configuration and exit")
	flag.BoolVar(&exportConfig, "export-config", false, "Export resolved configuration and exit")
	flag.BoolVar(&doBackup, "backup", false, "Create backup snapshot and exit")
	flag.StringVar(&restoreFrom, "restore", "", "Restore snapshot file")
	flag.BoolVar(&doMigrate, "migrate", false, "Run migrations and exit")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.Parse()

	if showVersion {
		fmt.Printf("monsoon %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	}

	if initConfig {
		if err := config.WriteDefault(configPath, false); err != nil {
			log.Printf("init failed: %v", err)
			return 1
		}
		fmt.Printf("config initialized at %s\n", configPath)
		return 0
	}

	cfgManager, err := config.NewManager(configPath)
	if err != nil {
		log.Printf("configuration error: %v", err)
		return 1
	}
	cfg := cfgManager.Get()
	if dataDirFlag != "" {
		cfg.Server.DataDir = dataDirFlag
	}
	if debug {
		cfg.Server.LogLevel = "debug"
	}

	if checkConfig {
		fmt.Printf("configuration valid (%s)\n", configPath)
		return 0
	}

	if exportConfig {
		body, err := yaml.Marshal(cfg)
		if err != nil {
			log.Printf("failed to marshal config: %v", err)
			return 1
		}
		fmt.Println(string(body))
		return 0
	}

	eng, err := storage.OpenEngine(filepath.Join(cfg.Server.DataDir, "storage"), []string{
		"leases",
		"subnets",
		"addresses",
		"reservations",
		"vlans",
		"audit",
		"settings",
		"users",
		"api_tokens",
		"api_tokens_by_hash",
		"discovery_scans",
		"discovery_meta",
	})
	if err != nil {
		log.Printf("storage startup failed: %v", err)
		return 1
	}
	defer func() {
		if err := eng.Close(); err != nil {
			log.Printf("storage close error: %v", err)
		}
	}()

	if restoreFrom != "" {
		trees, err := storage.ReadSnapshot(restoreFrom)
		if err != nil {
			log.Printf("restore failed: %v", err)
			return 1
		}
		for name, tree := range trees {
			it := storage.NewIterator(tree, nil, nil, false)
			for it.Next() {
				if err := eng.Put(name, it.Key(), it.Value()); err != nil {
					log.Printf("restore apply failed (%s): %v", name, err)
					return 1
				}
			}
		}
		fmt.Printf("snapshot restored from %s\n", restoreFrom)
		return 0
	}

	if doBackup {
		backupPath := filepath.Join(cfg.Backup.Auto.Path, fmt.Sprintf("monsoon-%d.snapshot", time.Now().Unix()))
		if err := eng.CreateSnapshot(); err != nil {
			log.Printf("backup failed: %v", err)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			log.Printf("backup dir failed: %v", err)
			return 1
		}
		from := filepath.Join(cfg.Server.DataDir, "storage", "snapshot.bin")
		body, err := os.ReadFile(from)
		if err != nil {
			log.Printf("backup source read failed: %v", err)
			return 1
		}
		if err := os.WriteFile(backupPath, body, 0o600); err != nil {
			log.Printf("backup write failed: %v", err)
			return 1
		}
		fmt.Printf("backup created: %s\n", backupPath)
		return 0
	}

	if doMigrate {
		fmt.Println("migration runner is scaffolded but source adapters are not implemented yet")
		return 0
	}

	leaseStore := lease.NewStore(eng)
	uiSettingsStore := uisettings.NewUIStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	discoveryEngine := discovery.NewEngineWithOptions(
		eng,
		leaseStore,
		ipamEngine,
		cfg.IPAM.Discovery.DefaultInterval.Duration,
		discovery.Options{
			Methods: cfg.IPAM.Discovery.Methods,
		},
	)
	auditLogger := audit.NewLogger(eng)
	authService := auth.NewService(eng, auth.ServiceOptions{
		CookieName:      cfg.Auth.Session.CookieName,
		SessionDuration: cfg.Auth.Session.Duration.Duration,
	})
	if cfg.Auth.Enabled && strings.EqualFold(cfg.Auth.Type, "local") {
		if err := authService.EnsureAdmin(context.Background(), cfg.Auth.Local.AdminUsername, cfg.Auth.Local.AdminPasswordHash); err != nil {
			log.Printf("auth bootstrap failed: %v", err)
			return 1
		}
	}
	if err := ipamEngine.SeedFromConfig(context.Background(), cfg.Subnets); err != nil {
		log.Printf("ipam seed failed: %v", err)
		return 1
	}
	quarantine := cfg.IPAM.Discovery.QuarantinePeriod.Duration
	if quarantine <= 0 {
		quarantine = 15 * time.Minute
	}
	sweeper := lease.NewSweeper(leaseStore, 30*time.Second, quarantine, nil)
	sweeper.Start()
	defer sweeper.Stop()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	if cfg.IPAM.Discovery.Enabled {
		discoveryEngine.Start(runCtx)
	}

	dhcpErr := make(chan error, 1)
	var dhcpServer *dhcpv4.Server
	dhcpStarted := false
	if cfg.DHCP.V4.Enabled {
		pools, err := dhcpv4.NewPoolManager(cfg.Subnets, cfg.DHCP.DefaultLeaseTime.Duration, leaseStore)
		if err != nil {
			log.Printf("dhcpv4 pool init failed: %v", err)
			return 1
		}
		handler := dhcpv4.NewHandler(
			leaseStore,
			pools,
			pickServerIdentifier(cfg),
			cfg.DHCP.DefaultLeaseTime.Duration,
			cfg.DHCP.MaxLeaseTime.Duration,
		)
		dhcpServer = dhcpv4.NewServer(cfg.DHCP.V4.Listen, handler)
		go func() {
			dhcpErr <- dhcpServer.Start(runCtx)
		}()
		dhcpStarted = true
	}

	reg := metrics.NewRegistry()
	reg.SetGauge("monsoon_build_info", map[string]string{"version": version}, 1)
	eventBroker := events.NewBroker(64)
	discoveryEngine.SetOnComplete(func(result discovery.ScanResult) {
		eventBroker.Publish(events.Event{
			Type: "discovery.scan_completed",
			Data: map[string]any{
				"scan_id":       result.ScanID,
				"total_hosts":   result.TotalHosts,
				"new_hosts":     result.NewHosts,
				"changed_hosts": result.ChangedHosts,
				"missing_hosts": result.MissingHosts,
				"conflicts":     len(result.Conflicts),
				"rogue_servers": len(result.RogueServers),
				"duration_ms":   result.DurationMS,
				"completed_at":  result.CompletedAt,
			},
		})
	})

	metricsPath := cfg.Metrics.Prometheus.Path
	if metricsPath == "" {
		metricsPath = "/metrics"
	}

	routeDeps := rest.RouterDeps{
		LeaseStore:       leaseStore,
		IPAMEngine:       ipamEngine,
		DiscoveryEngine:  discoveryEngine,
		AuthService:      authService,
		AuthSecureCookie: cfg.Auth.Session.Secure,
		AuditLogger:      auditLogger,
		Version:          version,
		MetricsPath:      metricsPath,
		DHCPv4Enabled:    cfg.DHCP.V4.Enabled,
		DHCPv4Listen:     cfg.DHCP.V4.Listen,
		Dashboard: rest.DashboardConfig{
			Enabled:  cfg.Dashboard.Enabled,
			DistDir:  webDistDir,
			BasePath: cfg.Dashboard.BasePath,
		},
		UISettings:  uiSettingsStore,
		EventBroker: eventBroker,
		DHCPv4Running: func() bool {
			if dhcpServer != nil {
				return dhcpServer.Running()
			}
			return dhcpStarted
		},
	}

	buildMux := func(deps rest.RouterDeps) (*http.ServeMux, error) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET "+metricsPath, func(w http.ResponseWriter, r *http.Request) {
			reg.Handler().ServeHTTP(w, r)
		})
		mux.HandleFunc("HEAD "+metricsPath, func(w http.ResponseWriter, r *http.Request) {
			reg.Handler().ServeHTTP(w, r)
		})
		if err := rest.RegisterRoutes(mux, deps); err != nil {
			return nil, err
		}
		return mux, nil
	}

	mux, err := buildMux(routeDeps)
	if err != nil {
		if routeDeps.Dashboard.Enabled {
			log.Printf("dashboard disabled (%v), continuing with API-only mode", err)
			routeDeps.Dashboard.Enabled = false
			mux, err = buildMux(routeDeps)
			if err != nil {
				log.Printf("route registration failed: %v", err)
				return 1
			}
		} else {
			log.Printf("route registration failed: %v", err)
			return 1
		}
	}

	enforceAuth := cfg.Auth.Enabled &&
		strings.EqualFold(cfg.Auth.Type, "local")

	restHandler := rest.Chain(
		mux,
		rest.RequestIDMiddleware(),
		rest.RecoveryMiddleware(),
		rest.CORSMiddleware(cfg.API.REST.CORSOrigins),
		rest.RateLimitMiddleware(cfg.API.REST.RateLimit),
		rest.AuthMiddleware(authService, enforceAuth),
		rest.LoggingMiddleware(),
	)
	restServer := rest.NewServer(cfg.API.REST.Listen, restHandler)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- restServer.Start()
	}()

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	log.Printf("monsoon started: rest=%s metrics=%s data=%s", cfg.API.REST.Listen, metricsPath, cfg.Server.DataDir)

	for {
		select {
		case err := <-serverErr:
			if err != nil {
				log.Printf("server error: %v", err)
				return 1
			}
			return 0
		case err := <-dhcpErr:
			if err != nil {
				log.Printf("dhcpv4 server error: %v", err)
				return 1
			}
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				if err := cfgManager.Reload(); err != nil {
					log.Printf("reload failed: %v", err)
					continue
				}
				cfg = cfgManager.Get()
				if dataDirFlag != "" {
					cfg.Server.DataDir = dataDirFlag
				}
				log.Printf("configuration reloaded from %s", configPath)
			case syscall.SIGINT, syscall.SIGTERM:
				cancelRun()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := restServer.Shutdown(ctx); err != nil {
					log.Printf("shutdown failed: %v", err)
					return 1
				}
				log.Printf("monsoon stopped")
				return 0
			}
		}
	}
}

func pickServerIdentifier(cfg *config.Config) net.IP {
	for _, s := range cfg.Subnets {
		if ip := net.ParseIP(s.Gateway).To4(); ip != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(cfg.DHCP.V4.Listen)
	if err == nil && host != "" && host != "0.0.0.0" {
		if ip := net.ParseIP(host).To4(); ip != nil {
			return ip
		}
	}
	return net.IPv4(127, 0, 0, 1).To4()
}
