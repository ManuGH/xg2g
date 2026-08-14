// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

// UnboundInventoryReader reports the ADR-032 Phase 0 census.
//
// Deliberately a separate optional interface rather than another member of
// StateStore: adding a method to StateStore would break every implementation
// and test double in the tree for a read-only diagnostic. Callers type-assert.
type UnboundInventoryReader interface {
	CountUnboundDeviceAuth(ctx context.Context, now time.Time) (model.UnboundInventory, error)
}

var (
	_ UnboundInventoryReader = (*SqliteStore)(nil)
	_ UnboundInventoryReader = (*MemoryStateStore)(nil)
)

// CountUnboundDeviceAuth counts what a binding cutoff would affect.
//
// Read-only and additive: it introduces no column, no migration and no write
// path, so it can ship before any part of the convergence.
func (s *SqliteStore) CountUnboundDeviceAuth(ctx context.Context, now time.Time) (model.UnboundInventory, error) {
	nowMs := now.UnixMilli()
	var inventory model.UnboundInventory

	scalars := []struct {
		query string
		args  []any
		dest  *int
	}{
		{`SELECT COUNT(*) FROM devices WHERE revoked_at_ms IS NULL`, nil, &inventory.Devices},
		{`SELECT COUNT(DISTINCT owner_id) FROM devices WHERE revoked_at_ms IS NULL`, nil, &inventory.Owners},
		{
			`SELECT COUNT(*) FROM device_grants WHERE revoked_at_ms IS NULL AND expires_at_ms > ?`,
			[]any{nowMs},
			&inventory.ActiveGrants,
		},
		{
			`SELECT COUNT(*) FROM access_sessions WHERE revoked_at_ms IS NULL AND expires_at_ms > ?`,
			[]any{nowMs},
			&inventory.ActiveSessions,
		},
	}

	for _, scalar := range scalars {
		if err := s.DB.QueryRowContext(ctx, scalar.query, scalar.args...).Scan(scalar.dest); err != nil {
			return model.UnboundInventory{}, fmt.Errorf("device auth inventory: %w", err)
		}
	}

	// MIN/MAX over an empty or all-NULL set yields NULL, hence the nullable
	// scan rather than a plain int64.
	timestamps := []struct {
		query string
		dest  *int64
	}{
		{
			`SELECT MIN(issued_at_ms) FROM device_grants WHERE revoked_at_ms IS NULL AND expires_at_ms > ?`,
			&inventory.OldestGrantIssuedAtMs,
		},
		{
			`SELECT MAX(last_used_at_ms) FROM device_grants WHERE revoked_at_ms IS NULL AND expires_at_ms > ?`,
			&inventory.NewestGrantLastUsedAtMs,
		},
	}

	for _, ts := range timestamps {
		var value sql.NullInt64
		if err := s.DB.QueryRowContext(ctx, ts.query, nowMs).Scan(&value); err != nil {
			return model.UnboundInventory{}, fmt.Errorf("device auth inventory: %w", err)
		}
		if value.Valid {
			*ts.dest = value.Int64
		}
	}

	return inventory, nil
}
