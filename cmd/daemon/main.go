// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"path/filepath"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	xgtls "github.com/ManuGH/xg2g/internal/tls"
	"github.com/ManuGH/xg2g/internal/validation"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	version   = "v3.2.0-85-g441be91-dirty"
	commit    = "441be91"
	buildDate = "2025-12-11T18:58:00Z"
)

// maskURL removes user info from a URL string for safe logging.
func maskURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "invalid-url-redacted"
	}
	parsedURL.User = nil
	return parsedURL.String()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "config" {
		os.Exit(runConfigCLI(os.Args[2:]))
	}

	// Handle command-line flags
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (YAML)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s (commit: %s, built: %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	logger := xglog.WithComponent("daemon")

	// Create a context that listens for the interrupt signal from the OS
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Determine config path:
	// - Explicit via --config
	// - Otherwise auto-load ${XG2G_DATA}/config.yaml if it exists (so UI-saved config persists)
	explicitConfigPath := strings.TrimSpace(*configPath)
	effectiveConfigPath := explicitConfigPath
	if effectiveConfigPath == "" {
		dataDir := strings.TrimSpace(os.Getenv("XG2G_DATA"))
		if dataDir == "" {
			dataDir = "/tmp"
		}
		autoPath := filepath.Join(dataDir, "config.yaml")
		if _, err := os.Stat(autoPath); err == nil {
			effectiveConfigPath = autoPath
		}
	}

	// Load configuration with precedence: ENV > File > Defaults
	loader := config.NewLoader(effectiveConfigPath, version)
	cfg, err := loader.Load()
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "config.load_failed").
			Str("config_path", effectiveConfigPath).
			Msg("failed to load configuration")
	}

	// Log config source
	if explicitConfigPath != "" {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file").
			Str("path", explicitConfigPath).
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

	// Legacy: Determine XMLTV path if EPG is enabled and no explicit path is set
	if cfg.EPGEnabled && cfg.XMLTVPath == "" {
		cfg.XMLTVPath = "xmltv.xml"
		logger.Info().
			Str("xmltv_path", cfg.XMLTVPath).
			Msg("EPG enabled but no XMLTV path set, using default")
	}

	// -------------------------------------------------------------------------
	// Pre-flight Checks (Fail Fast)
	// -------------------------------------------------------------------------
	if err := validation.PerformStartupChecks(ctx, cfg); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "startup.check_failed").
			Msg("Startup checks failed. Please verify configuration and permissions.")
	}
	// -------------------------------------------------------------------------

	// Parse server configuration
	serverCfg := config.ParseServerConfig()

	// Allow config.yaml to set the API listen address, but keep ENV as the highest priority.
	// ENV precedence: XG2G_LISTEN / XG2G_API_ADDR > config.yaml api.listenAddr > defaults.
	if _, ok := os.LookupEnv("XG2G_LISTEN"); !ok {
		if _, ok := os.LookupEnv("XG2G_API_ADDR"); !ok {
			if strings.TrimSpace(cfg.APIListenAddr) != "" {
				serverCfg.ListenAddr = cfg.APIListenAddr
			}
		}
	}

	bindHost := os.Getenv("XG2G_BIND_INTERFACE")
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
	} else if config.ParseBool("XG2G_TLS_ENABLED", false) {
		// Auto-generate self-signed certificates
		tlsCfg := xgtls.Config{
			CertPath: config.ParseString("XG2G_TLS_CERT", ""),
			KeyPath:  config.ParseString("XG2G_TLS_KEY", ""),
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
	logger.Info().Msgf("→ Receiver: %s (auth: %v)", maskURL(cfg.OWIBase), cfg.OWIUsername != "")
	logger.Info().Msgf("→ Bouquet: %s", cfg.Bouquet)
	if cfg.UseWebIFStreams {
		logger.Info().Msg("→ Stream: OpenWebIF /web/stream.m3u (receiver decides 8001/17999 internally)")
	} else {
		logger.Info().Msgf("→ Stream port: %d (direct TS)", cfg.StreamPort)
	}
	logger.Info().Msgf("→ EPG: %s (%d days)", cfg.XMLTVPath, cfg.EPGDays)
	if cfg.APIToken != "" {
		logger.Info().Msg("→ API token: configured")
	} else {
		logger.Warn().
			Str("security", "weak").
			Msg("→ API token: NOT configured (Auth Disabled). Set XG2G_API_TOKEN for security.")
	}
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		logger.Info().Msgf("→ TLS: enabled (cert: %s, key: %s)", cfg.TLSCert, cfg.TLSKey)
	}
	logger.Info().Msgf("→ Data dir: %s", cfg.DataDir)

	snap := config.BuildSnapshot(cfg)

	// Configure proxy (enabled by default in v2.0 for Zero Config experience)
	var proxyConfig *daemon.ProxyConfig

	if snap.Runtime.StreamProxy.Enabled {
		targetURL := strings.TrimSpace(snap.Runtime.StreamProxy.TargetURL)
		receiverHost := ""
		if cfg.OWIBase != "" {
			if parsed, err := url.Parse(cfg.OWIBase); err == nil {
				receiverHost = parsed.Hostname()
			}
		}

		// PROXY_TARGET is now optional - if not provided, we still require ReceiverHost for Web-API access
		if targetURL == "" && receiverHost == "" {
			logger.Fatal().
				Str("event", "proxy.config.invalid").
				Msg("XG2G_ENABLE_STREAM_PROXY is true but neither XG2G_PROXY_TARGET nor XG2G_OWI_BASE is set")
		}

		proxyConfig = &daemon.ProxyConfig{
			ListenAddr:   snap.Runtime.StreamProxy.ListenAddr,
			TargetURL:    targetURL,
			ReceiverHost: receiverHost,
			Logger:       xglog.WithComponent("proxy"),
			TLSCert:      cfg.TLSCert,
			TLSKey:       cfg.TLSKey,
			DataDir:      cfg.DataDir,
			PlaylistPath: filepath.Join(cfg.DataDir, snap.Runtime.PlaylistFilename),
			Runtime:      snap.Runtime,
		}
		if bindHost != "" {
			if newListen, err := config.BindListenAddr(proxyConfig.ListenAddr, bindHost); err != nil {
				logger.Fatal().
					Err(err).
					Msg("invalid XG2G_BIND_INTERFACE for proxy listen")
			} else {
				proxyConfig.ListenAddr = newListen
			}
		}

	}

	// Initial refresh before starting servers (enabled by default in v2.0)
	// Users can disable with XG2G_INITIAL_REFRESH=false if needed
	if config.ParseBool("XG2G_INITIAL_REFRESH", true) {
		logger.Info().Msg("performing initial data refresh on startup")
		if _, err := jobs.Refresh(ctx, snap); err != nil {
			logger.Error().Err(err).Msg("initial data refresh failed")
			logger.Warn().Msg("→ Channels will be empty until manual refresh via /api/refresh")
		} else {
			logger.Info().Msg("initial data refresh completed successfully")
		}
	} else {
		logger.Warn().Msg("Initial refresh is disabled (XG2G_INITIAL_REFRESH=false)")
		logger.Warn().Msg("→ No channels loaded. Trigger manual refresh via: POST /api/refresh")
	}

	// Initialize ConfigManager
	configMgrPath := effectiveConfigPath
	if configMgrPath == "" {
		configMgrPath = filepath.Join(cfg.DataDir, "config.yaml")
	}
	configMgr := config.NewManager(configMgrPath)

	// Hot reload support: watch config file and allow SIGHUP/API-triggered reload.
	cfgHolder := config.NewConfigHolder(cfg, config.NewLoader(configMgrPath, version), configMgrPath)
	if err := cfgHolder.StartWatcher(ctx); err != nil {
		logger.Warn().
			Err(err).
			Str("event", "config.watcher_start_failed").
			Str("path", configMgrPath).
			Msg("failed to start config watcher")
	}

	// Create API handler
	s := api.New(cfg, configMgr)
	s.SetConfigHolder(cfgHolder)

	if proxyConfig != nil {
		// Build proxy server to inject into API
		// NOTE: We recreate the proxy logic inside daemon manager usually.
		// But here we need the INSTANCE.
		// The daemon manager creates the proxy instance.
		// We need to pass the *proxy.Server instance from manager to API?
		// OR, create it here and pass it to manager?
		// Manager takes 'deps'.
	}

	applyCh := make(chan config.AppConfig, 1)
	cfgHolder.RegisterListener(applyCh)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case newCfg := <-applyCh:
				s.ApplyConfig(newCfg)
			}
		}
	}()

	// Use SIGHUP as a config reload trigger (non-fatal).
	hupChan := make(chan os.Signal, 1)
	signal.Notify(hupChan, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hupChan:
				logger.Info().
					Str("event", "config.reload_signal").
					Str("signal", "SIGHUP").
					Msg("received SIGHUP, reloading config")
				if err := cfgHolder.Reload(context.Background()); err != nil {
					logger.Warn().
						Err(err).
						Str("event", "config.reload_failed").
						Msg("config reload failed")
				}
			}
		}
	}()

	// Build daemon dependencies
	metricsAddr := ""
	if cfg.MetricsEnabled {
		metricsAddr = strings.TrimSpace(cfg.MetricsAddr)
		if metricsAddr == "" {
			metricsAddr = ":9090"
		}
	}

	deps := daemon.Deps{
		Logger:         logger,
		Config:         cfg,
		ConfigManager:  configMgr,
		APIHandler:     s.Handler(),
		MetricsHandler: promhttp.Handler(),
		MetricsAddr:    metricsAddr,
		ProxyConfig:    proxyConfig,
	}

	// Create daemon manager
	mgr, err := daemon.NewManager(serverCfg, deps)
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "manager.creation.failed").
			Msg("failed to create daemon manager")
	}

	// Start SSDP announcer for HDHomeRun auto-discovery if enabled (not in proxy-only mode)
	proxyOnlyMode := config.ParseString("XG2G_PROXY_ONLY_MODE", "false") == "true"
	if !proxyOnlyMode {
		if hdhrSrv := s.HDHomeRunServer(); hdhrSrv != nil {
			go func() {
				if err := hdhrSrv.StartSSDPAnnouncer(ctx); err != nil {
					logger.Error().
						Err(err).
						Str("event", "ssdp.failed").
						Msg("SSDP announcer failed")
				}
			}()

			// Register shutdown hook for SSDP cleanup
			mgr.RegisterShutdownHook("ssdp_announcer", func(shutdownCtx context.Context) error {
				logger.Info().Msg("Stopping SSDP announcer")
				// SSDP announcer stops when context is cancelled
				return nil
			})
		}
	}

	// Start daemon manager (blocks until shutdown)
	if err := mgr.Start(ctx); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "manager.failed").
			Msg("daemon manager failed")
	}

	logger.Info().Msg("server exiting")
}
