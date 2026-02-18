// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"errors"
	"fmt"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/rs/zerolog"
)

func buildAPIConstructorDeps(cfg config.AppConfig, snap config.Snapshot, logger zerolog.Logger) (api.ConstructorDeps, error) {
	var loadErrs []error

	channelManager := channels.NewManager(cfg.DataDir)
	if err := channelManager.Load(); err != nil {
		logger.Error().Err(err).Msg("failed to load channel states")
		loadErrs = append(loadErrs, fmt.Errorf("load channel states: %w", err))
	}

	seriesManager := dvr.NewManager(cfg.DataDir)
	if err := seriesManager.Load(); err != nil {
		logger.Error().Err(err).Msg("failed to load series rules")
		loadErrs = append(loadErrs, fmt.Errorf("load series rules: %w", err))
	}

	if len(loadErrs) > 0 {
		return api.ConstructorDeps{}, errors.Join(loadErrs...)
	}

	snapshot := snap
	return api.ConstructorDeps{
		ChannelManager:      channelManager,
		SeriesManager:       seriesManager,
		Snapshot:            &snapshot,
		RecordingPathMapper: recordings.NewPathMapper(cfg.RecordingPathMappings),
	}, nil
}
