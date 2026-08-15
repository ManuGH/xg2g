// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
	"github.com/ManuGH/xg2g/internal/health"
)

// deviceAuthCensusFor supplies the ADR-032 Phase 0 census to the runtime
// snapshot, reading the same store the daemon uses.
//
// Returns nil for a memory backend or an unset store path, so the snapshot
// omits the section entirely rather than reporting a fabricated zero.
func deviceAuthCensusFor(cfg config.AppConfig) health.UnboundDeviceAuthCensusFunc {
	storePath := strings.TrimSpace(cfg.Store.Path)
	if storePath == "" || strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") {
		return nil
	}

	dbPath := filepath.Join(storePath, "deviceauth.sqlite")

	return func(ctx context.Context) (deviceauthmodel.UnboundInventory, error) {
		// Opened without migrations: a preflight report must not upgrade the
		// schema of a live installation as a side effect.
		reader, closeReader, err := deviceauthstore.OpenUnboundInventoryReader(dbPath)
		if err != nil {
			return deviceauthmodel.UnboundInventory{}, err
		}
		defer func() { _ = closeReader() }()

		inventory, err := reader.CountUnboundDeviceAuth(ctx, time.Now().UTC())
		if err != nil {
			return deviceauthmodel.UnboundInventory{}, fmt.Errorf("device auth census: %w", err)
		}
		return inventory, nil
	}
}
