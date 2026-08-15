// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bootstrap

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

// logUnboundDeviceAuthCensus reports how many devices a binding cutoff would
// affect. See ADR-032 Phase 0.
//
// Read-only: no schema change, no write path, no behaviour change. It exists so
// the cutoff window is chosen from measured numbers rather than from a guess,
// and so the legacy fleet cannot quietly persist by being invisible.
//
// A non-empty fleet logs at warn level on purpose. It is an actionable
// condition with a deadline attached, not background information.
func logUnboundDeviceAuthCensus(ctx context.Context, store deviceauthstore.StateStore, logger zerolog.Logger, now time.Time) {
	reader, ok := store.(deviceauthstore.UnboundInventoryReader)
	if !ok {
		// A store backend without the census is not a failure; it simply cannot
		// answer, and saying so beats silence.
		logger.Debug().
			Str("adr", "032").
			Msg("device auth census unavailable for this store backend")
		return
	}

	inventory, err := reader.CountUnboundDeviceAuth(ctx, now)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("adr", "032").
			Msg("device auth census failed")
		return
	}

	event := logger.Info()
	if !inventory.IsEmpty() {
		event = logger.Warn()
	}

	event = event.
		Str("adr", "032").
		Str("binding", "legacy_unbound").
		Int("devices", inventory.Devices).
		Int("owners", inventory.Owners).
		Int("active_grants", inventory.ActiveGrants).
		Int("active_sessions", inventory.ActiveSessions)

	if inventory.OldestGrantIssuedAtMs > 0 {
		event = event.Time("oldest_grant_issued_at", time.UnixMilli(inventory.OldestGrantIssuedAtMs).UTC())
	}
	if inventory.NewestGrantLastUsedAtMs > 0 {
		event = event.Time("newest_grant_last_used_at", time.UnixMilli(inventory.NewestGrantLastUsedAtMs).UTC())
	} else if inventory.ActiveGrants > 0 {
		// "Never used" is a materially different cutoff decision from "used
		// recently", so it must not read as a missing field.
		event = event.Bool("any_grant_ever_used", false)
	}

	if inventory.IsEmpty() {
		event.Msg("device auth census: no cryptographically unbound devices")
		return
	}
	event.Msg("device auth census: cryptographically unbound devices present; these must re-pair before the ADR-032 cutoff")
}
