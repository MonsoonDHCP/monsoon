package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	grpcapi "github.com/monsoondhcp/monsoon/internal/api/grpc"
	"github.com/monsoondhcp/monsoon/internal/api/mcp"
	"github.com/monsoondhcp/monsoon/internal/api/rest"
	wsapi "github.com/monsoondhcp/monsoon/internal/api/websocket"
	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/dhcpv4"
	"github.com/monsoondhcp/monsoon/internal/dhcpv6"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ha"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/metrics"
	"github.com/monsoondhcp/monsoon/internal/migrate"
	uisettings "github.com/monsoondhcp/monsoon/internal/settings"
	"github.com/monsoondhcp/monsoon/internal/storage"
	"github.com/monsoondhcp/monsoon/internal/webhook"
	"gopkg.in/yaml.v3"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	startedAt := time.Now().UTC()

	var (
		configPath      string
		dataDirFlag     string
		webDistDir      string
		showVersion     bool
		initConfig      bool
		checkConfig     bool
		exportConfig    bool
		doBackup        bool
		restoreFrom     string
		doMigrate       bool
		migrateFrom     string
		migrateDry      bool
		migrateMode     string
		migrateSrcCfg   string
		migrateAPIURL   string
		migrateAPIToken string
		migrateSub      string
		migrateAddr     string
		migrateRes      string
		migrateLease    string
		debug           bool
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
	flag.StringVar(&migrateFrom, "from", "", "Migration source (csv, isc-dhcp, kea, netbox, phpipam)")
	flag.BoolVar(&migrateDry, "dry-run", false, "Validate migration inputs without writing")
	flag.StringVar(&migrateMode, "conflict-policy", migrate.ConflictOverwrite, "Conflict policy (overwrite|skip)")
	flag.StringVar(&migrateSrcCfg, "source-config", "", "Source configuration file for migration adapters such as Kea")
	flag.StringVar(&migrateAPIURL, "api-url", "", "Source API base URL for migration adapters such as NetBox")
	flag.StringVar(&migrateAPIToken, "api-token", "", "Source API token for migration adapters")
	flag.StringVar(&migrateSub, "subnets", "", "CSV file containing subnet records")
	flag.StringVar(&migrateAddr, "addresses", "", "CSV file containing address records")
	flag.StringVar(&migrateRes, "reservations", "", "CSV file containing reservation records")
	flag.StringVar(&migrateLease, "leases", "", "CSV file containing lease records")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	originalArgs := append([]string(nil), os.Args...)
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		rewritten := append([]string{os.Args[0], "--migrate"}, os.Args[2:]...)
		for idx := 2; idx < len(rewritten); idx++ {
			switch {
			case rewritten[idx] == "--config" || rewritten[idx] == "-c":
				rewritten[idx] = "--source-config"
			case strings.HasPrefix(rewritten[idx], "--config="):
				rewritten[idx] = "--source-config=" + strings.TrimPrefix(rewritten[idx], "--config=")
			}
		}
		os.Args = rewritten
		defer func() {
			os.Args = originalArgs
		}()
	}
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
	cfg := applyRuntimeOverrides(cfgManager.Get(), dataDirFlag, debug)
	runtimeCfg := newRuntimeSettings(cfg)
	reloadState := newRuntimeReloadState()
	cfgManager.RegisterOnReload(func(next *config.Config) {
		runtimeCfg.Apply(applyRuntimeOverrides(next, dataDirFlag, debug))
	})

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
		"sessions",
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
		if err := restoreSnapshotIntoEngine(eng, restoreFrom); err != nil {
			log.Printf("restore failed: %v", err)
			return 1
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

	leaseStore := lease.NewStore(eng)
	uiSettingsStore := uisettings.NewUIStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	if err := ipamEngine.SeedFromConfig(context.Background(), cfg.Subnets); err != nil {
		log.Printf("ipam seed failed: %v", err)
		return 1
	}
	if doMigrate {
		report, err := migrate.NewRunner(ipamEngine, leaseStore).Run(context.Background(), migrate.Options{
			Source:         migrateFrom,
			DryRun:         migrateDry,
			ConflictPolicy: migrateMode,
			SourceConfig:   migrateSrcCfg,
			APIURL:         migrateAPIURL,
			APIToken:       migrateAPIToken,
			CSV: migrate.CSVOptions{
				SubnetsPath:      migrateSub,
				AddressesPath:    migrateAddr,
				ReservationsPath: migrateRes,
				LeasesPath:       migrateLease,
			},
		})
		printMigrationReport(report)
		if err != nil {
			log.Printf("migration failed: %v", err)
			return 1
		}
		return 0
	}
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
		AuthType:          cfg.Auth.Type,
		CookieName:        cfg.Auth.Session.CookieName,
		SessionDuration:   cfg.Auth.Session.Duration.Duration,
		MaxFailedAttempts: cfg.Auth.Local.MaxFailedAttempts,
		LockoutDuration:   cfg.Auth.Local.LockoutDuration.Duration,
	})
	if cfg.Auth.Enabled && strings.EqualFold(cfg.Auth.Type, "local") {
		if strings.TrimSpace(cfg.Auth.Local.AdminPasswordHash) != "" {
			if err := authService.EnsureAdmin(context.Background(), cfg.Auth.Local.AdminUsername, cfg.Auth.Local.AdminPasswordHash); err != nil {
				log.Printf("auth bootstrap failed: %v", err)
				return 1
			}
		} else {
			log.Printf("local auth enabled without admin_password_hash; complete first-time setup via POST /api/v1/auth/bootstrap")
		}
	}
	eventBroker := events.NewBroker(64)
	quarantine := cfg.IPAM.Discovery.QuarantinePeriod.Duration
	if quarantine <= 0 {
		quarantine = 15 * time.Minute
	}
	sweeper := lease.NewSweeper(leaseStore, 30*time.Second, quarantine, func(item lease.Lease) {
		switch item.State {
		case lease.StateExpired:
			eventBroker.Publish(events.Event{
				Type: "lease.expired",
				Data: map[string]any{
					"ip":     item.IP,
					"mac":    item.MAC,
					"subnet": item.SubnetID,
				},
			})
		}
	})
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
		handler.SetOnLeaseEvent(func(eventType string, item lease.Lease) {
			data := map[string]any{
				"ip":       item.IP,
				"mac":      item.MAC,
				"hostname": item.Hostname,
				"subnet":   item.SubnetID,
			}
			if eventType == "lease.renewed" {
				remaining := time.Until(item.ExpiryTime).Round(time.Second)
				if remaining > 0 {
					data["remaining"] = remaining.String()
				}
			}
			eventBroker.Publish(events.Event{Type: eventType, Data: data})
		})
		dhcpServer = dhcpv4.NewServer(cfg.DHCP.V4.Listen, handler)
		go func() {
			dhcpErr <- dhcpServer.Start(runCtx)
		}()
		dhcpStarted = true
	}
	dhcpV6Err := make(chan error, 1)
	var dhcpV6Server *dhcpv6.Server
	if cfg.DHCP.V6.Enabled {
		poolsV6, err := dhcpv6.NewPoolManager(cfg.Subnets, cfg.DHCP.DefaultLeaseTime.Duration, leaseStore)
		if err != nil {
			log.Printf("dhcpv6 pool init failed: %v", err)
			return 1
		}
		serverDUID := pickServerDUID(cfg)
		handlerV6 := dhcpv6.NewHandler(
			leaseStore,
			poolsV6,
			serverDUID,
			cfg.DHCP.DefaultLeaseTime.Duration,
			cfg.DHCP.MaxLeaseTime.Duration,
		)
		if domain := strings.TrimSpace(cfg.DHCP.DDNS.ForwardZone); domain != "" {
			handlerV6.SetDomainList([]string{domain})
		}
		handlerV6.SetOnLeaseEvent(func(eventType string, item lease.Lease) {
			data := map[string]any{
				"ip":     item.IP,
				"subnet": item.SubnetID,
			}
			eventBroker.Publish(events.Event{Type: eventType, Data: data})
		})
		dhcpV6Server = dhcpv6.NewServer(cfg.DHCP.V6.Listen, handlerV6)
		go func() {
			dhcpV6Err <- dhcpV6Server.Start(runCtx)
		}()
	}

	reg := metrics.NewRegistry()
	reg.SetGauge("monsoon_build_info", map[string]string{"version": version}, 1)
	var haManager *ha.Manager
	if cfg.HA.Enabled {
		haManager = ha.NewManager(ha.Config{
			Node:              cfg.Server.Hostname,
			Mode:              cfg.HA.Mode,
			Priority:          cfg.HA.Priority,
			PeerAddress:       cfg.HA.PeerAddress,
			HeartbeatInterval: cfg.HA.HeartbeatInterval.Duration,
			FailoverTimeout:   cfg.HA.FailoverTimeout.Duration,
			LeaseSync:         cfg.HA.LeaseSync,
			SharedSecret:      cfg.HA.SharedSecret,
			WitnessPath:       cfg.HA.WitnessPath,
			WitnessHold:       cfg.HA.WitnessHoldTime.Duration,
		}, leaseStore, eventBroker, reg)
		if err := haManager.Start(runCtx); err != nil {
			log.Printf("ha manager startup failed: %v", err)
			return 1
		}
	}
	var wsHub *wsapi.Hub
	if cfg.API.WebSocket.Enabled {
		wsHub = wsapi.NewHub(eventBroker)
		wsHub.Start(runCtx)
	}
	var webhookDispatcher *webhook.Dispatcher
	if len(cfg.Webhooks) > 0 {
		webhookDispatcher = webhook.NewDispatcher(cfg.Webhooks, eventBroker, nil)
		webhookDispatcher.Start(runCtx)
	}
	discoveryEngine.SetOnComplete(func(result discovery.ScanResult) {
		eventBroker.Publish(events.Event{
			Type: "discovery.scan_completed",
			Data: map[string]any{
				"scan_id":       result.ScanID,
				"subnet":        firstSubnet(result.Subnets),
				"subnet_cidr":   firstSubnet(result.Subnets),
				"total_hosts":   result.TotalHosts,
				"new_hosts":     result.NewHosts,
				"changed_hosts": result.ChangedHosts,
				"missing_hosts": result.MissingHosts,
				"conflicts":     result.Conflicts,
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
		LeaseStore:           leaseStore,
		IPAMEngine:           ipamEngine,
		DiscoveryEngine:      discoveryEngine,
		DiscoveryEnabled:     cfg.IPAM.Discovery.Enabled,
		AuthService:          authService,
		AuthSecureCookie:     cfg.Auth.Session.Secure,
		AuthSecureCookieFunc: runtimeCfg.AuthSecureCookie,
		AuditLogger:          auditLogger,
		Version:              version,
		MetricsPath:          metricsPath,
		Metrics:              reg,
		StorageReady: func(ctx context.Context) error {
			return eng.Tx(func(tx *storage.Tx) error {
				return ctx.Err()
			})
		},
		DHCPv4Enabled: cfg.DHCP.V4.Enabled,
		DHCPv4Listen:  cfg.DHCP.V4.Listen,
		HAEnabled:     cfg.HA.Enabled,
		HAStatus: func() any {
			if haManager == nil {
				return map[string]any{"status": "disabled"}
			}
			return haManager.Status()
		},
		HATriggerFailover: func(_ context.Context, reason string) (any, error) {
			if haManager == nil {
				return nil, fmt.Errorf("ha is disabled")
			}
			return haManager.TriggerManualFailover(reason)
		},
		Dashboard: rest.DashboardConfig{
			Enabled:  cfg.Dashboard.Enabled,
			DistDir:  webDistDir,
			BasePath: cfg.Dashboard.BasePath,
		},
		UISettings:   uiSettingsStore,
		EventBroker:  eventBroker,
		StartedAt:    startedAt,
		ReloadStatus: reloadState.Snapshot,
		ConfigSnapshot: func() any {
			snapshot := cfgManager.Get()
			if dataDirFlag != "" {
				snapshot.Server.DataDir = dataDirFlag
			}
			return snapshot
		},
		UpdateConfig: func(_ context.Context, payload map[string]any) (any, error) {
			beforeReload := applyRuntimeOverrides(cfgManager.Get(), dataDirFlag, debug)
			next, err := mergeConfigPayload(cfgManager.Get(), payload)
			if err != nil {
				return nil, err
			}
			if err := config.Validate(next); err != nil {
				return nil, err
			}

			body, err := yaml.Marshal(next)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(configPath, body, 0o600); err != nil {
				return nil, err
			}
			if err := cfgManager.Reload(); err != nil {
				return nil, err
			}
			updated := applyRuntimeOverrides(cfgManager.Get(), dataDirFlag, debug)
			reloadState.Mark(time.Now().UTC(), restartRequiredConfigChanges(beforeReload, updated))
			return updated, nil
		},
		CreateBackup: func(_ context.Context) (rest.SystemBackup, error) {
			snapshot := cfgManager.Get()
			if dataDirFlag != "" {
				snapshot.Server.DataDir = dataDirFlag
			}

			if err := eng.CreateSnapshot(); err != nil {
				return rest.SystemBackup{}, err
			}
			backupDir := snapshot.Backup.Auto.Path
			if strings.TrimSpace(backupDir) == "" {
				backupDir = filepath.Join(snapshot.Server.DataDir, "backups")
			}
			if err := os.MkdirAll(backupDir, 0o755); err != nil {
				return rest.SystemBackup{}, err
			}

			now := time.Now().UTC()
			name := fmt.Sprintf("monsoon-%s.snapshot", now.Format("20060102-150405"))
			src := filepath.Join(snapshot.Server.DataDir, "storage", "snapshot.bin")
			dst := filepath.Join(backupDir, name)

			body, err := os.ReadFile(src)
			if err != nil {
				return rest.SystemBackup{}, err
			}
			if err := os.WriteFile(dst, body, 0o600); err != nil {
				return rest.SystemBackup{}, err
			}
			abs, _ := filepath.Abs(dst)
			if abs != "" {
				dst = abs
			}
			return rest.SystemBackup{
				Name:      name,
				Path:      dst,
				SizeBytes: int64(len(body)),
				CreatedAt: now,
			}, nil
		},
		ListBackups: func(ctx context.Context) ([]rest.SystemBackup, error) {
			snapshot := cfgManager.Get()
			if dataDirFlag != "" {
				snapshot.Server.DataDir = dataDirFlag
			}
			backupDir := snapshot.Backup.Auto.Path
			if strings.TrimSpace(backupDir) == "" {
				backupDir = filepath.Join(snapshot.Server.DataDir, "backups")
			}
			entries, err := os.ReadDir(backupDir)
			if err != nil {
				if os.IsNotExist(err) {
					return []rest.SystemBackup{}, nil
				}
				return nil, err
			}
			out := make([]rest.SystemBackup, 0, len(entries))
			for _, entry := range entries {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if entry.IsDir() {
					continue
				}
				info, infoErr := entry.Info()
				if infoErr != nil {
					continue
				}
				path := filepath.Join(backupDir, entry.Name())
				abs, _ := filepath.Abs(path)
				if abs != "" {
					path = abs
				}
				out = append(out, rest.SystemBackup{
					Name:      entry.Name(),
					Path:      path,
					SizeBytes: info.Size(),
					CreatedAt: info.ModTime().UTC(),
				})
			}
			sort.Slice(out, func(i, j int) bool {
				return out[i].CreatedAt.After(out[j].CreatedAt)
			})
			return out, nil
		},
		RestoreBackup: func(ctx context.Context, req rest.RestoreRequest) (rest.SystemBackup, error) {
			snapshot := cfgManager.Get()
			if dataDirFlag != "" {
				snapshot.Server.DataDir = dataDirFlag
			}
			return restoreBackupFromRequest(ctx, eng, snapshot, dataDirFlag, req)
		},
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
		if wsHub != nil {
			mux.Handle("GET /ws", wsHub.Handler())
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

	restHandler := rest.Chain(
		mux,
		rest.RequestIDMiddleware(),
		rest.TrustedProxyHeadersMiddlewareFunc(runtimeCfg.RESTTrustedProxies),
		rest.RecoveryMiddleware(),
		rest.SecurityHeadersMiddleware(),
		rest.CORSMiddlewareFunc(runtimeCfg.RESTCORSOrigins),
		rest.CSRFMiddleware(authService, reg, auditLogger),
		rest.AuthRateLimitMiddlewareFunc(runtimeCfg.RESTAuthRateLimit, reg, auditLogger),
		rest.RateLimitMiddlewareFunc(runtimeCfg.RESTRateLimit),
		rest.AuthMiddlewareFunc(authService, runtimeCfg.AuthEnforced),
		rest.LoggingMiddleware(),
	)
	restServer := rest.NewServer(
		cfg.API.REST.Listen,
		restHandler,
		rest.WithTLS(cfg.API.REST.TLSCertFile, cfg.API.REST.TLSKeyFile),
	)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- restServer.Start()
	}()

	var mcpServer *rest.Server
	mcpErr := make(chan error, 1)
	if cfg.API.MCP.Enabled {
		mcpHandler := mcp.NewServer(mcp.HandlerDeps{
			LeaseStore:       leaseStore,
			IPAMEngine:       ipamEngine,
			DiscoveryEngine:  discoveryEngine,
			DiscoveryEnabled: cfg.IPAM.Discovery.Enabled,
			AuditLogger:      auditLogger,
			EventBroker:      eventBroker,
			Version:          version,
			StartedAt:        startedAt,
			StorageReady: func(ctx context.Context) error {
				return eng.Tx(func(tx *storage.Tx) error {
					return ctx.Err()
				})
			},
			DHCPv4Enabled: cfg.DHCP.V4.Enabled,
			DHCPv4Listen:  cfg.DHCP.V4.Listen,
			DHCPv4Running: func() bool {
				if dhcpServer != nil {
					return dhcpServer.Running()
				}
				return dhcpStarted
			},
			HAEnabled: cfg.HA.Enabled,
			HAStatus: func() any {
				if haManager == nil {
					return map[string]any{"status": "disabled"}
				}
				return haManager.Status()
			},
			MCPListen: cfg.API.MCP.Listen,
		}).Handler()
		mcpHandler = rest.Chain(
			mcpHandler,
			rest.RequestIDMiddleware(),
			rest.TrustedProxyHeadersMiddlewareFunc(runtimeCfg.RESTTrustedProxies),
			rest.RecoveryMiddleware(),
			rest.SecurityHeadersMiddleware(),
			rest.CORSMiddlewareFunc(runtimeCfg.RESTCORSOrigins),
			rest.CSRFMiddleware(authService, reg, auditLogger),
			rest.AuthRateLimitMiddlewareFunc(runtimeCfg.RESTAuthRateLimit, reg, auditLogger),
			rest.RateLimitMiddlewareFunc(runtimeCfg.RESTRateLimit),
			rest.AuthMiddlewareFunc(authService, runtimeCfg.AuthEnforced),
			rest.LoggingMiddleware(),
		)
		mcpServer = rest.NewServer(
			cfg.API.MCP.Listen,
			mcpHandler,
			rest.WithTLS(cfg.API.MCP.TLSCertFile, cfg.API.MCP.TLSKeyFile),
		)
		go func() {
			mcpErr <- mcpServer.Start()
		}()
	}

	var grpcServer *grpcapi.Server
	grpcErr := make(chan error, 1)
	if cfg.API.GRPC.Enabled {
		grpcTLSConfig, err := loadServerTLSConfig(cfg.API.GRPC.TLSCertFile, cfg.API.GRPC.TLSKeyFile)
		if err != nil {
			log.Printf("grpc tls config failed: %v", err)
			return 1
		}
		grpcHandler := grpcapi.NewHandler(grpcapi.HandlerDeps{
			LeaseStore:       leaseStore,
			IPAMEngine:       ipamEngine,
			DiscoveryEngine:  discoveryEngine,
			DiscoveryEnabled: cfg.IPAM.Discovery.Enabled,
			Version:          version,
			StartedAt:        startedAt,
			StorageReady: func(ctx context.Context) error {
				return eng.Tx(func(tx *storage.Tx) error {
					return ctx.Err()
				})
			},
			DHCPv4Enabled: cfg.DHCP.V4.Enabled,
			DHCPv4Listen:  cfg.DHCP.V4.Listen,
			DHCPv4Running: func() bool {
				if dhcpServer != nil {
					return dhcpServer.Running()
				}
				return dhcpStarted
			},
			HAEnabled: cfg.HA.Enabled,
			HAStatus: func() any {
				if haManager == nil {
					return map[string]any{"status": "disabled"}
				}
				return haManager.Status()
			},
			EventBroker: eventBroker,
		}).Handler()
		grpcHandler = rest.Chain(
			grpcHandler,
			rest.RequestIDMiddleware(),
			rest.TrustedProxyHeadersMiddlewareFunc(runtimeCfg.RESTTrustedProxies),
			rest.RecoveryMiddleware(),
			rest.SecurityHeadersMiddleware(),
			rest.CSRFMiddleware(authService, reg, auditLogger),
			rest.AuthRateLimitMiddlewareFunc(runtimeCfg.RESTAuthRateLimit, reg, auditLogger),
			rest.RateLimitMiddlewareFunc(runtimeCfg.RESTRateLimit),
			rest.AuthMiddlewareFunc(authService, runtimeCfg.AuthEnforced),
			rest.LoggingMiddleware(),
		)
		var grpcOpts []grpcapi.ServerOption
		if grpcTLSConfig != nil {
			grpcOpts = append(grpcOpts, grpcapi.WithTLSConfig(grpcTLSConfig))
		}
		grpcServer = grpcapi.NewServer(cfg.API.GRPC.Listen, grpcHandler, grpcOpts...)
		go func() {
			grpcErr <- grpcServer.Start()
		}()
	}

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	restScheme := "http"
	if tlsFilesConfigured(cfg.API.REST.TLSCertFile, cfg.API.REST.TLSKeyFile) {
		restScheme = "https"
	}
	grpcScheme := "grpc"
	if tlsFilesConfigured(cfg.API.GRPC.TLSCertFile, cfg.API.GRPC.TLSKeyFile) {
		grpcScheme = "grpcs"
	}
	mcpScheme := "http"
	if tlsFilesConfigured(cfg.API.MCP.TLSCertFile, cfg.API.MCP.TLSKeyFile) {
		mcpScheme = "https"
	}
	log.Printf(
		"monsoon started: rest=%s://%s grpc=%s://%s mcp=%s://%s metrics=%s data=%s",
		restScheme,
		cfg.API.REST.Listen,
		grpcScheme,
		cfg.API.GRPC.Listen,
		mcpScheme,
		cfg.API.MCP.Listen,
		metricsPath,
		cfg.Server.DataDir,
	)

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
		case err := <-dhcpV6Err:
			if err != nil {
				log.Printf("dhcpv6 server error: %v", err)
				return 1
			}
		case err := <-mcpErr:
			if err != nil {
				log.Printf("mcp server error: %v", err)
				return 1
			}
		case err := <-grpcErr:
			if err != nil {
				log.Printf("grpc server error: %v", err)
				return 1
			}
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				beforeReload := cfg
				if err := cfgManager.Reload(); err != nil {
					log.Printf("reload failed: %v", err)
					continue
				}
				cfg = applyRuntimeOverrides(cfgManager.Get(), dataDirFlag, debug)
				runtimeCfg.Apply(cfg)
				restartRequired := restartRequiredConfigChanges(beforeReload, cfg)
				reloadState.Mark(time.Now().UTC(), restartRequired)
				if len(restartRequired) > 0 {
					log.Printf("configuration reloaded from %s; restart required for: %s", configPath, strings.Join(restartRequired, ", "))
					continue
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
				if mcpServer != nil {
					if err := mcpServer.Shutdown(ctx); err != nil {
						log.Printf("mcp shutdown failed: %v", err)
						return 1
					}
				}
				if grpcServer != nil {
					if err := grpcServer.Shutdown(ctx); err != nil {
						log.Printf("grpc shutdown failed: %v", err)
						return 1
					}
				}
				if webhookDispatcher != nil {
					webhookDispatcher.Wait()
				}
				if haManager != nil {
					_ = haManager.Shutdown()
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

func pickServerDUID(cfg *config.Config) []byte {
	seed := strings.TrimSpace(cfg.Server.Hostname)
	if seed == "" {
		seed = "monsoon"
	}
	sum := sha256.Sum256([]byte(seed))
	var uuid [16]byte
	copy(uuid[:], sum[:16])
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return dhcpv6.GenerateDUIDUUID(uuid)
}

func printMigrationReport(report migrate.Report) {
	startedAt := report.StartedAt.UTC().Format(time.RFC3339)
	completedAt := report.CompletedAt.UTC().Format(time.RFC3339)
	fmt.Printf("migration source=%s dry_run=%t started=%s completed=%s\n", report.Source, report.DryRun, startedAt, completedAt)
	for _, file := range report.Files {
		fmt.Printf("- %s: path=%s rows=%d applied=%d skipped=%d errors=%d\n", file.Kind, file.Path, file.Rows, file.Applied, file.Skipped, len(file.Errors))
		for _, rowErr := range file.Errors {
			fmt.Printf("  row %d: %s\n", rowErr.Row, rowErr.Message)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}

func firstSubnet(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.TrimSpace(items[0])
}
