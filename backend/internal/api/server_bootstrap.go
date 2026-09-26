// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/health"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/recordings"
)

func (s *Server) initPlaybackSubsystem(cfg config.AppConfig) error {
	vodMgr, err := vod.NewManager(
		infra.NewExecutor(cfg.FFmpeg.Bin, *log.L(), cfg.Timeouts.TranscodeStart, cfg.Timeouts.TranscodeNoProgress),
		infra.NewProber(cfg.FFmpeg.FFprobeBin),
		recordings.NewPathMapper(cfg.RecordingPathMappings),
	)
	if err != nil {
		return fmt.Errorf("initialize vod manager: %w", err)
	}
	s.vodManager = vodMgr
	s.preflightProvider = preflight.NewHTTPPreflightProvider(nil, cfg.Enigma2.PreflightTimeout, preflightOutboundPolicyFromConfig(cfg))
	return nil
}

func preflightOutboundPolicyFromConfig(cfg config.AppConfig) platformnet.OutboundPolicy {
	allow := cfg.Network.Outbound.Allow
	return platformnet.OutboundPolicy{
		Enabled: cfg.Network.Outbound.Enabled,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   append([]string(nil), allow.Hosts...),
			CIDRs:   append([]string(nil), allow.CIDRs...),
			Ports:   append([]int(nil), allow.Ports...),
			Schemes: append([]string(nil), allow.Schemes...),
		},
	}
}

func (s *Server) wireV3Subsystem(cfg config.AppConfig, cfgMgr *config.Manager) error {
	s.v3Handler = s.v3Factory(cfg, cfgMgr, s.rootCancel)
	if s.rootCtx != nil {
		if err := s.v3Handler.SetRuntimeContext(s.rootCtx); err != nil {
			return fmt.Errorf("set v3 runtime context: %w", err)
		}
	}
	// Ensure runtime values are visible before the first request.
	s.v3Handler.UpdateConfig(cfg, s.snap)

	// [SECURITY] Wire decision secret — hard fail if missing.
	// Without a secret, live-stream JWT tokens cannot be signed or verified.
	secret := config.DecisionSecretFromEnv()
	if secret == nil {
		return fmt.Errorf("XG2G_DECISION_SECRET is required but not set (live stream security prerequisite)")
	}
	s.v3Handler.SetJWTSecret(secret)

	var resolverOpts recservice.ResolverOptions
	var persistor recservice.DurationPersistor

	resolverOpts.ProbeRootContext = s.rootCtx
	resolverOpts.ProbeFn = func(ctx context.Context, serviceRef, sourceURL string) (*vod.StreamInfo, error) {
		// SSRF Guard: sourceURL is already derived from validated IDs in truthProvider.
		// We call the synchronous Probe method here.
		return s.vodManager.Probe(ctx, sourceURL)
	}

	if libSvc := s.v3Handler.LibraryService(); libSvc != nil {
		ds := recservice.NewLibraryDurationStore(libSvc.GetStore())
		pr := recservice.NewLibraryPathResolver(s.recordingPathMapper, libSvc.GetConfigs())
		resolverOpts.DurationStore = ds
		resolverOpts.PathResolver = pr

		// Option A Orchestrator Boundary: Persist dynamic truth async.
		persistor = durationPersistorAdapter{store: ds, pr: pr}
		resolverOpts.DurationPersistor = persistor
	}
	v4Resolver, err := recservice.NewResolver(&cfg, s.vodManager, resolverOpts)
	if err != nil {
		return fmt.Errorf("initialize recordings resolver: %w", err)
	}

	s.owiClient = openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
		Timeout:  cfg.Enigma2.Timeout,
		Username: cfg.Enigma2.Username,
		Password: cfg.Enigma2.Password,
	})

	owiAdapter := v3.NewOWIAdapter(s.owiClient)
	resumeAdapter := v3.NewResumeAdapter(s.v3RuntimeDeps.ResumeStore)

	recSvc, err := recservice.NewService(&cfg, s.vodManager, v4Resolver, owiAdapter, resumeAdapter)
	if err != nil {
		return fmt.Errorf("initialize recordings service: %w", err)
	}
	s.WireV3Overrides(V3Overrides{
		Resolver:          v4Resolver,
		RecordingsService: recSvc,
	})

	return nil
}

