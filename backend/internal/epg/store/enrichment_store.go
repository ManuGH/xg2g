// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
)

// Default TTL configurations for enrichment records.
const (
	DefaultMatchTTL   = 30 * 24 * time.Hour // 30 days for confirmed provider matches
	DefaultNoMatchTTL = 24 * time.Hour      // 24 hours for deterministic negative matches
)

// EnrichmentStore is the storage interface for persisting and retrieving EPG enrichment data.
// Invariant: Expired rows or entries with matcher_version mismatch are treated as misses.
type EnrichmentStore interface {
	// Get retrieves enrichment data for the given fingerprint.
	// Returns (data, true, nil) on a valid, unexpired hit with matching matcher_version.
	// Returns (nil, false, nil) on a miss, expired entry, or matcher_version mismatch.
	Get(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, bool, error)

	// Put stores an enrichment record with an expiration timestamp.
	Put(ctx context.Context, data *epg.EnrichmentData) error

	// PruneExpired removes all records whose ExpiresAt is before the specified time.
	PruneExpired(ctx context.Context, now time.Time) (int64, error)

	// Close releases any database handles or background timers.
	Close() error
}
