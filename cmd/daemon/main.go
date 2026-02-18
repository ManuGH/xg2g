// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/daemon"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	xgtls "github.com/ManuGH/xg2g/internal/tls"
	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/ManuGH/xg2g/internal/verification/checks"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

var (
	version   = "3.1.5"
	commit    = "dev"
	buildDate = "unknown"
)

// maskURL removes user info from a URL string for safe logging.
func maskURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid_url]"
	}
	parsedURL.User = nil
	return parsedURL.String()
}

func printMainUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xg2g - Next Gen to Go")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g [--config path] [--version]")
	_, _ = fmt.Fprintln(w, "  xg2g config <command> [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g healthcheck [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g help [command]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  config       Validate, dump, and migrate config files")
	_, _ = fmt.Fprintln(w, "  storage      Manage and verify local storage (SQLite)")
	_, _ = fmt.Fprintln(w, "  healthcheck  Probe API readiness/liveness endpoints")
	_, _ = fmt.Fprintln(w, "  diagnostic   Trigger diagnostic actions against the API")
	_, _ = fmt.Fprintln(w, "  help         Show help for a command")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --config string  path to config file (YAML)")
	_, _ = fmt.Fprintln(w, "  --version        print version and exit")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Notes:")
	_, _ = fmt.Fprintln(w, "  Running without a subcommand starts the daemon.")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  xg2g --config /etc/xg2g/config.yaml")
	_, _ = fmt.Fprintln(w, "  xg2g config validate -f /etc/xg2g/config.yaml")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify --path /var/lib/xg2g/sessions.sqlite")
	_, _ = fmt.Fprintln(w, "  xg2g healthcheck --mode=ready --port=8088")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic --action=refresh --token $XG2G_API_TOKEN")
}

