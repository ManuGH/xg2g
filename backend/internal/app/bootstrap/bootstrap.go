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
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/daemon"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/ManuGH/xg2g/internal/receipts"
	receiptamazon "github.com/ManuGH/xg2g/internal/receipts/amazon"
	receiptgoogle "github.com/ManuGH/xg2g/internal/receipts/google"
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
	piconPool        *jobs.PiconPool
	scanManager      *scan.Manager
	verificationWork *verification.Worker

	startOnce        sync.Once
	runtimeHooksOnce sync.Once
}

type wireBootstrapState struct {
	cfg                 config.AppConfig
	logger              zerolog.Logger
	effectiveConfigPath string
	serverCfg           config.ServerConfig
}

// WireServices builds the production dependency graph and returns a runnable container.
func WireServices(ctx context.Context, version, commit, buildDate, explicitConfigPath string) (*Container, error) {
	if ctx == nil {
		return nil, fmt.Errorf("wire services context is nil")
	}

	state, err := prepareWireBootstrapState(ctx, version, explicitConfigPath)
	if err != nil {
		return nil, err
	}

	cfg := state.cfg
	logger := state.logger
	effectiveConfigPath := state.effectiveConfigPath
	serverCfg := state.serverCfg

	if err := ensureWireTLSCertificates(&cfg, logger); err != nil {
		return nil, err
	}
	if err := logWireStartup(logger, cfg, version, commit, buildDate, serverCfg.ListenAddr); err != nil {
		return nil, err
	}

	// L22: promote ForceHTTPS BEFORE cfg is consumed below. buildWireConfigState snapshots
	// this cfg (BuildSnapshot passes App through unchanged, and the holder snapshots the input
	// cfg — no file re-load), and api.NewWithDeps builds the server from it. Promoting it
	// afterwards (as the code did) left both the snapshot and the running server with
	// ForceHTTPS=false despite TLS being enabled, so the HTTPS-only posture was never advertised.
	if cfg.TLSEnabled && !cfg.ForceHTTPS {
		cfg.ForceHTTPS = true
		logger.Info().Msg("tls enabled - advertising HTTPS-only posture (ForceHTTPS)")
	}

	configMgr, cfgHolder, snap, cfg := buildWireConfigState(cfg, version, effectiveConfigPath)

	apiDeps := buildAPIConstructorDeps(cfg, snap, logger)
	s, err := api.NewWithDeps(cfg, configMgr, apiDeps, api.WithRootContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("initialize api server: %w", err)
	}
	s.StartMonitors()
	s.SetConfigHolder(cfgHolder)

	v3Bus := pipebus.NewMemoryBus()
	v3Store, err := sessionstore.OpenStateStore(cfg.Store.Backend, filepath.Join(cfg.Store.Path, "sessions.sqlite"))
	if err != nil {
		return nil, fmt.Errorf("initialize session store: %w", err)
	}
	deviceAuthStore, err := deviceauthstore.OpenStateStore(cfg.Store.Backend, filepath.Join(cfg.Store.Path, "deviceauth.sqlite"))
	if err != nil {
		return nil, fmt.Errorf("initialize device auth store: %w", err)
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
	decisionAuditStore, err := decisionaudit.NewAuditStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize decision audit store, continuing without decision history")
		decisionAuditStore = nil
	}
	capabilityRegistry, err := capreg.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize capability registry, continuing without capability history")
		capabilityRegistry = nil
	}
	monetization, err := buildMonetizationServices(cfg)
	if err != nil {
		return nil, err
	}
	entitlementService := monetization.entitlements
	householdService := monetization.households
	receiptService := monetization.receipts

	playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
	if err != nil {
		return nil, fmt.Errorf("invalid playlist path: %w", err)
	}

	e2Client, owiClient := buildEnigmaClients(cfg, snap)

	v3Scan := scan.NewManager(v3ScanStore, playlistPath, e2Client)
	v3Scan.ActivePlaybackFn = newBackgroundScanPlaybackDetector(v3Store, owiClient)
	mediaPipeline := buildMediaPipeline(cfg, e2Client, logger)

	s.WireV3Runtime(v3.Dependencies{
		Bus:                v3Bus,
		Store:              v3Store,
		DeviceAuthStore:    deviceAuthStore,
		ResumeStore:        resumeStore,
		Scan:               v3Scan,
		DecisionAudit:      decisionAuditStore,
		CapabilityRegistry: capabilityRegistry,
		Entitlements:       entitlementService,
		Households:         householdService,
		Receipts:           receiptService,
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
	runtimeCheck := checks.NewRuntimeChecker(checks.NewRealRunner(), runtime.Version(), "8.1")
	var verifyWorker *verification.Worker
	if !cfg.Verification.Enabled {
		logger.Info().Msg("Verification worker disabled by config (XG2G_VERIFY_ENABLED=false)")
		verification.InitMetrics()
	} else {
		verifyWorker = verification.NewWorker(verifyStore, cfg.Verification.Interval, configCheck, runtimeCheck)
		s.SetVerificationStore(verifyStore)
	}

	metricsAddr := resolveMetricsAddr(cfg)

	deps := daemon.Deps{
		Logger:                logger,
		Config:                cfg,
		ConfigManager:         configMgr,
		APIHandler:            s.Handler(),
		APIServerSetter:       s,
		MetricsHandler:        promhttp.Handler(),
		MetricsAddr:           metricsAddr,
		ProxyOnly:             false,
		V3Bus:                 v3Bus,
		V3Store:               v3Store,
		ResumeStore:           resumeStore,
		ScanManager:           v3Scan,
		ReceiverHealthCheck:   newReceiverHealthCheck(cfg, e2Client),
		MediaPipeline:         mediaPipeline,
		V3OrchestratorFactory: buildV3OrchestratorFactory(),
	}

	mgr, err := daemon.NewManager(serverCfg, deps)
	if err != nil {
		return nil, fmt.Errorf("create daemon manager: %w", err)
	}

	if err := registerHealthCheckers(s.HealthManager(), cfg, cfgHolder, logger); err != nil {
		return nil, err
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

// monetizationServices bundles the entitlement, household and receipt services
// produced from the application configuration.
type monetizationServices struct {
	entitlements *entitlements.Service
	households   *household.Service
	receipts     *receipts.Service
}

// buildMonetizationServices constructs the entitlement, household and receipt
// services (including any configured store-front receipt verifiers) from cfg.
func buildMonetizationServices(cfg config.AppConfig) (monetizationServices, error) {
	entitlementStore, err := entitlements.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		return monetizationServices{}, fmt.Errorf("initialize entitlement store: %w", err)
	}
	entitlementService := entitlements.NewService(entitlementStore)
	householdStore, err := household.NewStore(cfg.Store.Backend, cfg.Store.Path)
	if err != nil {
		return monetizationServices{}, fmt.Errorf("initialize household store: %w", err)
	}
	householdService := household.NewService(householdStore)
	receiptVerifiers := make([]receipts.Verifier, 0, 2)
	normalizedMonetization := cfg.Monetization.Normalized()
	if normalizedMonetization.GooglePlay.PackageName != "" || normalizedMonetization.GooglePlay.ServiceAccountCredentialsFile != "" {
		googleVerifier, err := receiptgoogle.NewVerifier(receiptgoogle.Config{
			PackageName:                   normalizedMonetization.GooglePlay.PackageName,
			ServiceAccountCredentialsFile: normalizedMonetization.GooglePlay.ServiceAccountCredentialsFile,
		})
		if err != nil {
			return monetizationServices{}, fmt.Errorf("initialize google play receipt verifier: %w", err)
		}
		receiptVerifiers = append(receiptVerifiers, googleVerifier)
	}
	if normalizedMonetization.Amazon.SharedSecretFile != "" || normalizedMonetization.Amazon.UseSandbox {
		amazonVerifier, err := receiptamazon.NewVerifier(receiptamazon.Config{
			SharedSecretFile: normalizedMonetization.Amazon.SharedSecretFile,
			UseSandbox:       normalizedMonetization.Amazon.UseSandbox,
		})
		if err != nil {
			return monetizationServices{}, fmt.Errorf("initialize amazon appstore receipt verifier: %w", err)
		}
		receiptVerifiers = append(receiptVerifiers, amazonVerifier)
	}
	receiptService, err := receipts.NewService(normalizedMonetization, entitlementService, receiptVerifiers...)
	if err != nil {
		return monetizationServices{}, fmt.Errorf("initialize receipt service: %w", err)
	}

	return monetizationServices{
		entitlements: entitlementService,
		households:   householdService,
		receipts:     receiptService,
	}, nil
}

// buildEnigmaClients constructs the enigma2 and OpenWebIF clients used to talk
// to the receiver from cfg and the active config snapshot.
func buildEnigmaClients(cfg config.AppConfig, snap config.Snapshot) (*enigma2.Client, *openwebif.Client) {
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
	owiClient := openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, openwebif.Options{
		Timeout:             cfg.Enigma2.Timeout,
		Username:            cfg.Enigma2.Username,
		Password:            cfg.Enigma2.Password,
		UseWebIFStreams:     cfg.Enigma2.UseWebIFStreams,
		StreamBaseURL:       snap.Runtime.OpenWebIF.StreamBaseURL,
		HTTPMaxConnsPerHost: snap.Runtime.OpenWebIF.HTTPMaxConnsPerHost,
	})
	return e2Client, owiClient
}

// newReceiverHealthCheck returns the daemon receiver health-check probe, issuing
// a HEAD request against the enigma2 base URL with the configured credentials.
func newReceiverHealthCheck(cfg config.AppConfig, e2Client *enigma2.Client) func(context.Context) error {
	return func(ctx context.Context) error {
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
	}
}

// registerHealthCheckers wires the connectivity-contract checker and, when
// strict readiness is enabled, the OpenWebIF receiver checker onto hm. It is a
// no-op when hm is nil.
func registerHealthCheckers(hm *health.Manager, cfg config.AppConfig, cfgHolder *config.ConfigHolder, logger zerolog.Logger) error {
	if hm == nil {
		return nil
	}
	hm.SetReadyStrict(cfg.ReadyStrict)
	hm.RegisterChecker(health.NewConnectivityContractChecker("public_connectivity_contract", func() config.AppConfig {
		if cfgHolder == nil {
			return cfg
		}
		return cfgHolder.Get()
	}))
	if cfg.ReadyStrict {
		if strings.TrimSpace(cfg.Enigma2.BaseURL) == "" {
			return fmt.Errorf("strict readiness enabled but OpenWebIF base URL is missing")
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
	return nil
}

func prepareWireBootstrapState(ctx context.Context, version, explicitConfigPath string) (wireBootstrapState, error) {
	xglog.Configure(xglog.Config{
		Level:   "info",
		Service: "xg2g",
		Version: version,
	})
	logger := xglog.WithComponent("bootstrap")

	effectiveConfigPath, explicitMode, err := resolveConfigPath(strings.TrimSpace(explicitConfigPath))
	if err != nil {
		return wireBootstrapState{}, fmt.Errorf("resolve config path: %w", err)
	}

	loader := config.NewLoader(effectiveConfigPath, version)
	cfg, err := loader.Load()
	if err != nil {
		return wireBootstrapState{}, fmt.Errorf("failed to load configuration: %w", err)
	}

	xglog.Configure(xglog.Config{
		Level:   cfg.LogLevel,
		Service: cfg.LogService,
		Version: cfg.Version,
	})
	logger = xglog.WithComponent("bootstrap")

	logLoadedConfigSource(logger, effectiveConfigPath, explicitMode)
	logConfigFingerprint(logger, cfg)

	if cfg.Engine.Enabled && !cfg.ConfigStrict {
		logger.Warn().
			Str("event", "config.strict.disabled").
			Msg("v3 strict validation disabled via XG2G_V3_CONFIG_STRICT override")
	}

	if err := health.PerformStartupChecks(ctx, cfg); err != nil {
		return wireBootstrapState{}, fmt.Errorf("startup checks failed: %w", err)
	}

	serverCfg := config.ParseServerConfigForApp(cfg)
	bindHost := strings.TrimSpace(config.ParseString("XG2G_BIND_INTERFACE", ""))
	if bindHost != "" {
		newListen, bindErr := config.BindListenAddr(serverCfg.ListenAddr, bindHost)
		if bindErr != nil {
			return wireBootstrapState{}, fmt.Errorf("invalid XG2G_BIND_INTERFACE for API listen: %w", bindErr)
		}
		serverCfg.ListenAddr = newListen
	}

	return wireBootstrapState{
		cfg:                 cfg,
		logger:              logger,
		effectiveConfigPath: effectiveConfigPath,
		serverCfg:           serverCfg,
	}, nil
}

func logLoadedConfigSource(logger zerolog.Logger, effectiveConfigPath string, explicitMode bool) {
	if explicitMode {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file").
			Str("path", effectiveConfigPath).
			Msg("loaded configuration from file")
		return
	}
	if effectiveConfigPath != "" {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file(auto)").
			Str("path", effectiveConfigPath).
			Msg("loaded configuration from file")
		return
	}
	logger.Info().
		Str("event", "config.loaded").
		Str("source", "env+defaults").
		Msg("loaded configuration from environment and defaults")
}

func logConfigFingerprint(logger zerolog.Logger, cfg config.AppConfig) {
	canonicalCfg := config.Clone(cfg)
	canonicalCfg.Monetization = canonicalCfg.Monetization.Normalized()

	configBytes, marshalErr := json.Marshal(canonicalCfg) // #nosec G117 -- marshaled only to compute a sha256 fingerprint; bytes (incl. APIToken) are hashed, never logged or persisted
	if marshalErr != nil {
		return
	}
	hash := sha256.Sum256(configBytes)
	logger.Info().
		Str("event", "config.snapshot").
		Str("sha256", fmt.Sprintf("%x", hash)).
		Msg("configuration snapshot fingerprint")
}

func ensureWireTLSCertificates(cfg *config.AppConfig, logger zerolog.Logger) error {
	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		if cfg.TLSCert == "" || cfg.TLSKey == "" {
			return fmt.Errorf("both XG2G_TLS_CERT and XG2G_TLS_KEY must be set together")
		}
		logger.Info().Str("cert", cfg.TLSCert).Str("key", cfg.TLSKey).Msg("using user-provided TLS certificates")
		return nil
	}
	if !cfg.TLSEnabled {
		return nil
	}

	tlsCfg := xgtls.Config{CertPath: cfg.TLSCert, KeyPath: cfg.TLSKey, Logger: logger}
	certPath, keyPath, err := xgtls.EnsureCertificates(tlsCfg)
	if err != nil {
		return fmt.Errorf("failed to ensure TLS certificates: %w", err)
	}
	cfg.TLSCert = certPath
	cfg.TLSKey = keyPath
	return nil
}

func logWireStartup(logger zerolog.Logger, cfg config.AppConfig, version, commit, buildDate, listenAddr string) error {
	logger.Info().
		Str("event", "startup").
		Str("version", version).
		Str("commit", commit).
		Str("build_date", buildDate).
		Str("addr", listenAddr).
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
		return fmt.Errorf("no API tokens configured: set XG2G_API_TOKEN or XG2G_API_TOKENS")
	}
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		logger.Info().Msgf("→ TLS: enabled (cert: %s, key: %s)", cfg.TLSCert, cfg.TLSKey)
	}
	logger.Info().Msgf("→ Data dir: %s", cfg.DataDir)
	return nil
}

