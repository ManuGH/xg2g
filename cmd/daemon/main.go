// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
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
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/proxy"
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

// bindListenAddr replaces the host portion of a listen address (":8080") with
// the provided bind host/IP. If the listen address already contains a host,
// it is left untouched. Supports "if:<name>" to bind to the first IPv4 of an
// interface. Returns the adjusted listen address or an error.
func bindListenAddr(listenAddr, bind string) (string, error) {
	if bind == "" {
		return listenAddr, nil
	}

	// Only override when the listen address is just ":port" or empty.
	if listenAddr == "" || strings.HasPrefix(listenAddr, ":") {
		port := strings.TrimPrefix(listenAddr, ":")
		if port == "" {
			port = "0"
		}

		host := bind
		if strings.HasPrefix(bind, "if:") {
			ifName := strings.TrimPrefix(bind, "if:")
			iface, err := net.InterfaceByName(ifName)
			if err != nil {
				return "", fmt.Errorf("resolve interface %q: %w", ifName, err)
			}
			addrs, err := iface.Addrs()
			if err != nil {
				return "", fmt.Errorf("list addrs for %q: %w", ifName, err)
			}
			found := false
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLoopback() || ip.To4() == nil {
					continue
				}
				host = ip.String()
				found = true
				break
			}
			if !found {
				return "", fmt.Errorf("no suitable IPv4 on interface %q", ifName)
			}
		}

		return net.JoinHostPort(host, port), nil
	}

	return listenAddr, nil
}

func main() {
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

	// Load configuration with precedence: ENV > File > Defaults
	loader := config.NewLoader(*configPath, version)
	cfg, err := loader.Load()
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "config.load_failed").
			Str("config_path", *configPath).
			Msg("failed to load configuration")
	}

	// Log config source
	if *configPath != "" {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file").
			Str("path", *configPath).
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
	bindHost := os.Getenv("XG2G_BIND_INTERFACE")
	if bindHost != "" {
		if newListen, err := bindListenAddr(serverCfg.ListenAddr, bindHost); err != nil {
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
	logger.Info().Msgf("→ Stream port: %d", cfg.StreamPort)
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

	// Configure proxy (enabled by default in v2.0 for Zero Config experience)
	var streamDetector *openwebif.StreamDetector
	var proxyConfig *daemon.ProxyConfig

	if config.ParseBool("XG2G_ENABLE_STREAM_PROXY", true) {
		targetURL := config.ParseString("XG2G_PROXY_TARGET", "")
		receiverHost := proxy.GetReceiverHost()
		if receiverHost == "" && cfg.OWIBase != "" {
			if parsed, err := url.Parse(cfg.OWIBase); err == nil {
				receiverHost = parsed.Hostname()
			}
		}

		// PROXY_TARGET is now optional - if not provided, use Smart Detection
		if targetURL == "" && receiverHost == "" {
			logger.Fatal().
				Str("event", "proxy.config.invalid").
				Msg("XG2G_ENABLE_STREAM_PROXY is true but neither XG2G_PROXY_TARGET nor XG2G_OWI_BASE is set")
		}

		// Create StreamDetector if Smart Detection is enabled AND Instant Tune is enabled
		if receiverHost != "" && openwebif.IsEnabled() && cfg.InstantTuneEnabled {
			streamDetector = openwebif.NewStreamDetector(receiverHost, xglog.WithComponent("stream-detector"))
			if targetURL == "" {
				logger.Info().
					Str("receiver", receiverHost).
					Msg("Stream proxy using Smart Detection (automatic port selection)")
			} else {
				logger.Info().
					Str("receiver", receiverHost).
					Str("target", targetURL).
					Msg("Stream proxy using Smart Detection with fallback target")
			}
		} else if !cfg.InstantTuneEnabled {
			logger.Info().Msg("Instant Tune (StreamDetector) is disabled by configuration")
		}

		proxyConfig = &daemon.ProxyConfig{
			ListenAddr:     config.ParseString("XG2G_PROXY_LISTEN", ":18000"),
			TargetURL:      targetURL,
			ReceiverHost:   receiverHost,
			StreamDetector: streamDetector,
			Logger:         xglog.WithComponent("proxy"),
			TLSCert:        cfg.TLSCert,
			TLSKey:         cfg.TLSKey,
			DataDir:        cfg.DataDir,
			PlaylistPath:   filepath.Join(cfg.DataDir, "playlist.m3u"), // Default name
		}
		if bindHost != "" {
			if newListen, err := bindListenAddr(proxyConfig.ListenAddr, bindHost); err != nil {
				logger.Fatal().
					Err(err).
					Msg("invalid XG2G_BIND_INTERFACE for proxy listen")
			} else {
				proxyConfig.ListenAddr = newListen
			}
		}

		// Allow overriding playlist filename if needed
		playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
		if playlistName != "" {
			proxyConfig.PlaylistPath = filepath.Join(cfg.DataDir, playlistName)
		}
	}

	// Initial refresh before starting servers (enabled by default in v2.0)
	// Users can disable with XG2G_INITIAL_REFRESH=false if needed
	if config.ParseBool("XG2G_INITIAL_REFRESH", true) {
		logger.Info().Msg("performing initial data refresh on startup")
		// Instant Tune: Pass streamDetector to pre-warm cache
		if _, err := jobs.Refresh(ctx, cfg, streamDetector); err != nil {
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
	configMgr := config.NewManager(*configPath)
	if *configPath == "" {
		// If no config file specified, default to data dir
		configMgr = config.NewManager("config.yaml") // or data/config.yaml?
		// User requested precedence: ENV > config.yaml > defaults.
		// If configPath is empty, Loader uses defaults.
		// If we want to save changes, we need a path.
		// Let's defer to a safe default if not provided: "./config.yaml" or cfg.DataDir/config.yaml
		// For now, let's use "config.yaml" in CWD if not specified, matching legacy behavior potentially?
		// Better: use the *configPath if set, otherwise "config.yaml"
	}

	// Create API handler
	s := api.New(cfg, streamDetector, configMgr)

	// Build daemon dependencies
	deps := daemon.Deps{
		Logger:         logger,
		Config:         cfg,
		ConfigManager:  configMgr,
		APIHandler:     s.Handler(),
		MetricsHandler: promhttp.Handler(),
		ProxyConfig:    proxyConfig,
	}

	// Log unique build ID to verify deployment
	logger.Info().Str("build_uuid", "DEPLOY_CHECK_ABC123").Msg("Daemon starting...")

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
