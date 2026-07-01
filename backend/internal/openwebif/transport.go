package openwebif

import (
	"context"
	"errors"
	"fmt"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/resilience"
	"github.com/rs/zerolog"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// slowRequestInfoThreshold is the duration above which a successful receiver
// request is still logged at info level; faster successes go to debug since
// metrics already cover them.
const slowRequestInfoThreshold = 500 * time.Millisecond

func (c *Client) isTechnicalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		if owiErr.Status >= 500 {
			return true
		}
	}
	return false
}

func (c *Client) backoffDuration(attempt int) time.Duration {
	if c.backoff <= 0 {
		return 0
	}
	factor := 1 << (attempt - 1)
	d := min(time.Duration(factor)*c.backoff, c.maxBackoff)
	return d
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return netErr.Timeout()
		}
		return true
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 {
		return true
	}
	return false
}

func classifyError(err error, status int) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			if netErr.Timeout() {
				return "timeout"
			}
			return "network"
		}
		return "error"
	}
	if status >= 500 {
		return "http_5xx"
	}
	if status >= 400 {
		return "http_4xx"
	}
	if status == 0 {
		return "unknown"
	}
	return "ok"
}

func wrapError(operation string, err error, status int, body []byte) error {
	var sentinel error
	var snippet string

	// Append body snippet if available (useful for Enigma2 stack traces)
	if len(body) > 0 {
		limit := min(len(body), 500)
		snippet = string(body[:limit])
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		snippet = strings.ReplaceAll(snippet, "\r", "")

		// Crude redaction for sensitive patterns (tokens, passwords, session keys)
		sensitivePatterns := []string{"token=", "password=", "secret=", "key=", "sid="}
		for _, pattern := range sensitivePatterns {
			if idx := strings.Index(strings.ToLower(snippet), pattern); idx != -1 {
				// Redact 16 chars or until space after the pattern
				start := idx + len(pattern)
				end := min(start+16, len(snippet))
				if spaceIdx := strings.Index(snippet[start:end], " "); spaceIdx != -1 {
					end = start + spaceIdx
				}
				snippet = snippet[:start] + "[REDACTED]" + snippet[end:]
			}
		}
	}

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			sentinel = ErrTimeout
		} else {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				sentinel = ErrTimeout
			} else {
				sentinel = ErrUpstreamUnavailable
			}
		}
		return &OWIError{
			Sentinel:  sentinel,
			Operation: operation,
			Status:    status,
			Body:      snippet,
			Err:       err,
		}
	}

	switch status {
	case http.StatusNotFound:
		sentinel = ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		sentinel = ErrForbidden
	default:
		if status >= 500 {
			sentinel = ErrUpstreamError
		} else if status >= 400 {
			sentinel = ErrUpstreamBadResponse
		} else {
			sentinel = ErrUpstreamBadResponse // Treat non-200/4xx/5xx as bad response
		}
	}

	return &OWIError{
		Sentinel:  sentinel,
		Operation: operation,
		Status:    status,
		Body:      snippet,
	}
}

func logPath(path string) string {
	endpoint := path
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return endpoint
}

func (c *Client) logAttempt(
	ctx context.Context,
	operation string,
	path string,
	attempt, maxAttempts, status int,
	duration time.Duration,
	err error,
	errClass string,
	retry bool,
	decorate func(*zerolog.Context),
) {
	builder := c.loggerFor(ctx).With().
		Str("event", "openwebif.request").
		Str("operation", operation).
		Str("method", http.MethodGet).
		Str("endpoint", logPath(path)).
		Int("attempt", attempt).
		Int("max_attempts", maxAttempts).
		Int64("duration_ms", duration.Milliseconds()).
		Str("error_class", errClass)
	if status > 0 {
		builder = builder.Int("status", status)
	}
	if decorate != nil {
		decorate(&builder)
	}
	logger := builder.Logger()
	if err == nil && status == http.StatusOK {
		// Success metrics are already recorded by recordAttemptMetrics; keep the
		// steady-state polling traffic out of the info log and only surface
		// unusually slow requests there.
		evt := logger.Debug()
		if duration >= slowRequestInfoThreshold {
			evt = logger.Info()
		}
		evt.Msg("openwebif request completed")
		return
	}
	if retry {
		logger.Warn().Err(err).Msg("openwebif request retry")
		return
	}
	logger.Error().Err(err).Msg("openwebif request failed")
}

func recordAttemptMetrics(operation string, attempt, status int, duration time.Duration, success bool, errClass string, retry bool) {
	statusLabel := "0"
	if status > 0 {
		statusLabel = strconv.Itoa(status)
	}
	requestDuration.WithLabelValues(operation, statusLabel, strconv.Itoa(attempt)).Observe(duration.Seconds())
	if success {
		requestSuccess.WithLabelValues(operation).Inc()
		return
	}
	requestFailures.WithLabelValues(operation, errClass).Inc()
	if retry {
		requestRetries.WithLabelValues(operation).Inc()
	}
}

