package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	xgtls "github.com/ManuGH/xg2g/internal/tls"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// Container holds the wired service graph.
// It represents the "Single Source of Truth" for the runtime application structure.
type Container struct {
	Config        config.AppConfig
	ConfigManager *config.Manager
	Logger        zerolog.Logger
	Server        *api.Server
	Daemon        daemon.Manager
	App           *daemon.App

	// Dependencies enabling test observation
	Deps daemon.Deps
}

// Global state for logger - unavoidable due to zerolog global configuration usage in current codebase
var loggerOnce sync.Once

// WireServices bootstraps the entire application from a config file (or auto-discovery).
// This replaces the ad-hoc wiring in cmd/daemon/main.go.
func WireServices(ctx context.Context, version, commit, buildDate string, explicitConfigPath string) (*Container, error) {
	// 1. Initial Logger Setup (Safe defaults)
	// We configure this globally once.
	loggerOnce.Do(func() {
		log.Configure(log.Config{
			Level:   "info",
			Service: "xg2g",
			Version: version,
		})
	})
	logger := log.WithComponent("bootstrap")

	// 2. Config Resolution & Loading
	effectiveConfigPath := resolveConfigPath(logger, explicitConfigPath)
	loader := config.NewLoader(effectiveConfigPath, version)
	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// 3. Re-configure Logger with loaded config
	log.Configure(log.Config{
		Level:   cfg.LogLevel,
		Service: cfg.LogService,
		Version: cfg.Version,
	})

	// 4. Pre-flight Checks (Fail Fast)
	if err := health.PerformStartupChecks(ctx, cfg); err != nil {
		return nil, fmt.Errorf("startup checks failed: %w", err)
	}

	// 5. Config Manager & Config Holder (Hot Reload)
	configMgrPath := effectiveConfigPath
	if configMgrPath == "" {
		configMgrPath = filepath.Join(cfg.DataDir, "config.yaml")
	}
	configMgr := config.NewManager(configMgrPath)
	cfgHolder := config.NewConfigHolder(cfg, config.NewLoader(configMgrPath, version), configMgrPath)

	// 6. Server Config & TLS
	serverCfg := config.ParseServerConfig()
	// Priority: ENV > YAML > Default
	if strings.TrimSpace(config.ParseString("XG2G_LISTEN", "")) == "" {
		if strings.TrimSpace(cfg.APIListenAddr) != "" {
			serverCfg.ListenAddr = cfg.APIListenAddr
		}
	}
	// Bind Interface Override
	bindHost := strings.TrimSpace(config.ParseString("XG2G_BIND_INTERFACE", ""))
	if bindHost != "" {
		if newListen, err := config.BindListenAddr(serverCfg.ListenAddr, bindHost); err != nil {
			return nil, fmt.Errorf("invalid XG2G_BIND_INTERFACE: %w", err)
		} else {
			serverCfg.ListenAddr = newListen
		}
	}
	// TLS Auto-Generation
	if cfg.TLSEnabled {
		if cfg.TLSCert == "" && cfg.TLSKey == "" {
			tlsCfg := xgtls.Config{
				Logger: logger,
			}
			certPath, keyPath, err := xgtls.EnsureCertificates(tlsCfg)
			if err != nil {
				return nil, fmt.Errorf("failed to generate TLS certs: %w", err)
			}
			cfg.TLSCert = certPath
			cfg.TLSKey = keyPath
		}
	}

	// 7. Initialize API Server (The "Big Ball of Mud" Factory - wrapped here)
	s := api.New(cfg, configMgr)
	s.SetRootContext(ctx)
	s.SetConfigHolder(cfgHolder)
	// Apply initial snapshot
	if current := cfgHolder.Current(); current != nil {
		s.ApplySnapshot(current)
	} else {
		env := config.DefaultEnv()
		snap := config.BuildSnapshot(cfg, env)
		s.ApplySnapshot(&snap)
	}

	// 8. Background Workers (e.g. Cache Eviction)
	// Side-effect removed from constructor. Moved to Start()

	// 9. Initial Refresh (Background)
	// Side-effect removed from constructor. Moved to Start()

	// 10. Daemon Manager & App
	metricsAddr := ":9090"
	if cfg.MetricsEnabled && cfg.MetricsAddr != "" {
		metricsAddr = cfg.MetricsAddr
	}

	deps := daemon.Deps{
		Logger:          log.WithComponent("daemon"),
		Config:          cfg,
		ConfigManager:   configMgr,
		APIHandler:      s.Handler(),
		APIServerSetter: s,
		MetricsHandler:  promhttp.Handler(),
		MetricsAddr:     metricsAddr,
	}

	mgr, err := daemon.NewManager(serverCfg, deps)
	if err != nil {
		return nil, fmt.Errorf("failed to create daemon manager: %w", err)
	}

	// Configure Readiness Checks (Health Manager)
	if cfg.ReadyStrict {
		hm := s.HealthManager()
		hm.SetReadyStrict(true)
		if cfg.Enigma2.BaseURL == "" {
			return nil, fmt.Errorf("strict readiness enabled but OpenWebIF URL missing")
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
	}

	app := daemon.NewApp(log.WithComponent("app"), mgr, cfgHolder, s, false)

	return &Container{
		Config:        cfg,
		ConfigManager: configMgr,
		Logger:        logger,
		Server:        s,
		Daemon:        mgr,
		App:           app,
		Deps:          deps,
	}, nil
}

// Start launches background processes and the application logic.
// This is distinct from WireServices to ensure the constructor is side-effect free.
func (c *Container) Start(ctx context.Context) error {
	// 1. Start Cache Evicter
	go c.Server.StartRecordingCacheEvicter(ctx)

	// 2. Initial Refresh (if enabled)
	if config.ParseBool("XG2G_INITIAL_REFRESH", true) {
		go func() {
			// Create a synthetic request for the background refresh
			// This avoids panics in handleRefresh which expects non-nil w/r
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/internal/background/refresh", nil)
			req.RemoteAddr = "internal"
			// Use a discard writer
			c.Server.HandleRefreshInternal(&bgWriter{}, req)
		}()
	}

	// 3. Run the Daemon App (Blocks)
	// Note: Usually Start() should be non-blocking or we should have Run().
	// But daemon.App.Run() is blocking.
	// For "Boot", we might want non-blocking Start?
	// The user interface for main.go expects blocking.
	// Let's keep App.Run() as the blocking call, but Start() handles background wires.
	// Actually, main.go called app.Run().
	// We should probably just expose StartBackground() or assume the caller calls App.Run()
	// and we attach our background stuff there?
	// But `StartRecordingCacheEvicter` needs to happen.
	// Let's make Start() non-blocking for background tasks, and let caller invoke App.Run() for the main loop.
	return nil
}

type bgWriter struct{}

func (w *bgWriter) Header() http.Header         { return make(http.Header) }
func (w *bgWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *bgWriter) WriteHeader(statusCode int)  {}

// resolveConfigPath mimics main.go logic
func resolveConfigPath(logger zerolog.Logger, explicit string) string {
	if explicit != "" {
		abs, _ := filepath.Abs(explicit)
		return abs
	}
	// Fallback logic
	dataDir := config.ParseString("XG2G_DATA", "/tmp")
	autoPath := filepath.Join(dataDir, "config.yaml")
	if info, err := os.Stat(autoPath); err == nil && !info.IsDir() {
		abs, _ := filepath.Abs(autoPath)
		return abs
	}
	return ""
}
