// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"context"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/net"
)

// Tuner implements exec.Tuner using Enigma2 API.
type Tuner struct {
	Client       *Client
	Checker      *ReadyChecker
	Slot         int
	Timeout      time.Duration
	PollInterval time.Duration
}

// NewTuner returns a new Enigma2 tuner instance.
func NewTuner(client *Client, slot int, timeout time.Duration) *Tuner {
	return &Tuner{
		Client:       client,
		Checker:      NewReadyChecker(client),
		Slot:         slot,
		Timeout:      timeout,
		PollInterval: 500 * time.Millisecond,
	}
}

// Tune zaps to the service and waits for a lock using ReadyChecker.
func (t *Tuner) Tune(ctx context.Context, serviceRef string) error {
	// Enforce timeout for the tuning operation
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	logger := log.L().With().Int("slot", t.Slot).Str("ref", serviceRef).Logger()
	logger.Debug().Msg("initiating zap")

	// CRITICAL FIX: Always force Zap, even if useWebIFStreams is true.
	// Optional middleware (Port 17999) may REQUIRE the receiver to be tuned to the service for proper processing.
	// Relying on "implicit tuning via stream URL" works for 8001 but may fail for 17999.
	// The overhead of an extra Zap is negligible compared to the stability gain.
	// if t.Client != nil && t.Client.useWebIFStreams {
	// 	logger.Info().Msg("skipping zap for WebIF stream")
	// 	return nil
	// }

	// Skip Zap if UseWebIFStreams is enabled.
	// Rationale: calling /web/stream.m3u implies that OpenWebIF handles zapping and port selection internally.
	// Manual Zap from xg2g interferes with this logic or causes unnecessary main-tuner switches.
	if t.Client != nil && t.Client.UseWebIFStreams {
		logger.Info().Msg("skipping explicit zap for WebIF stream (OpenWebIF manages zap/port)")
		return nil
	}

	// Skip Zap if StreamPort is configured (direct port access like 8001).
	// Port 8001 provides direct streams without requiring tuner zap.
	// This allows parallel usage: HDMI-TV stays on current channel, xg2g streams independently.
	if t.Client != nil && t.Client.StreamPort > 0 {
		logger.Info().Int("streamPort", t.Client.StreamPort).Msg("skipping zap for direct port access")
		return nil
	}

	if err := t.Client.Zap(ctx, serviceRef); err != nil {
		return fmt.Errorf("zap failed: %w", err)
	}

	// Bypass readiness check for HTTP streams (recordings/IPTV) as they don't produce tuner lock
	if _, ok := net.ParseDirectHTTPURL(serviceRef); ok {
		logger.Info().Msg("skipping tuner readiness check for HTTP stream")
		return nil
	}

	// Wait for readiness with debounce/jitter
	key := fmt.Sprintf("%s:slot:%d", t.Client.BaseURL, t.Slot)
	if err := t.Checker.WaitReady(ctx, key, serviceRef); err != nil {
		return fmt.Errorf("tuner readiness failed: %w", err)
	}

	// Optional middleware may require a moment to stabilize stream processing after tuner lock.
	// This prevents FFmpeg from reading initial unstable packets.
	time.Sleep(2000 * time.Millisecond)

	logger.Info().Msg("tuner locked and ready (settled)")
	return nil
}

// Healthy checks if the tuner is still locked and active.
// Note: It doesn't check ServiceRef strictness here because we might strictly enforce specific behavior in Tune.
// But for robustness, we should generally check we are still tuned to "something" or the intended service?
// The interface logic `Healthy(ctx, activeRef)` would be better, but signature is `Healthy(ctx)`.
// We will check generally for Lock.
func (t *Tuner) Healthy(ctx context.Context) error {
	sig, err := t.Client.GetSignal(ctx)
	if err != nil {
		return err
	}
	if !sig.Locked {
		return fmt.Errorf("tuner lost lock (snr=%d)", int(sig.Snr))
	}
	return nil
}

// Close is a no-op for Enigma2 (we don't shutdown the box).
func (t *Tuner) Close() error {
	return nil
}