func (c *Client) get(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) ([]byte, error) {
	// Apply receiver rate limiting before making request
	if err := c.receiverLimiter.Wait(ctx); err != nil {
		// A cancelled context is expected during shutdown or when a batch
		// (e.g. EPG refresh) is aborted; every queued request fails at once,
		// so warn-level logging would flood the log with one line per request.
		evt := c.loggerFor(ctx).Warn()
		if errors.Is(err, context.Canceled) {
			evt = c.loggerFor(ctx).Debug()
		}
		evt.Err(err).
			Str("event", "openwebif.rate_limit").
			Str("operation", operation).
			Msg("rate limit wait cancelled")
		return nil, fmt.Errorf("rate limit wait cancelled: %w", err)
	}

	// Wrap request in Circuit Breaker
	if !c.cb.AllowRequest() {
		c.loggerFor(ctx).Warn().
			Str("event", "circuit_breaker.open").
			Str("operation", operation).
			Msg("request blocked by circuit breaker")
		return nil, resilience.ErrCircuitOpen
	}

	// Record the permitted request as an attempt. Without it, EventAttempt is
	// never recorded and the breaker's `attempts >= minAttempts` trip gate can
	// never be satisfied, so it would never open no matter how many requests fail.
	c.cb.RecordAttempt()

	result, err := c.doGet(ctx, path, operation, decorate)
	if err != nil {
		// Only record technical failures. Use the method variant (c.isTechnicalError),
		// which also classifies upstream HTTP 5xx (OWIError.Status >= 500) as technical.
		// The package-level isTechnicalError is status-blind, so calling it here meant
		// 5xx responses never reached the breaker and it could never trip on them.
		if c.isTechnicalError(err) {
			c.cb.RecordTechnicalFailure()
		}
		return nil, err
	}

	c.cb.RecordSuccess()
	return result, nil
}

// doGet performs the actual HTTP request with retries (extracted from get)
func (c *Client) doGet(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) ([]byte, error) {

	maxAttempts := c.maxRetries + 1
	var lastErr error
	var lastStatus int
	var lastData []byte
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var res *http.Response
		var err error
		var status int
		var duration time.Duration
		var data []byte

		func() {
			attemptCtx := ctx
			var cancel context.CancelFunc
			if c.timeout > 0 {
				attemptCtx, cancel = context.WithTimeout(ctx, c.timeout)
				defer cancel() // Ensure cancel is always called
			}

			req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodGet, c.base+path, nil)
			if reqErr != nil {
				c.loggerFor(ctx).Error().Err(reqErr).
					Str("event", "openwebif.request.build").
					Str("operation", operation).
					Msg("failed to build OpenWebIF request")
				err = reqErr
				return
			}

			// Add HTTP Basic Auth if credentials are provided
			if c.username != "" && c.password != "" {
				req.SetBasicAuth(c.username, c.password)
			}

			// HYGIENE: Enforce connection closure
			req.Close = true
			req.Header.Set("Connection", "close")

			start := time.Now()
			res, err = c.http.Do(req)
			duration = time.Since(start)
			if res != nil {
				status = res.StatusCode
				defer func() {
					if res.Body != nil {
						// HYGIENE: Drain body safely to ensure TCP socket hygiene
						// Limit drain to maxDrainBytes to prevent unbounded reads on stuck streams
						// EOF is expected and ignored.
						_, _ = io.CopyN(io.Discard, res.Body, maxDrainBytes)
						_ = res.Body.Close()
					}
				}()
			}

			if err == nil {
				if status == http.StatusOK {
					// Read body fully while attemptCtx is still active
					var readErr error

					// Check Content-Type header for charset
					contentType := res.Header.Get("Content-Type")

					// Read raw bytes first
					rawData, readErr := io.ReadAll(res.Body)
					if readErr != nil {
						err = readErr
						return
					}

					// Handle encoding if needed (e.g., ISO-8859-1)
					if needsLatin1Conversion(rawData, contentType) {
						data = convertLatin1ToUTF8(rawData)
					} else {
						data = rawData
					}
				} else if res.Body != nil {
					// HYGIENE: For non-200 responses, read a bounded snippet for context/logging.
					// This ensures we have debug data (e.g. Enigma2 stack traces) without risking huge reads.
					// The rest will be drained by the defer.
					// Applies the same charset conversion logic as success path (e.g., ISO-8859-1).
					rawSnippet, _ := io.ReadAll(io.LimitReader(res.Body, maxErrBody))

					contentType := res.Header.Get("Content-Type")
					if needsLatin1Conversion(rawSnippet, contentType) {
						data = convertLatin1ToUTF8(rawSnippet)
					} else {
						data = rawSnippet
					}
				}
			}
		}()

		// Metrics & Logging
		success := err == nil && status == http.StatusOK
		errClass := classifyError(err, status)
		retry := !success && attempt < maxAttempts && shouldRetry(status, err)

		c.logAttempt(ctx, operation, path, attempt, maxAttempts, status, duration, err, errClass, retry, decorate)
		recordAttemptMetrics(operation, attempt, status, duration, success, errClass, retry)

		if success {
			return data, nil
		}

		lastErr = err
		lastStatus = status
		lastData = data

		if !retry {
			break
		}

		// Wait before retry
		if attempt < maxAttempts {
			sleep := c.backoffDuration(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(sleep):
				continue
			}
		}
	}

	return nil, wrapError(operation, lastErr, lastStatus, lastData)
}

func (c *Client) loggerFor(ctx context.Context) *zerolog.Logger {
	logger := xglog.WithContext(ctx, c.log)
	return &logger
}
