// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"

	"github.com/ManuGH/xg2g/internal/metrics"
)

// ReadyChecker defines the interface for checking stream readiness.
type ReadyChecker interface {
	WaitReady(ctx context.Context, serviceRef string) error
	CheckInvariant(ctx context.Context, serviceRef string) error
}

// Enigma2ReadyChecker implements a robust, polled readiness check for Enigma2 receivers.
type Enigma2ReadyChecker struct {
	client openwebif.ClientInterface
	logger zerolog.Logger
	sf     singleflight.Group
}

// NewReadyChecker creates a new readiness checker.
func NewReadyChecker(client openwebif.ClientInterface, logger zerolog.Logger) *Enigma2ReadyChecker {
	return &Enigma2ReadyChecker{
		client: client,
		logger: logger,
	}
}

// CheckInvariant verifies that the receiver is actually tuned to the expected service reference.
// This is a defensive check to be called immediately before starting the stream.
func (c *Enigma2ReadyChecker) CheckInvariant(ctx context.Context, serviceRef string) error {
	status, err := c.client.GetStatusInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status info: %w", err)
	}

	currentRef := status.ServiceRef
	if currentRef == "" {
		// Fallback to getcurrent if statusinfo is empty (unlikely but safe)
		curr, err := c.client.GetCurrent(ctx)
		if err == nil {
			currentRef = curr.Info.ServiceRef
		}
	}

	if currentRef != serviceRef {
		c.logger.Error().
			Str("expected_ref", serviceRef).
			Str("actual_ref", currentRef).
			Msg("invariant violation: receiver tuned to wrong service at stream start")
		return fmt.Errorf("%w: expected ref %q, got %q", ErrInvariant, serviceRef, currentRef)
	}
	return nil
}

// WaitReady waits for the Enigma2 receiver to be fully ready to stream the given service.
// It uses singleflight to ensure only one poll loop runs per serviceRef.
func (c *Enigma2ReadyChecker) WaitReady(ctx context.Context, serviceRef string) error {
	// P4c Hardening: Decouple shared poll from individual request cancellation.
	// Use context.WithoutCancel (Go 1.21+) to keep the poll loop alive even if the
	// initiating client disconnects, preventing "blast radius" cancellations for shared waiters.
	// We preserve the deadline if present, but ignore the Cancel channel of the parent.
	sharedCtx := context.WithoutCancel(ctx)
	if dl, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		sharedCtx, cancel = context.WithDeadline(sharedCtx, dl)
		defer cancel()
	}

	// Use singleflight to deduplicate concurrent requests for the same service
	_, err, _ := c.sf.Do(serviceRef, func() (interface{}, error) {
		return nil, c.pollUntilReady(sharedCtx, serviceRef)
	})

	// If the shared work failed, return that error.
	// If the individual client context was canceled while waiting, return that error explicitly.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return err
}

func (c *Enigma2ReadyChecker) pollUntilReady(ctx context.Context, ref string) (err error) {
	const (
		maxPolls       = 30
		baseInterval   = 250 * time.Millisecond
		jitterMax      = 100 // ms
		debounceNeeded = 2
	)

	start := time.Now()
	// Variable to track current ref for defer closure
	var currentRef string
	defer func() {
		dur := time.Since(start)
		outcome := "ready"
		if err != nil {
			if errors.Is(err, context.Canceled) {
				outcome = "cancelled"
			} else if errors.Is(err, ErrReadyTimeout) {
				// Distinguish tuning failure (ref mismatch) from other timeouts
				if currentRef != "" && currentRef != ref {
					outcome = "timeout_ref_mismatch"
				} else {
					outcome = "timeout"
				}
			} else {
				outcome = "error"
			}
		}
		metrics.ObserveEnigma2Ready(dur)
		metrics.IncEnigma2ReadyOutcome(outcome)
	}()

	owiClient := c.client
	stableCount := 0

	// Local seeded RNG for thread-safe deterministic jitter without global lock contention
	src := rand.NewSource(time.Now().UnixNano())
	rng := rand.New(src)

	c.logger.Info().Str("ref", ref).Msg("checking readiness...")

	// Use a timer for precise control over the loop interval + jitter
	timer := time.NewTimer(0) // Start immediately
	defer timer.Stop()

	// Capture detailed state for debugging timeouts
	var lastState struct {
		CurrentRef string
		SNR        int
		IsStandby  bool
		VideoPID   int
		PMTPID     int
	}

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			// 1. Check Status Info (Fast & Reliable Service Ref Check)
			status, err := owiClient.GetStatusInfo(ctx)
			if err != nil {
				c.logger.Warn().Err(err).Msg("failed to get status info")
				c.scheduleNextPoll(timer, rng, baseInterval, jitterMax)
				continue
			}

			// 2. Check Signal & Standby
			sig, err := owiClient.GetSignal(ctx)
			if err != nil {
				c.logger.Warn().Err(err).Msg("failed to get signal info")
				c.scheduleNextPoll(timer, rng, baseInterval, jitterMax)
				continue
			}

			// 3. Check PIDs (Proof of stream data)
			curr, err := owiClient.GetCurrent(ctx)
			if err != nil {
				c.logger.Warn().Err(err).Msg("failed to get current info")
				c.scheduleNextPoll(timer, rng, baseInterval, jitterMax)
				continue
			}

			// Refine Criteria based on User Feedback
			currentRef = status.ServiceRef
			if currentRef == "" {
				currentRef = curr.Info.ServiceRef // Fallback if statusinfo missing ref
			}

			lastState.CurrentRef = currentRef
			lastState.SNR = sig.SNR
			lastState.IsStandby = (sig.InStandby == "true" || status.InStandby == "true")
			lastState.VideoPID = curr.Info.VideoPID
			lastState.PMTPID = curr.Info.PMTPID

			isCorrectRef := (currentRef == ref)

			// Signal Criteria: "Locked/Decode indirekt: snr > 0 + inStandby == false + PIDs vorhanden"
			hasSignal := sig.SNR > 0
			isStandby := lastState.IsStandby
			hasPIDs := (curr.Info.VideoPID > 0 && curr.Info.PMTPID > 0)

			isReady := isCorrectRef &&
				hasSignal &&
				!isStandby &&
				hasPIDs

			if isReady {
				stableCount++
				if stableCount >= debounceNeeded {
					c.logger.Info().
						Int("polls", i+1).
						Str("vpid", fmt.Sprintf("%d", curr.Info.VideoPID)).
						Int("snr", sig.SNR).
						Str("svc_ref", currentRef).
						Msg("stream ready")
					return nil
				}
			} else {
				if stableCount > 0 {
					c.logger.Debug().Msg("flapping stream state, resetting debounce")
				}
				stableCount = 0
			}

			c.scheduleNextPoll(timer, rng, baseInterval, jitterMax)
		}
	}

	c.logger.Error().
		Str("expected_ref", ref).
		Str("actual_ref", lastState.CurrentRef).
		Int("snr", lastState.SNR).
		Bool("standby", lastState.IsStandby).
		Int("video_pid", lastState.VideoPID).
		Int("pmt_pid", lastState.PMTPID).
		Msg("timeout waiting for stream readiness")
	return ErrReadyTimeout
}

func (c *Enigma2ReadyChecker) scheduleNextPoll(timer *time.Timer, rng *rand.Rand, base time.Duration, jitterMax int) {
	// sleep(baseInterval + jitter) via time.NewTimer (deterministisch, ohne Drift)
	jitter := time.Duration(rng.Intn(jitterMax)) * time.Millisecond
	timer.Reset(base + jitter)
}
