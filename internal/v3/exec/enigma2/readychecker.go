package enigma2

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"golang.org/x/sync/singleflight"
)

// ReadyChecker ensures the tuner is locked to the correct service.
type ReadyChecker struct {
	Client     *Client
	PollBase   time.Duration
	JitterFrac float64
	DebounceN  int
	sf         singleflight.Group
	rnd        *rand.Rand
	mu         sync.Mutex
}

// NewReadyChecker creates a new checker with safe defaults.
func NewReadyChecker(c *Client) *ReadyChecker {
	return &ReadyChecker{
		Client:     c,
		PollBase:   250 * time.Millisecond,
		JitterFrac: 0.2,                                             // +/- 20%
		DebounceN:  2,                                               // 2 consecutive successes
		rnd:        rand.New(rand.NewSource(time.Now().UnixNano())), // #nosec G404 -- jitter only, no crypto
	}
}

// NormalizeServiceRef standardizes service references for comparison.
func NormalizeServiceRef(ref string) string {
	parts := strings.Split(ref, ":")
	if len(parts) >= 10 {
		// Mask Namespace (index 6) to avoid mismatches (e.g. C00000 vs C00001)
		parts[6] = "0"
		// Reassemble the first 10 fields (standard Enigma2 ref parts)
		// This also effectively strips any trailing URL or channel name
		return strings.Join(parts[:10], ":") + ":"
	}
	return strings.TrimSuffix(strings.TrimSpace(ref), ":")
}

// WaitReady blocks until the tuner is locked to the expected service or ctx errors.
// It uses singleflight keyed by the provided contention key (e.g. host:slot).
func (rc *ReadyChecker) WaitReady(ctx context.Context, key, expectedRef string) error {
	_, err, _ := rc.sf.Do(key, func() (interface{}, error) {
		return nil, rc.waitReadyInner(ctx, expectedRef)
	})
	return err
}

func (rc *ReadyChecker) waitReadyInner(ctx context.Context, expectedRef string) error {
	expected := NormalizeServiceRef(expectedRef)
	success := 0

	// Use Timer for jittered polling (avoids Ticker drift)
	timer := time.NewTimer(0)
	defer timer.Stop()

	// Drain immediate fire if needed, though NewTimer(0) fires immediately
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	for {
		// 1. Probe State
		if err := rc.check(ctx, expected); err != nil {
			// If context is done, return specific classification
			if ctx.Err() != nil {
				return classifyCtxErr(ctx.Err())
			}
			// Transient failure (not locked, wrong ref, network) -> reset success count
			success = 0
		} else {
			success++
		}

		if success >= rc.DebounceN {
			return nil
		}

		// 2. Schedule next poll
		d := rc.jittered(rc.PollBase)
		timer.Reset(d)

		select {
		case <-ctx.Done():
			return classifyCtxErr(ctx.Err())
		case <-timer.C:
			// continue loop
		}
	}
}

func (rc *ReadyChecker) check(ctx context.Context, expected string) error {
	// A. Get Current
	curr, err := rc.Client.GetCurrent(ctx)
	if err != nil {
		return fmt.Errorf("%w: get current: %w", ErrUpstreamUnavailable, err)
	}

	actual := NormalizeServiceRef(curr.Info.ServiceReference)
	if actual != expected {
		// Log mismatch for debugging
		log.L().Debug().Str("expected", expected).Str("actual", actual).Str("raw", curr.Info.ServiceReference).Msg("ReadyChecker mismatch")
		return fmt.Errorf("%w: expected %s, got %s", ErrWrongServiceRef, expected, actual)
	}

	// B. Check Signal
	sig, err := rc.Client.GetSignal(ctx)
	if err != nil {
		return fmt.Errorf("%w: get signal: %w", ErrUpstreamUnavailable, err)
	}

	if !sig.Locked {
		log.L().Debug().Int("snr", int(sig.Snr)).Msg("ReadyChecker not locked")
		return ErrNotLocked
	}

	return nil
}

func (rc *ReadyChecker) jittered(base time.Duration) time.Duration {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	// +/- JitterFrac
	f := 1.0 + (rc.rnd.Float64()*2.0-1.0)*rc.JitterFrac
	return time.Duration(float64(base) * f)
}

func classifyCtxErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrReadyTimeout
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return ErrReadyTimeout
}
