package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/daemon"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	xgtls "github.com/ManuGH/xg2g/internal/tls"
	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/ManuGH/xg2g/internal/verification/checks"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// Container is the production composition root output.
type Container struct {
	Config        config.AppConfig
	ConfigManager *config.Manager
	ConfigHolder  *config.ConfigHolder
	Logger        zerolog.Logger
	Server        *api.Server
	Manager       daemon.Manager
	App           *daemon.App

	snapshot         config.Snapshot
	scanManager      *scan.Manager
	verificationWork *verification.Worker

	startOnce        sync.Once
	runtimeHooksOnce sync.Once
}

// WireServices builds the production dependency graph and returns a runnable container.
func WireServices(ctx context.Context, version, commit, buildDate, explicitConfigPath string) (*Container, error) {
	if ctx == nil {
		return nil, fmt.Errorf("wire services context is nil")
	}

	xglog.Configure(xglog.Config{
		Level:   "info",
		Service: "xg2g",
		Version: version,
	})
	logger := xglog.WithComponent("bootstrap")

	effectiveConfigPath, explicitMode, err := resolveConfigPath(strings.TrimSpace(explicitConfigPath))
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	loader := config.NewLoader(effectiveConfigPath, version)
	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	xglog.Configure(xglog.Config{
		Level:   cfg.LogLevel,
		Service: cfg.LogService,
		Version: cfg.Version,
	})
	logger = xglog.WithComponent("bootstrap")

	if explicitMode {
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

	if configBytes, marshalErr := json.Marshal(cfg); marshalErr == nil {
		hash := sha256.Sum256(configBytes)
		logger.Info().
			Str("event", "config.snapshot").
			Str("sha256", fmt.Sprintf("%x", hash)).
			Msg("configuration snapshot fingerprint")
	}

	if cfg.Engine.Enabled && !cfg.ConfigStrict {
		logger.Warn().
			Str("event", "config.strict.disabled").
			Msg("v3 strict validation disabled via XG2G_V3_CONFIG_STRICT override")
	}

	if err := health.PerformStartupChecks(ctx, cfg); err != nil {
		return nil, fmt.Errorf("startup checks failed: %w", err)
	}

	serverCfg := config.ParseServerConfigForApp(cfg)

	bindHost := strings.TrimSpace(config.ParseString("XG2G_BIND_INTERFACE", ""))
	if bindHost != "" {
		newListen, err := config.BindListenAddr(serverCfg.ListenAddr, bindHost)
		if err != nil {
			return nil, fmt.Errorf("invalid XG2G_BIND_INTERFACE for API listen: %w", err)
		}
		serverCfg.ListenAddr = newListen
	}

	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		if cfg.TLSCert == "" || cfg.TLSKey == "" {
			return nil, fmt.Errorf("both XG2G_TLS_CERT and XG2G_TLS_KEY must be set together")
		}
		logger.Info().Str("cert", cfg.TLSCert).Str("key", cfg.TLSKey).Msg("using user-provided TLS certificates")
	} else if cfg.TLSEnabled {
		tlsCfg := xgtls.Config{CertPath: cfg.TLSCert, KeyPath: cfg.TLSKey, Logger: logger}
		certPath, keyPath, err := xgtls.EnsureCertificates(tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to ensure TLS certificates: %w", err)
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
	if strings.TrimSpace(cfg.APIToken) != "" {
		logger.Info().Str("event", "auth.configured").Msg("→ API token: configured")
	} else if len(cfg.APITokens) > 0 {
		logger.Info().Str("event", "auth.configured").Msg("→ API tokens: configured")
	} else {
		return nil, fmt.Errorf("no API tokens configured: set XG2G_API_TOKEN or XG2G_API_TOKENS")
	}
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		logger.Info().Msgf("→ TLS: enabled (cert: %s, key: %s)", cfg.TLSCert, cfg.TLSKey)
	}
	logger.Info().Msgf("→ Data dir: %s", cfg.DataDir)

	configMgrPath := effectiveConfigPath
	if configMgrPath == "" {
		configMgrPath = filepath.Join(cfg.DataDir, "config.yaml")
	}
	configMgr := config.NewManager(configMgrPath)
	cfgHolder := config.NewConfigHolder(cfg, config.NewLoader(configMgrPath, version), configMgrPath)

	var snap config.Snapshot
	if current := cfgHolder.Current(); current != nil {
		snap = *current
	} else {
		snap = config.BuildSnapshot(cfg, config.DefaultEnv())
	}
	cfg = snap.App

	apiDeps := buildAPIConstructorDeps(cfg, snap, logger)
	s, err := api.NewWithDeps(cfg, configMgr, apiDeps)
	if err != nil {
		return nil, fmt.Errorf("initialize api server: %w", err)
	}
	if err := s.SetRootContext(ctx); err != nil {
		return nil, fmt.Errorf("set api root context: %w", err)
	}
	s.StartMonitors()
	s.SetConfigHolder(cfgHolder)

	v3Bus := pipebus.NewMemoryBus()
	v3Store, err := sessionstore.OpenStateStore(cfg.Store.Backend, filepath.Join(cfg.Store.Path, "sessions.sqlite"))
	if err != nil {
		return nil, fmt.Errorf("initialize session store: %w", err)
	}

	resumeStore, err := resume.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize resume store, falling back to memory")
		resumeStore, err = resume.NewStore("memory", "")
		if err != nil {
			return nil, fmt.Errorf("initialize fallback resume store: %w", err)
		}
	}

	v3ScanStore, err := scan.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		return nil, fmt.Errorf("initialize scan store: %w", err)
	}

	playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
	if err != nil {
		return nil, fmt.Errorf("invalid playlist path: %w", err)
	}

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

	s.WireV3Runtime(v3.Dependencies{
		Bus:         v3Bus,
		Store:       v3Store,
		ResumeStore: resumeStore,
		Scan:        v3Scan,
	}, nil)

	driftStatePath, err := paths.ResolveDataFilePath(cfg.DataDir, "drift_state.json", true)
	if err != nil {
		return nil, fmt.Errorf("resolve drift state path: %w", err)
	}
	verifyStore, err := verification.NewFileStore(driftStatePath)
	if err != nil {
		return nil, fmt.Errorf("initialize verification store: %w", err)
	}

	configCheck := checks.NewConfigChecker(effectiveConfigPath, cfgHolder)
	runtimeCheck := checks.NewRuntimeChecker(checks.NewRealRunner(), runtime.Version(), "7.1.3")
	var verifyWorker *verification.Worker
	if !cfg.Verification.Enabled {
		logger.Info().Msg("Verification worker disabled by config (XG2G_VERIFY_ENABLED=false)")
		verification.InitMetrics()
	} else {
		verifyWorker = verification.NewWorker(verifyStore, cfg.Verification.Interval, configCheck, runtimeCheck)
		s.SetVerificationStore(verifyStore)
	}

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
		ProxyOnly:       false,
		V3Bus:           v3Bus,
		V3Store:         v3Store,
		ResumeStore:     resumeStore,
		ScanManager:     v3Scan,
		ReceiverHealthCheck: func(ctx context.Context) error {
			if e2Client == nil || e2Client.HTTPClient == nil {
				return fmt.Errorf("enigma2 client is not available")
			}
			if strings.TrimSpace(e2Client.BaseURL) == "" {
				return fmt.Errorf("XG2G_V3_E2_HOST is empty")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, e2Client.BaseURL, nil)
			if err != nil {
				return err
			}
			if cfg.Enigma2.UserAgent != "" {
				req.Header.Set("User-Agent", cfg.Enigma2.UserAgent)
			}
			if cfg.Enigma2.Username != "" || cfg.Enigma2.Password != "" {
				req.SetBasicAuth(cfg.Enigma2.Username, cfg.Enigma2.Password)
			}
			resp, err := e2Client.HTTPClient.Do(req)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("receiver returned status %d", resp.StatusCode)
			}
			return nil
		},
		MediaPipeline:         mediaPipeline,
		V3OrchestratorFactory: buildV3OrchestratorFactory(),
	}

	mgr, err := daemon.NewManager(serverCfg, deps)
	if err != nil {
		return nil, fmt.Errorf("create daemon manager: %w", err)
	}

	hm := s.HealthManager()
	if hm != nil {
		hm.SetReadyStrict(cfg.ReadyStrict)
		if cfg.ReadyStrict {
			if strings.TrimSpace(cfg.Enigma2.BaseURL) == "" {
				return nil, fmt.Errorf("strict readiness enabled but OpenWebIF base URL is missing")
			}
			checker := health.NewReceiverChecker(func(ctx context.Context) error {
				client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
					Timeout:  2 * time.Second,
					Username: cfg.Enigma2.Username,
					Password: cfg.Enigma2.Password,
				})
				_, err := client.About(ctx)
				return err
			})
			hm.RegisterChecker(checker)
			logger.Info().Msg("Strict readiness checks enabled: monitoring OpenWebIF connectivity")
		}
	}

	app := daemon.NewApp(logger, mgr, cfgHolder, s, false)

	return &Container{
		Config:           cfg,
		ConfigManager:    configMgr,
		ConfigHolder:     cfgHolder,
		Logger:           logger,
		Server:           s,
		Manager:          mgr,
		App:              app,
		snapshot:         snap,
		scanManager:      v3Scan,
		verificationWork: verifyWorker,
	}, nil
}