func buildWireConfigState(cfg config.AppConfig, version, effectiveConfigPath string) (configMgr *config.Manager, cfgHolder *config.ConfigHolder, snap config.Snapshot, resolved config.AppConfig) {
	configMgrPath := effectiveConfigPath
	if configMgrPath == "" {
		configMgrPath = filepath.Join(cfg.DataDir, "config.yaml")
	}
	configMgr = config.NewManager(configMgrPath)
	// L23: the reload loader must load from the REAL config source, so it is wired with
	// effectiveConfigPath (NOT the fabricated configMgrPath). For an env-only deploy
	// effectiveConfigPath is "", so Reload()'s loader.Load() skips the (non-existent) file and
	// re-derives from defaults+env — previously it was pointed at DataDir/config.yaml, which
	// does not exist, so every SIGHUP/API reload failed. The Save Manager and the holder's
	// save/watch metadata keep configMgrPath.
	//
	// env-only: Env is the source of truth across reloads; an API-saved config file is NOT
	// re-read by design (source consistency — whatever was the source at startup, here Env,
	// stays the source on reload). Do not "fix" this into reading the saved file.
	cfgHolder = config.NewConfigHolder(cfg, config.NewLoader(effectiveConfigPath, version), configMgrPath)

	if current := cfgHolder.Current(); current != nil {
		snap = *current
	} else {
		snap = config.BuildSnapshot(cfg, config.DefaultEnv())
	}
	return configMgr, cfgHolder, snap, snap.App
}

