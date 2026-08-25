// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package normalizer

import (
	"errors"
	"time"
)

var (
	ErrInvalidConfig         = errors.New("invalid stream normalizer configuration")
	ErrStagingBufferOverflow = errors.New("staging buffer overflow: egress/sink is stalled")
	ErrNormalizerClosed      = errors.New("stream normalizer closed")
	ErrInvalidPacketSize     = errors.New("data slice is not 188-byte packet aligned")
)

const (
	TSPacketSize = 188
	SyncByte     = 0x47
)

// Config configures the Closed-Loop Stream Normalizer.
type Config struct {
	// StartupReservoirMs: Milliseconds of media data required before egress release (default 650.0 ms).
	StartupReservoirMs float64

	// TargetWatermarkMs: Closed-loop target equilibrium buffer depth (default 650.0 ms).
	TargetWatermarkMs float64

	// DeadbandMs: Deadband around target where correction factor is 1.0 (default 75.0 ms).
	DeadbandMs float64

	// MaxCorrectionTrim: Maximum proportional correction trim (default 0.02 = ±2%).
	MaxCorrectionTrim float64

	// Kp: Proportional gain for watermark error correction (default 0.04).
	Kp float64

	// PacerIntervalMs: Nominal egress pacing slice interval (default 20.0 ms).
	PacerIntervalMs float64

	// StagingBufferCapacity: Circular staging buffer byte size (default 4 MiB).
	StagingBufferCapacity int

	// InitialBitrateKbps: Initial fallback rate estimate before first valid PCR pair (default 4500.0 kbps).
	InitialBitrateKbps float64
}

// DefaultConfig returns the authoritative baseline configuration.
func DefaultConfig() Config {
	return Config{
		StartupReservoirMs:    650.0,
		TargetWatermarkMs:     650.0,
		DeadbandMs:            75.0,
		MaxCorrectionTrim:     0.02,
		Kp:                    0.04,
		PacerIntervalMs:       20.0,
		StagingBufferCapacity: 4 * 1024 * 1024, // 4 MiB (~6.5s of 4.8 Mbps)
		InitialBitrateKbps:    4500.0,
	}
}

// Validate ensures all configuration fields are within sound operational boundaries.
func (c Config) Validate() error {
	if c.StartupReservoirMs < 0 || c.StartupReservoirMs > 10000 {
		return errors.New("StartupReservoirMs must be between 0 and 10000 ms")
	}
	if c.TargetWatermarkMs <= 0 || c.TargetWatermarkMs > 10000 {
		return errors.New("TargetWatermarkMs must be between 1 and 10000 ms")
	}
	if c.DeadbandMs < 0 || c.DeadbandMs >= c.TargetWatermarkMs {
		return errors.New("DeadbandMs must be >= 0 and < TargetWatermarkMs")
	}
	if c.MaxCorrectionTrim < 0 || c.MaxCorrectionTrim > 0.50 {
		return errors.New("MaxCorrectionTrim must be between 0 and 0.50")
	}
	if c.Kp < 0 || c.Kp > 1.0 {
		return errors.New("kp must be between 0 and 1.0")
	}
	if c.PacerIntervalMs < 1.0 || c.PacerIntervalMs > 1000.0 {
		return errors.New("PacerIntervalMs must be between 1 and 1000 ms")
	}
	if c.StagingBufferCapacity < TSPacketSize*100 {
		return errors.New("StagingBufferCapacity must be at least 100 TS packets")
	}
	if c.InitialBitrateKbps < 100.0 || c.InitialBitrateKbps > 500000.0 {
		return errors.New("InitialBitrateKbps must be between 100 and 500000 kbps")
	}
	return nil
}

// PacerDuration returns the pacer interval as a time.Duration.
func (c Config) PacerDuration() time.Duration {
	return time.Duration(c.PacerIntervalMs * float64(time.Millisecond))
}