// Start launches bootstrap-owned background workers.
func (c *Container) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("start context is nil")
	}
	if c == nil {
		return fmt.Errorf("container is nil")
	}
	if c.Server == nil {
		return fmt.Errorf("container server is nil")
	}

	c.startOnce.Do(func() {
		go c.Server.StartRecordingCacheEvicter(ctx)

		if c.verificationWork != nil {
			go c.verificationWork.Start(ctx)
		}

		if config.ParseBool("XG2G_INITIAL_REFRESH", true) {
			go c.runInitialRefresh(ctx)
		} else {
			c.Logger.Warn().Msg("Initial refresh is disabled (XG2G_INITIAL_REFRESH=false)")
			c.Logger.Warn().Msg("→ No channels loaded. Trigger manual refresh via: POST /api/refresh")
		}
	})

	return nil
}

// Run starts the daemon app loop.
func (c *Container) Run(ctx context.Context, stop context.CancelFunc) error {
	if ctx == nil {
		return fmt.Errorf("run context is nil")
	}
	if c == nil {
		return fmt.Errorf("container is nil")
	}
	if c.App == nil || c.Manager == nil || c.Server == nil {
		return fmt.Errorf("container is not fully initialized")
	}

	c.runtimeHooksOnce.Do(func() {
		var shutdownOnce sync.Once
		c.Server.SetShutdownFunc(func(shutdownCtx context.Context) error {
			var shutdownErr error
			shutdownOnce.Do(func() {
				if stop != nil {
					stop()
				}
				if shutdownCtx == nil {
					shutdownErr = fmt.Errorf("shutdown context is nil")
					return
				}
				shutdownErr = c.Manager.Shutdown(shutdownCtx)
			})
			return shutdownErr
		})
		c.Manager.RegisterShutdownHook("api_server_shutdown", func(shutdownCtx context.Context) error {
			return c.Server.Shutdown(shutdownCtx)
		})
	})

	return c.App.Run(ctx)
}