func runHelp(args []string) int {
	if len(args) == 0 {
		printMainUsage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "config":
		printConfigUsage(os.Stdout)
		return 0
	case "storage":
		printStorageUsage(os.Stdout)
		return 0
	case "healthcheck":
		printHealthcheckUsage(os.Stdout)
		return 0
	case "diagnostic":
		printDiagnosticUsage(os.Stdout)
		return 0
	case "daemon":
		printMainUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown help topic: %s\n\n", args[0])
		printMainUsage(os.Stderr)
		return 2
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			os.Exit(runHelp(os.Args[2:]))
		case "config":
			os.Exit(runConfigCLI(os.Args[2:]))
		case "storage":
			os.Exit(runStorageCLI(os.Args[2:]))
		case "healthcheck":
			os.Exit(runHealthcheckCLI(os.Args[2:]))
		case "diagnostic":
			os.Exit(runDiagnosticCLI(os.Args[2:]))
		case "status":
			if err := statusCmd.Execute(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		case "report":
			if err := reportCmd.Execute(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	// Handle command-line flags
	flag.Usage = func() {
		printMainUsage(flag.CommandLine.Output())
	}
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (YAML)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s (commit: %s, built: %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	// Parse Config
	// Configure logger with safe defaults until config is loaded
	xglog.Configure(xglog.Config{
		Level:   "info",
		Service: "xg2g",
		Version: version,
	})

	logger := xglog.WithComponent("daemon")

	// Create a context that listens for the interrupt signal from the OS
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Determine config path:
	// - Explicit via --config: Fail fast if invalid
	// - Otherwise auto-load ${XG2G_DATA}/config.yaml: Fallback to defaults if invalid/dir
	explicitConfigPath := strings.TrimSpace(*configPath)
	var effectiveConfigPath string

	if explicitConfigPath != "" {
		// Explicit mode: Strict checks
		absPath, err := filepath.Abs(explicitConfigPath)
		if err != nil {
			logger.Fatal().Err(err).Str("path", explicitConfigPath).Msg("failed to resolve absolute path for explicit config")
		}
		info, err := os.Stat(absPath)
		if err != nil {
			logger.Fatal().Err(err).Str("path", absPath).Msg("explicit config file not found")
		}
		if info.IsDir() {
			logger.Fatal().Str("path", absPath).Msg("explicit config path is a directory, expected a file")
		}
		effectiveConfigPath = absPath
	} else {
		// Auto mode: Graceful fallback
		dataDir := strings.TrimSpace(config.ParseString("XG2G_DATA", "/tmp"))
		if dataDir == "" {
			dataDir = "/tmp" // Fallback, though XG2G_DATA should usually be set
		}
		autoPath := filepath.Join(dataDir, "config.yaml")

		// Only use auto path if it exists and is a regular file
		if info, err := os.Stat(autoPath); err == nil && !info.IsDir() {
			if absPath, err := filepath.Abs(autoPath); err == nil {
				effectiveConfigPath = absPath
			}
		}
	}

	// Load configuration with precedence: ENV > File > Defaults
	loader := config.NewLoader(effectiveConfigPath, version)
	cfg, err := loader.Load()
	if err != nil {
		// Log failure using default logger
		logger.Fatal().
			Err(err).
			Str("event", "config.load_failed").
			Str("config_path", effectiveConfigPath).
			Msg("failed to load configuration")
	}

	// Re-configure logger with loaded configuration
	xglog.Configure(xglog.Config{
		Level:   cfg.LogLevel,
		Service: cfg.LogService,
		Version: cfg.Version,
	})

	// Log config source
	if explicitConfigPath != "" {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file").
			Str("path", effectiveConfigPath).
			Msg("loaded configuration from file")
	} else if effectiveConfigPath != "" {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file(auto)").
			Str("path", effectiveConfigPath).
			Msg("loaded configuration from file")
	} else {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "env+defaults").
			Msg("loaded configuration from environment and defaults")
	}

	// Calculate and log configuration hash for debugging traceability
	if configBytes, err := json.Marshal(cfg); err == nil {
		hash := sha256.Sum256(configBytes)
		logger.Info().
			Str("event", "config.snapshot").
			Str("sha256", fmt.Sprintf("%x", hash)).
			Msg("configuration snapshot fingerprint")
	}

	if cfg.Engine.Enabled {
		if !cfg.ConfigStrict {
			logger.Warn().
				Str("event", "config.strict.disabled").
				Msg("v3 strict validation disabled via XG2G_V3_CONFIG_STRICT override")
		}
	}

	// -------------------------------------------------------------------------
	// Pre-flight Checks (Fail Fast)
	// -------------------------------------------------------------------------
	if err := health.PerformStartupChecks(ctx, cfg); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "startup.check_failed").
			Msg("Startup checks failed. Please verify configuration and permissions.")
	}
	// -------------------------------------------------------------------------

	// Parse server configuration
	serverCfg := config.ParseServerConfig()

	// Allow config.yaml to set the API listen address, but keep ENV as the highest priority.
	// ENV precedence: XG2G_LISTEN > config.yaml api.listenAddr > defaults.
	if strings.TrimSpace(config.ParseString("XG2G_LISTEN", "")) == "" {
		if strings.TrimSpace(cfg.APIListenAddr) != "" {
			serverCfg.ListenAddr = cfg.APIListenAddr
		}
	}

	bindHost := strings.TrimSpace(config.ParseString("XG2G_BIND_INTERFACE", ""))
	if bindHost != "" {
		if newListen, err := config.BindListenAddr(serverCfg.ListenAddr, bindHost); err != nil {
			logger.Fatal().
				Err(err).
				Msg("invalid XG2G_BIND_INTERFACE for API listen")
		} else {
			serverCfg.ListenAddr = newListen
		}
	}

	// Auto-generate TLS certificates if enabled but not provided
	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		// User provided explicit paths, use them as-is
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			logger.Info().
				Str("cert", cfg.TLSCert).
				Str("key", cfg.TLSKey).
				Msg("Using user-provided TLS certificates")
		} else {
			logger.Fatal().
				Str("event", "tls.config.invalid").
				Str("cert", cfg.TLSCert).
				Str("key", cfg.TLSKey).
				Msg("Both XG2G_TLS_CERT and XG2G_TLS_KEY must be set together")
		}
	} else if cfg.TLSEnabled {
		// Auto-generate self-signed certificates
		tlsCfg := xgtls.Config{
			CertPath: cfg.TLSCert,
			KeyPath:  cfg.TLSKey,
			Logger:   logger,
		}
		certPath, keyPath, err := xgtls.EnsureCertificates(tlsCfg)
		if err != nil {
			logger.Fatal().
				Err(err).
				Str("event", "tls.ensure.failed").
				Msg("Failed to ensure TLS certificates")
		}
		cfg.TLSCert = certPath
		cfg.TLSKey = keyPath
	}

	logger.Info().
		Str("event", "startup").
		Str("version", version).
		Str("commit", commit).
		Str("build_date", buildDate).
		Str("addr", serverCfg.ListenAddr).
		Msg("starting xg2g")

	// Log key configuration
	logger.Info().Msgf("→ Receiver: %s (auth: %v)", maskURL(cfg.Enigma2.BaseURL), cfg.Enigma2.Username != "")
	logger.Info().Msgf("→ Bouquet: %s", cfg.Bouquet)
	if cfg.Enigma2.UseWebIFStreams {
		if cfg.Enigma2.StreamPort > 0 {
			logger.Info().Msgf("→ Stream: Direct port %d (V3 bypasses /web/stream.m3u)", cfg.Enigma2.StreamPort)
		} else {
			logger.Info().Msg("→ Stream: OpenWebIF /web/stream.m3u (receiver decides port)")
		}
	} else {
		logger.Info().Msgf("→ Stream port: %d (direct TS)", cfg.Enigma2.StreamPort)
	}
	logger.Info().Msgf("→ EPG: %s (%d days)", cfg.XMLTVPath, cfg.EPGDays)
	// Enforce Fail-Closed Authentication
	// Default: refuse startup if no API tokens are configured.
	if strings.TrimSpace(cfg.APIToken) != "" {
		logger.Info().
			Str("event", "auth.configured").
			Msg("→ API token: configured")
	} else if len(cfg.APITokens) > 0 {
		logger.Info().
			Str("event", "auth.configured").
			Msg("→ API tokens: configured")
	} else {
		logger.Fatal().
			Str("event", "auth.missing_token").
			Msg("No API tokens configured. Set XG2G_API_TOKEN or XG2G_API_TOKENS.")
	}
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		logger.Info().Msgf("→ TLS: enabled (cert: %s, key: %s)", cfg.TLSCert, cfg.TLSKey)
	}
	logger.Info().Msgf("→ Data dir: %s", cfg.DataDir)

	// Initialize ConfigManager (needed for API config endpoints + hot reload).
	configMgrPath := effectiveConfigPath
	if configMgrPath == "" {
		configMgrPath = filepath.Join(cfg.DataDir, "config.yaml")
	}
	configMgr := config.NewManager(configMgrPath)

	// Hot reload support: watch config file and allow SIGHUP/API-triggered reload.
	cfgHolder := config.NewConfigHolder(cfg, config.NewLoader(configMgrPath, version), configMgrPath)

	var snap config.Snapshot
	if current := cfgHolder.Current(); current != nil {
		snap = *current
	} else {
		snap = config.BuildSnapshot(cfg, config.DefaultEnv())
	}
	cfg = snap.App

	// Configure proxy (enabled by default in v2.0 for Zero Config experience)

	// Create API handler
	apiDeps, err := buildAPIConstructorDeps(cfg, snap)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to build API constructor dependencies")
	}
	s, err := api.NewWithDeps(cfg, configMgr, apiDeps)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize API server")
	}
	if err := s.SetRootContext(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to set root context")
	}
	s.StartMonitors()
	s.SetConfigHolder(cfgHolder)
	// Initialize v3 components
	// Bus (In-Memory for MVP)
	v3Bus := bus.NewMemoryBus()

	// Session Store (Factory honors store.backend/store.path)
	v3Store, err := store.OpenStateStore(cfg.Store.Backend, filepath.Join(cfg.Store.Path, "sessions.sqlite"))
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize session store")
	}

	// Resume Store (SQLite if persisted, Memory otherwise)
	// Per ADR-021: Only sqlite and memory backends supported
	resumeStore, err := resume.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize resume store, falling back to memory")
		resumeStore, err = resume.NewStore("memory", "")
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to initialize fallback resume store")
		}
	}

	// Scan Manager & Store
	v3ScanStore, err := scan.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize scan store")
	}
	// Playlist filename from runtime or config (default internal/playlist.m3u)
	playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid playlist path")
	}
	// Initialize E2 Client for Smart Scanning (avoid dumb-scanner race condition)
	e2Opts := enigma2.Options{
		Timeout:               cfg.Enigma2.Timeout,
		ResponseHeaderTimeout: cfg.Enigma2.ResponseHeaderTimeout,
		MaxRetries:            cfg.Enigma2.Retries,
		Backoff:               cfg.Enigma2.Backoff,
		MaxBackoff:            cfg.Enigma2.MaxBackoff,
		Username:              cfg.Enigma2.Username,
		Password:              cfg.Enigma2.Password,
		UserAgent:             cfg.Enigma2.UserAgent,
		RateLimit:             rate.Limit(cfg.Enigma2.RateLimit),
		RateLimitBurst:        cfg.Enigma2.RateBurst,
		UseWebIFStreams:       cfg.Enigma2.UseWebIFStreams,
		StreamPort:            cfg.Enigma2.StreamPort,
	}
	e2Client := enigma2.NewClientWithOptions(cfg.Enigma2.BaseURL, e2Opts)

	v3Scan := scan.NewManager(v3ScanStore, playlistPath, e2Client)

	mediaPipeline := buildMediaPipeline(cfg, e2Client, logger)

	// Composition-root ownership: runtime wiring happens exactly once here.
	// The daemon worker must not mutate API runtime dependencies later.
	s.WireV3Runtime(v3.Dependencies{
		Bus:         v3Bus,
		Store:       v3Store,
		ResumeStore: resumeStore,
		Scan:        v3Scan,
	}, nil)

	// Phase 8: Start Recording Cache Eviction Worker (Background)
	go s.StartRecordingCacheEvicter(ctx)

	// Phase 8: Verification / Drift Detection
	// 1. Store (Secure Path)
	driftStatePath, err := paths.ResolveDataFilePath(cfg.DataDir, "drift_state.json", true)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to resolve drift state path")
	}
	verifyStore, err := verification.NewFileStore(driftStatePath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize verification store")
	}

	// 2. Checkers
	// Config Checker (Safe Provider)
	// cfgHolder implements checks.ConfigProvider interface (Current() *config.Snapshot)
	configCheck := checks.NewConfigChecker(effectiveConfigPath, cfgHolder)

	// Runtime Checker (FFmpeg + Go)
	// Use embedded runner
	runtimeCheck := checks.NewRuntimeChecker(checks.NewRealRunner(), runtime.Version(), "7.1.3")

	// 3. Worker
	// 60s cadence (or configured)
	if !cfg.Verification.Enabled {
		logger.Info().Msg("Verification worker disabled by config (XG2G_VERIFY_ENABLED=false)")
		verification.InitMetrics() // Ensure gauges serve 0 (Clean) instead of missing
	} else {
		verifier := verification.NewWorker(verifyStore, cfg.Verification.Interval, configCheck, runtimeCheck)
		go verifier.Start(ctx)
		s.SetVerificationStore(verifyStore) // Only expose status if we plan to write to it?
		// Actually, even if worker is off, reading from store (previous state) might be useful?
		// But if disabled, we probably shouldn't show stale state as "current".
		// User said: "Backout: Stop starting verification.Worker... /status will remain stable".
		// The store itself is just a file reader. Injecting it is safe.
		// So we inject it regardless? No, if we want to "disable drift detection", maybe we shouldn't show the drift block.
		// However, `s.SetVerificationStore` allows the API to read.
		// If the file exists from a previous run, it will show drift.
		// If the user effectively wants to turn it off, they might delete the file or ignore it.
		// But keeping it consistent: The API reads the file. The worker updates the file.
		// If worker is stopped, file is stale.
		// Current API implementation shows `LastCheck`. If it's old, it's old.
		// But okay, let's keep injection inside the 'else' block?
		// If I inject it, `/status` works.
		// If I don't inject it, `Drift` field is nil (clean).
		// User said: "/status will remain stable (no drift field) if worker is disabled." -> This implies I should NOT inject the store if disabled.
	}

	// Inject store ONLY if enabled (or always? see user note).
	// User said: "Stop starting verification.Worker ... /status will remain stable ... with no drift field".
	// This implies `s.Drift` will be nil. Use `s.SetVerificationStore` only if enabled.
	if cfg.Verification.Enabled {
		s.SetVerificationStore(verifyStore)
	}

	// Initial refresh (Async "Safety Net" for fast startup)
	// We run this in the background so the HTTP server binds ports immediately.
	// Users can disable with XG2G_INITIAL_REFRESH=false if needed
	if config.ParseBool("XG2G_INITIAL_REFRESH", true) {
		go func() {
			// Delay slightly to allow server to bind first (optional, but nice for logs)
			time.Sleep(100 * time.Millisecond)
			logger.Info().Msg("performing initial data refresh (background)")
			if st, err := jobs.Refresh(ctx, snap); err != nil {
				logger.Error().Err(err).Msg("initial data refresh failed")
				logger.Warn().Msg("→ Channels will be empty until manual refresh via /api/refresh")
			} else {
				logger.Info().Msg("initial data refresh completed successfully")
				// Update server status so UI shows correct "Last Sync" time
				s.UpdateStatus(*st)

				// Trigger v3 scan logic to ingest the newly written playlist
				logger.Info().Msg("triggering v3 data ingest")
				v3Scan.RunBackground()
			}
		}()
	} else {
		logger.Warn().Msg("Initial refresh is disabled (XG2G_INITIAL_REFRESH=false)")
		logger.Warn().Msg("→ No channels loaded. Trigger manual refresh via: POST /api/refresh")
	}

	// Build daemon dependencies
	metricsAddr := ""
	if cfg.MetricsEnabled {
		metricsAddr = strings.TrimSpace(cfg.MetricsAddr)
		if metricsAddr == "" {
			metricsAddr = ":9090"
		}
	}

	deps := daemon.Deps{
		Logger:          logger,
		Config:          cfg,
		ConfigManager:   configMgr,
		APIHandler:      s.Handler(),
		APIServerSetter: s,
		MetricsHandler:  promhttp.Handler(),
		MetricsAddr:     metricsAddr,

		ProxyOnly: false, // Deprecated, always false now

		// Inject Shared V3 Components
		V3Bus:       v3Bus,
		V3Store:     v3Store,
		ResumeStore: resumeStore,
		ScanManager: v3Scan,
		ReceiverHealthCheck: func(ctx context.Context) error {
			client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
				Timeout:  2 * time.Second,
				Username: cfg.Enigma2.Username,
				Password: cfg.Enigma2.Password,
			})
			_, err := client.About(ctx)
			return err
		},
		MediaPipeline: mediaPipeline,
		// DG-04: daemon receives only a factory port; concrete worker wiring stays in composition root.
		V3OrchestratorFactory: buildV3OrchestratorFactory(),
	}

	// Create daemon manager
	mgr, err := daemon.NewManager(serverCfg, deps)
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "manager.creation.failed").
			Msg("failed to create daemon manager")
	}
	var shutdownOnce sync.Once
	s.SetShutdownFunc(func(ctx context.Context) error {
		var shutdownErr error
		shutdownOnce.Do(func() {
			stop()
			if ctx == nil {
				shutdownErr = fmt.Errorf("shutdown context is nil")
				return
			}
			shutdownErr = mgr.Shutdown(ctx)
		})
		return shutdownErr
	})
	mgr.RegisterShutdownHook("api_server_shutdown", func(ctx context.Context) error {
		return s.Shutdown(ctx)
	})
	logger.Info().Msg("Starting daemon manager")

	// Configure Health Manager (Strict Mode)
	hm := s.HealthManager()
	if hm != nil {
		hm.SetReadyStrict(cfg.ReadyStrict)
		if cfg.ReadyStrict {
			if cfg.Enigma2.BaseURL == "" {
				// Strict mode requires a target to check. Fail startup if missing.
				logger.Fatal().Msg("Strict readiness enabled (XG2G_READY_STRICT=true) but OpenWebIF base URL is missing. Cannot perform strict checks.")
			}

			// Register strict OWI connectivity checker
			checker := health.NewReceiverChecker(func(ctx context.Context) error {
				client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
					Timeout:  2 * time.Second, // Client-side timeout
					Username: cfg.Enigma2.Username,
					Password: cfg.Enigma2.Password,
				})
				// Use the probe context (propagating the 2s timeout)
				_, err := client.About(ctx)
				return err
			})
			hm.RegisterChecker(checker)
			logger.Info().Msg("Strict readiness checks enabled: monitoring OpenWebIF connectivity")
		}
	}

	// Start daemon app (blocks until shutdown)
	app := daemon.NewApp(logger, mgr, cfgHolder, s, false)
	if err := app.Run(ctx); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "manager.failed").
			Msg("daemon app failed")
	}

	logger.Info().Msg("server exiting")
}
