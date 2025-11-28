// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/proxy"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Version = "v3.0.6"

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
	// Handle command-line flags
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (YAML)")
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	logger := xglog.WithComponent("daemon")

	// Create a context that listens for the interrupt signal from the OS
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load configuration with precedence: ENV > File > Defaults
	loader := config.NewLoader(*configPath, Version)
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

	// Parse server configuration
	serverCfg := config.ParseServerConfig()

	logger.Info().
		Str("event", "startup").
		Str("version", Version).
		Str("addr", serverCfg.ListenAddr).
		Msg("starting xg2g")

	// Log key configuration
	logger.Info().Msgf("→ Receiver: %s (auth: %v)", maskURL(cfg.OWIBase), cfg.OWIUsername != "")
	logger.Info().Msgf("→ Bouquet: %s", cfg.Bouquet)
	logger.Info().Msgf("→ Stream port: %d", cfg.StreamPort)
	logger.Info().Msgf("→ EPG: %s (%d days)", cfg.XMLTVPath, cfg.EPGDays)
	if cfg.APIToken != "" {
		logger.Info().Msg("→ API token: configured")
	}
	logger.Info().Msgf("→ Data dir: %s", cfg.DataDir)

	// Initial refresh before starting servers (enabled by default in v2.0)
	// Users can disable with XG2G_INITIAL_REFRESH=false if needed
	if config.ParseBool("XG2G_INITIAL_REFRESH", true) {
		logger.Info().Msg("performing initial data refresh on startup")
		if _, err := jobs.Refresh(ctx, cfg); err != nil {
			logger.Error().Err(err).Msg("initial data refresh failed")
			logger.Warn().Msg("→ Channels will be empty until manual refresh via /api/refresh")
		} else {
			logger.Info().Msg("initial data refresh completed successfully")
		}
	} else {
		logger.Warn().Msg("Initial refresh is disabled (XG2G_INITIAL_REFRESH=false)")
		logger.Warn().Msg("→ No channels loaded. Trigger manual refresh via: POST /api/refresh")
	}

	// Create API handler
	s := api.New(cfg)

	// Build daemon dependencies
	deps := daemon.Deps{
		Logger:         logger,
		Config:         cfg,
		APIHandler:     s.Handler(),
		MetricsHandler: promhttp.Handler(),
	}

	// Configure proxy (enabled by default in v2.0 for Zero Config experience)
	if config.ParseBool("XG2G_ENABLE_STREAM_PROXY", true) {
		targetURL := config.ParseString("XG2G_PROXY_TARGET", "")
		receiverHost := proxy.GetReceiverHost()

		// PROXY_TARGET is now optional - if not provided, use Smart Detection
		if targetURL == "" && receiverHost == "" {
			logger.Fatal().
				Str("event", "proxy.config.invalid").
				Msg("XG2G_ENABLE_STREAM_PROXY is true but neither XG2G_PROXY_TARGET nor XG2G_OWI_BASE is set")
		}

		// Create StreamDetector if Smart Detection is enabled
		var streamDetector *openwebif.StreamDetector
		if receiverHost != "" && openwebif.IsEnabled() {
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
		}

		deps.ProxyConfig = &daemon.ProxyConfig{
			ListenAddr:     config.ParseString("XG2G_PROXY_LISTEN", ":18000"),
			TargetURL:      targetURL,
			ReceiverHost:   receiverHost,
			StreamDetector: streamDetector,
			Logger:         xglog.WithComponent("proxy"),
			TLSCert:        cfg.TLSCert,
			TLSKey:         cfg.TLSKey,
		}
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
