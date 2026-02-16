// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/recordings"
)

// ConstructorDeps allows composition roots to provide pre-built API dependencies.
// Any nil field falls back to default API-local construction for compatibility.
type ConstructorDeps struct {
	ChannelManager      *channels.Manager
	SeriesManager       *dvr.Manager
	Snapshot            *config.Snapshot
	RecordingPathMapper *recordings.PathMapper
}

type resolvedConstructorDeps struct {
	channelManager *channels.Manager
	seriesManager  *dvr.Manager
	snapshot       config.Snapshot
	pathMapper     *recordings.PathMapper
}

func resolveConstructorDeps(cfg config.AppConfig, deps ConstructorDeps) resolvedConstructorDeps {
	channelManager := deps.ChannelManager
	if channelManager == nil {
		channelManager = channels.NewManager(cfg.DataDir)
		if err := channelManager.Load(); err != nil {
			log.L().Error().Err(err).Msg("failed to load channel states")
		}
	}

	seriesManager := deps.SeriesManager
	if seriesManager == nil {
		seriesManager = dvr.NewManager(cfg.DataDir)
		if err := seriesManager.Load(); err != nil {
			log.L().Error().Err(err).Msg("failed to load series rules")
		}
	}

	snapshot := defaultSnapshot(cfg)
	if deps.Snapshot != nil {
		snapshot = *deps.Snapshot
	}

	pathMapper := deps.RecordingPathMapper
	if pathMapper == nil {
		pathMapper = recordings.NewPathMapper(cfg.RecordingPathMappings)
	}

	return resolvedConstructorDeps{
		channelManager: channelManager,
		seriesManager:  seriesManager,
		snapshot:       snapshot,
		pathMapper:     pathMapper,
	}
}

func defaultSnapshot(cfg config.AppConfig) config.Snapshot {
	env, err := config.ReadOSRuntimeEnv()
	if err != nil {
		log.L().Warn().Err(err).Msg("failed to read runtime environment, using defaults")
		env = config.DefaultEnv()
	}
	return config.BuildSnapshot(cfg, env)
}
