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

	if t.Client != nil && t.Client.useWebIFStreams {
		logger.Info().Msg("skipping zap for WebIF stream")
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

	logger.Info().Msg("tuner locked and ready")
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
