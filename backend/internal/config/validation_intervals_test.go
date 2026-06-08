// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These guard the two interval fields that flow into time.NewTicker
// (Verification worker, lease-expiry worker), which panics on a non-positive
// duration. Validation must reject the bad value instead of crashing at startup.

func TestValidate_VerificationIntervalMustBePositiveWhenEnabled(t *testing.T) {
	for _, d := range []time.Duration{0, -1 * time.Minute} {
		cfg := baseV3Config(t)
		cfg.Verification.Enabled = true
		cfg.Verification.Interval = d

		err := Validate(cfg)
		require.Error(t, err, "interval %s must be rejected", d)
		require.Contains(t, err.Error(), "Verification.Interval")
	}
}

func TestValidate_VerificationIntervalIgnoredWhenDisabled(t *testing.T) {
	// Disabled verification never starts the worker, so a zero interval is fine.
	cfg := baseV3Config(t)
	cfg.Verification.Enabled = false
	cfg.Verification.Interval = 0

	require.NoError(t, Validate(cfg))
}

func TestValidate_VerificationIntervalPositiveOK(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.Verification.Enabled = true
	cfg.Verification.Interval = 60 * time.Second

	require.NoError(t, Validate(cfg))
}

func TestValidate_SessionExpiryIntervalRejectsNegative(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.Sessions.ExpiryCheckInterval = -1 * time.Second

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Sessions.ExpiryCheckInterval")
}

func TestValidate_SessionExpiryIntervalZeroAllowed(t *testing.T) {
	// 0 is the documented "use default" sentinel honored by the lease worker.
	cfg := baseV3Config(t)
	cfg.Sessions.ExpiryCheckInterval = 0

	require.NoError(t, Validate(cfg))
}