func (s *Server) newSeriesOWIClient(cfg config.AppConfig) dvr.OWIClient {
	timeout := cfg.Enigma2.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
		Timeout:  timeout,
		Username: cfg.Enigma2.Username,
		Password: cfg.Enigma2.Password,
	})
}

func (s *Server) newHealthManager(cfg config.AppConfig) *health.Manager {
	return health.NewManager(cfg.Version)
}

// warnRemovedHDHR tells an operator who still enables the HDHomeRun emulation
// that it is gone. Its HTTP endpoints were removed with the legacy surface in
// #210, and announcing a device whose URLs all answer 404 only sends clients to
// dead ends, so nothing is started any more. Startup-only, not hot-path.
func warnRemovedHDHR(cfg config.AppConfig) {
	if msg, warn := removedHDHRWarning(cfg.HDHR.Enabled); warn {
		logger := log.WithComponent("api")
		logger.Warn().Bool("hdhr_enabled", true).Msg(msg)
	}
}

// removedHDHRWarning reports whether the configuration still asks for the
// removed HDHomeRun emulation. Only an explicit hdhr.enabled: true is worth a
// warning; an absent or disabled switch asks for nothing.
func removedHDHRWarning(enabled *bool) (string, bool) {
	if enabled == nil || !*enabled {
		return "", false
	}
	return "hdhr.enabled is set but has no effect: the HDHomeRun emulation was removed (deprecated, inert); nothing is announced on the network", true
}

func (s *Server) registerHealthCheckers(cfg config.AppConfig) {
	s.healthManager.RegisterChecker(health.NewExistingWritableDirChecker("data_dir", cfg.DataDir))
	if !strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") && strings.TrimSpace(cfg.Store.Path) != "" {
		s.healthManager.RegisterChecker(health.NewExistingWritableDirChecker("store_path", cfg.Store.Path))
	}
	if cfg.Engine.Enabled {
		s.healthManager.RegisterChecker(health.NewExistingWritableDirChecker("hls_root", cfg.HLS.Root))
	}
	for id, path := range cfg.RecordingRoots {
		s.healthManager.RegisterChecker(health.NewExistingWritableDirChecker("recording_root:"+id, path))
	}

	playlistName := s.snap.Runtime.PlaylistFilename
	playlistPath := filepath.Join(cfg.DataDir, playlistName)
	s.healthManager.RegisterChecker(health.NewFileChecker("playlist", playlistPath))

	if strings.TrimSpace(cfg.XMLTVPath) != "" {
		xmltvPath := filepath.Join(cfg.DataDir, cfg.XMLTVPath)
		s.healthManager.RegisterChecker(health.NewFileChecker("xmltv", xmltvPath))
	}

	s.healthManager.RegisterChecker(health.NewLastRunChecker(func() (time.Time, string) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.status.LastRun, s.status.Error
	}))

	s.healthManager.RegisterChecker(health.NewReceiverChecker(func(ctx context.Context) error {
		if cfg.Enigma2.BaseURL == "" {
			return fmt.Errorf("receiver not configured")
		}

		timeout := cfg.Enigma2.Timeout
		if timeout <= 0 {
			timeout = 2 * time.Second
		}

		client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, 0, openwebif.Options{
			Timeout:  timeout,
			Username: cfg.Enigma2.Username,
			Password: cfg.Enigma2.Password,
		})
		_, err := client.About(ctx)
		return err
	}))

	s.healthManager.RegisterChecker(health.NewChannelsChecker(func() int {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.status.Channels
	}))

	s.healthManager.RegisterChecker(health.NewEPGChecker(func() (bool, time.Time) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.status.EPGProgrammes > 0, s.status.LastRun
	}))
}

type durationPersistorAdapter struct {
	store recservice.DurationStore
	pr    recservice.PathResolver
}

func (d durationPersistorAdapter) PersistDuration(ctx context.Context, serviceRef string, duration int64) error {
	_, rootID, relPath, err := d.pr.ResolveRecordingPath(serviceRef)
	if err != nil || rootID == "" || relPath == "" {
		return nil
	}
	return d.store.SetDuration(ctx, rootID, relPath, duration)
}
