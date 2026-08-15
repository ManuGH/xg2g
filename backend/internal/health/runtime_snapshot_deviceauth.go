// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package health

import (
	"context"

	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

// UnboundDeviceAuthCensusFunc supplies the ADR-032 Phase 0 census.
//
// A function rather than a store handle so `health` stays free of the
// deviceauth store, while still returning the *same* value type the store
// produces. Startup log and diagnostics are two views of one census; there is
// deliberately no second counting implementation to drift out of step.
type UnboundDeviceAuthCensusFunc func(ctx context.Context) (deviceauthmodel.UnboundInventory, error)

// LifecycleRuntimeDeviceAuthSnapshot reports how many devices still hold
// cryptographically unbound credentials.
type LifecycleRuntimeDeviceAuthSnapshot struct {
	// Always "legacy_unbound" while the deviceauth store has no binding column.
	Binding   string                           `json:"binding"`
	Inventory deviceauthmodel.UnboundInventory `json:"inventory"`
	// Set when the census could not be taken, so an absent count is never
	// mistaken for a count of zero.
	Error string `json:"error,omitempty"`
}

func collectDeviceAuthSnapshot(ctx context.Context, census UnboundDeviceAuthCensusFunc) *LifecycleRuntimeDeviceAuthSnapshot {
	if census == nil {
		return nil
	}

	inventory, err := census(ctx)
	if err != nil {
		return &LifecycleRuntimeDeviceAuthSnapshot{
			Binding: "legacy_unbound",
			Error:   err.Error(),
		}
	}

	return &LifecycleRuntimeDeviceAuthSnapshot{
		Binding:   "legacy_unbound",
		Inventory: inventory,
	}
}
