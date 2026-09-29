// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// ResilientConfig configures upstream auto-reconnection parameters.
type ResilientConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultResilientConfig returns default resilient reader configuration.
func DefaultResilientConfig() ResilientConfig {
	return ResilientConfig{
		MaxRetries:     6,
		InitialBackoff: 250 * time.Millisecond,
		MaxBackoff:     1500 * time.Millisecond,
	}
}

// ResilientUpstreamReader provides transparent auto-reconnect capability over
// volatile HTTP upstream TS streams, shielding downstream ring buffers from disconnects.
type ResilientUpstreamReader struct {
	ctx       context.Context
	cancel    context.CancelFunc
	client    *http.Client
	targetURL string
	headers   http.Header
	cfg       ResilientConfig

	mu                sync.Mutex
	currentBody       io.ReadCloser
	consecutiveErrors int
	hasConnected      bool
	closed            bool

	ReconnectCount int64
	BytesRead      int64
}

// NewResilientUpstreamReader creates a resilient reader wrapping an existing initial body.
func NewResilientUpstreamReader(
	parentCtx context.Context,
	client *http.Client,
	targetURL string,
	headers http.Header,
	initialBody io.ReadCloser,
	cfg ResilientConfig,
) *ResilientUpstreamReader {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 6
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 250 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 1500 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(parentCtx)

	h := make(http.Header)
	for k, vv := range headers {
		for _, v := range vv {
			h.Add(k, v)
		}
	}

	return &ResilientUpstreamReader{
		ctx:          ctx,
		cancel:       cancel,
		client:       client,
		targetURL:    targetURL,
		headers:      h,
		cfg:          cfg,
		currentBody:  initialBody,
		hasConnected: initialBody != nil,
	}
}

// Read reads from the active upstream body, transparently reconnecting on failure.
func (r *ResilientUpstreamReader) Read(p []byte) (int, error) {
	for {
		if r.ctx.Err() != nil {
			return 0, r.ctx.Err()
		}

		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return 0, io.EOF
		}
		body := r.currentBody
		r.mu.Unlock()

		if body == nil {
			if err := r.reconnect(); err != nil {
				return 0, err
			}
			continue
		}

		n, err := body.Read(p)
		if n > 0 {
			atomic.AddInt64(&r.BytesRead, int64(n))
			if err != nil {
				// We read n bytes, but stream reached EOF or error.
				// Close current body and clear it so subsequent Read() reconnects transparently.
				r.mu.Lock()
				_ = body.Close()
				r.currentBody = nil
				r.mu.Unlock()
				return n, nil
			}
			r.mu.Lock()
			r.consecutiveErrors = 0
			r.mu.Unlock()
			return n, nil
		}

		if err != nil {
			if r.ctx.Err() != nil {
				return 0, r.ctx.Err()
			}
			r.mu.Lock()
			_ = body.Close()
			r.currentBody = nil
			r.mu.Unlock()

			if reconnErr := r.reconnect(); reconnErr != nil {
				return 0, reconnErr
			}
			continue
		}
	}
}

// reconnect attempts to re-establish an HTTP connection to the target URL.
func (r *ResilientUpstreamReader) reconnect() error {
	for {
		if r.ctx.Err() != nil {
			return r.ctx.Err()
		}

		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return io.EOF
		}
		r.consecutiveErrors++
		if r.consecutiveErrors > r.cfg.MaxRetries {
			attemptCount := r.consecutiveErrors - 1
			r.mu.Unlock()
			log.L().Error().
				Str("targetURL", r.targetURL).
				Int("maxRetries", r.cfg.MaxRetries).
				Msg("upstream reconnect failed: max retries exceeded")
			return fmt.Errorf("upstream reconnect failed after %d attempts", attemptCount)
		}
		attempt := r.consecutiveErrors
		r.mu.Unlock()

		backoff := r.cfg.InitialBackoff * time.Duration(1<<(attempt-1))
		if backoff > r.cfg.MaxBackoff {
			backoff = r.cfg.MaxBackoff
		}

		log.L().Warn().
			Str("targetURL", r.targetURL).
			Int("attempt", attempt).
			Int("maxRetries", r.cfg.MaxRetries).
			Dur("backoff", backoff).
			Msg("upstream stream interrupted: waiting before transparent reconnect")

		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-time.After(backoff):
		}

		req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.targetURL, nil)
		if err != nil {
			continue
		}
		for k, vv := range r.headers {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}

		resp, err := r.client.Do(req)
		if err != nil {
			log.L().Warn().
				Err(err).
				Str("targetURL", r.targetURL).
				Int("attempt", attempt).
				Msg("upstream reconnect HTTP dial failed")
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			log.L().Warn().
				Int("statusCode", resp.StatusCode).
				Str("targetURL", r.targetURL).
				Int("attempt", attempt).
				Msg("upstream reconnect returned non-200 status")
			continue
		}

		r.mu.Lock()
		if r.closed {
			_ = resp.Body.Close()
			r.mu.Unlock()
			return io.EOF
		}
		r.currentBody = resp.Body
		r.consecutiveErrors = 0
		if r.hasConnected {
			r.ReconnectCount++
		} else {
			r.hasConnected = true
		}
		reconnCount := r.ReconnectCount
		r.mu.Unlock()

		if reconnCount > 0 {
			log.L().Info().
				Str("targetURL", r.targetURL).
				Int64("totalReconnects", reconnCount).
				Msg("upstream stream transparently reconnected")
		} else {
			log.L().Info().
				Str("targetURL", r.targetURL).
				Msg("upstream stream initially connected")
		}
		return nil
	}
}

// Close closes the reader and any open underlying upstream body.
func (r *ResilientUpstreamReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	r.cancel()

	if r.currentBody != nil {
		err := r.currentBody.Close()
		r.currentBody = nil
		return err
	}
	return nil
}