func (c *Container) runInitialRefresh(ctx context.Context) {
	time.Sleep(100 * time.Millisecond)
	c.Logger.Info().Msg("performing initial data refresh (background)")
	st, err := jobs.Refresh(ctx, c.snapshot)
	if err != nil {
		c.Logger.Error().Err(err).Msg("initial data refresh failed")
		c.Logger.Warn().Msg("→ Channels will be empty until manual refresh via /api/refresh")
		return
	}

	c.Logger.Info().Msg("initial data refresh completed successfully")
	c.Server.UpdateStatus(*st)

	if c.scanManager != nil {
		c.Logger.Info().Msg("triggering v3 data ingest")
		c.scanManager.RunBackground()
	}
}

func resolveConfigPath(explicit string) (path string, explicitMode bool, err error) {
	if explicit != "" {
		absPath, err := filepath.Abs(explicit)
		if err != nil {
			return "", true, fmt.Errorf("resolve absolute path for explicit config %q: %w", explicit, err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return "", true, fmt.Errorf("explicit config file not found %q: %w", absPath, err)
		}
		if info.IsDir() {
			return "", true, fmt.Errorf("explicit config path %q is a directory", absPath)
		}
		return absPath, true, nil
	}

	dataDir := strings.TrimSpace(config.ParseString("XG2G_DATA", "/tmp"))
	if dataDir == "" {
		dataDir = "/tmp"
	}
	autoPath := filepath.Join(dataDir, "config.yaml")
	if info, err := os.Stat(autoPath); err == nil && !info.IsDir() {
		absPath, absErr := filepath.Abs(autoPath)
		if absErr == nil {
			return absPath, false, nil
		}
	}

	return "", false, nil
}

func maskURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid_url]"
	}
	parsedURL.User = nil
	return parsedURL.String()
}
