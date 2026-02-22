// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/platform/httpx"
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
	s.preflightProvider = preflight.NewHTTPPreflightProvider(nil, cfg.Enigma2.PreflightTimeout)
	return nil
}

func (s *Server) wireV3Subsystem(cfg config.AppConfig, cfgMgr *config.Manager) error {
	s.v3Handler = s.v3Factory(cfg, cfgMgr, s.rootCancel)
	// Ensure runtime values are visible before the first request.
	s.v3Handler.UpdateConfig(cfg, s.snap)

	var resolverOpts recservice.ResolverOptions
	if libSvc := s.v3Handler.LibraryService(); libSvc != nil {
		resolverOpts.DurationStore = recservice.NewLibraryDurationStore(libSvc.GetStore())
		resolverOpts.PathResolver = recservice.NewLibraryPathResolver(s.recordingPathMapper, libSvc.GetConfigs())
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

	recSvc, err := recservice.NewService(&cfg, s.vodManager, v4Resolver, owiAdapter, resumeAdapter, v4Resolver)
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
	return openwebif.New(cfg.Enigma2.BaseURL)
}

func (s *Server) newHealthManager(cfg config.AppConfig) *health.Manager {
	return health.NewManager(cfg.Version)
}

func (s *Server) initHDHR(cfg config.AppConfig, cm *channels.Manager) {
	logger := log.WithComponent("api")
	hdhrEnabled := false
	if cfg.HDHR.Enabled != nil {
		hdhrEnabled = *cfg.HDHR.Enabled
	}
	if !hdhrEnabled {
		return
	}

	tunerCount := 4
	if cfg.HDHR.TunerCount != nil {
		tunerCount = *cfg.HDHR.TunerCount
	}
	plexForceHLS := false
	if cfg.HDHR.PlexForceHLS != nil {
		plexForceHLS = *cfg.HDHR.PlexForceHLS
	}

	hdhrConf := hdhr.Config{
		Enabled:          hdhrEnabled,
		DeviceID:         cfg.HDHR.DeviceID,
		FriendlyName:     cfg.HDHR.FriendlyName,
		ModelName:        cfg.HDHR.ModelNumber,
		FirmwareName:     cfg.HDHR.FirmwareName,
		BaseURL:          cfg.HDHR.BaseURL,
		TunerCount:       tunerCount,
		PlexForceHLS:     plexForceHLS,
		PlaylistFilename: s.snap.Runtime.PlaylistFilename,
		DataDir:          cfg.DataDir,
		Logger:           logger,
	}

	s.hdhr = hdhr.NewServer(hdhrConf, cm)
	logger.Info().
		Bool("hdhr_enabled", true).
		Str("device_id", hdhrConf.DeviceID).
		Msg("HDHomeRun emulation enabled")
}

func (s *Server) registerHealthCheckers(cfg config.AppConfig) {
	receiverProbeClient := httpx.NewClient(5 * time.Second)

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
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, cfg.Enigma2.BaseURL, nil)
		if err != nil {
			return err
		}
		resp, err := receiverProbeClient.Do(req)
		if err != nil {
			return err
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("receiver returned HTTP %d", resp.StatusCode)
		}
		return nil
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