func resolveMetricsAddr(cfg config.AppConfig) string {
	if !cfg.MetricsEnabled {
		return ""
	}
	metricsAddr := strings.TrimSpace(cfg.MetricsAddr)
	if metricsAddr == "" {
		// Bind to 0.0.0.0 only if behind authenticated reverse proxy.
		return "127.0.0.1:9090"
	}
	return metricsAddr
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
		if err := c.initPiconPool(ctx); err != nil {
			c.Logger.Warn().Err(err).Msg("failed to initialize picon pool; background pre-warm disabled")
		}

		if c.scanManager != nil {
			c.scanManager.AttachLifecycle(ctx)
		}

		if err := c.Server.SetRootContext(ctx); err != nil {
			c.Logger.Warn().Err(err).Msg("failed to set root context on API server during container start")
		}

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

func (c *Container) initPiconPool(ctx context.Context) error {
	if c == nil || c.piconPool != nil {
		return nil
	}
	if c.snapshot.App.Enigma2.BaseURL == "" && c.snapshot.App.PiconBase == "" {
		return nil
	}

	pool, err := jobs.NewPiconPoolForConfig(ctx, c.snapshot.App)
	if err != nil {
		return err
	}
	c.piconPool = pool
	if c.Manager != nil {
		c.Manager.RegisterShutdownHook("picon_pool_stop", func(context.Context) error {
			pool.Stop()
			return nil
		})
	}
	if c.App != nil {
		c.App.SetPiconPool(pool)
	}
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
	st, err := jobs.RefreshWithOptions(ctx, c.snapshot, jobs.WithPiconPool(c.piconPool))
	if err != nil {
		c.Logger.Error().Err(err).Msg("initial data refresh failed")
		c.Logger.Warn().Msg("→ Channels will be empty until manual refresh via /api/refresh")
		return
	}

	c.Logger.Info().Msg("initial data refresh completed successfully")
	c.Server.UpdateStatus(*st)

	if c.scanManager != nil {
		if backgroundScanEnabled() {
			c.Logger.Info().Msg("triggering media truth background scan")
			c.scanManager.RunBackground()
			c.scanManager.StartPeriodicRefresh(backgroundScanInterval())
		} else {
			c.Logger.Warn().Msg("Background media truth scan is disabled (XG2G_BACKGROUND_SCAN_ENABLED=false)")
		}
	}
}

func backgroundScanEnabled() bool {
	return config.ParseBool("XG2G_BACKGROUND_SCAN_ENABLED", true)
}

// defaultBackgroundScanInterval is the cadence at which the capability cache is
// re-checked so still-cold channels (never probed, or past next_retry_at) are
// drained. Once every channel is warm, RunBackground self-gates to a no-op, so
// this is cheap in steady state.
const defaultBackgroundScanInterval = time.Hour

// backgroundScanInterval controls the periodic capability-refresh cadence.
// XG2G_BACKGROUND_SCAN_INTERVAL=0 (or negative) disables periodic refresh,
// leaving only the one-shot startup scan.
func backgroundScanInterval() time.Duration {
	return config.ParseDuration("XG2G_BACKGROUND_SCAN_INTERVAL", defaultBackgroundScanInterval)
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
