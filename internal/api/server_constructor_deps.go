// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"errors"
	"fmt"

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

func resolveConstructorDeps(cfg config.AppConfig, deps ConstructorDeps) (resolvedConstructorDeps, error) {
	var loadErrs []error

	channelManager := deps.ChannelManager
	if channelManager == nil {
		channelManager = channels.NewManager(cfg.DataDir)
		if err := channelManager.Load(); err != nil {
			loadErrs = append(loadErrs, fmt.Errorf("load channel states: %w", err))
		}
	}

	seriesManager := deps.SeriesManager
	if seriesManager == nil {
		seriesManager = dvr.NewManager(cfg.DataDir)
		if err := seriesManager.Load(); err != nil {
			loadErrs = append(loadErrs, fmt.Errorf("load series rules: %w", err))
		}
	}
	if len(loadErrs) > 0 {
		return resolvedConstructorDeps{}, errors.Join(loadErrs...)
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
	}, nil
}

func defaultSnapshot(cfg config.AppConfig) config.Snapshot {
	env, err := config.ReadOSRuntimeEnv()
	if err != nil {
		log.L().Warn().Err(err).Msg("failed to read runtime environment, using defaults")
		env = config.DefaultEnv()
	}
	return config.BuildSnapshot(cfg, env)
}
