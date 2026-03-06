package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/metrics"
)

type preflightResult struct {
	ok           bool
	bytes        int
	reason       string
	httpStatus   int
	latencyMs    int64
	resolvedPort int
}

type preflightFn func(context.Context, string) (preflightResult, error)

func (a *LocalAdapter) selectStreamURL(ctx context.Context, sessionID, serviceRef, streamURL string) (string, error) {
	return a.selectStreamURLWithPreflight(ctx, sessionID, serviceRef, streamURL, a.preflightTS)
}

func (a *LocalAdapter) selectStreamURLWithPreflight(ctx context.Context, sessionID, serviceRef, streamURL string, preflight preflightFn) (string, error) {
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "input_preflight_started").
		Str("resolved_url", sanitizeURLForLog(streamURL)).
		Msg("stream input preflight started")
	result, err := preflight(ctx, streamURL)
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "input_preflight_finished").
		Str("resolved_url", sanitizeURLForLog(streamURL)).
		Bool("ok", err == nil && result.ok).
		Int("bytes", result.bytes).
		Int64("latency_ms", result.latencyMs).
		Int("http_status", result.httpStatus).
		Msg("stream input preflight finished")
	reason := preflightReason(result, err)
	if err == nil && result.ok {
		return streamURL, nil
	}

	resolvedLogURL := sanitizeURLForLog(streamURL)
	isRelay := isStreamRelayURL(streamURL)
	if isRelay {
		a.Logger.Warn().
			Str("event", "streamrelay_preflight_failed").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
			Str("resolved_url", resolvedLogURL).
			Int("preflight_bytes", result.bytes).
			Str("preflight_reason", reason).
			Int64("preflight_latency_ms", result.latencyMs).
			Int("http_status", result.httpStatus).
			Int("resolved_port", result.resolvedPort).
			Msg("streamrelay preflight failed")
	}

	if isRelay && a.FallbackTo8001 {
		fallbackURL, buildErr := buildFallbackURL(streamURL, serviceRef)
		if buildErr != nil {
			a.Logger.Error().
				Str("event", "preflight_failed_no_valid_ts").
				Str("sessionId", sessionID).
				Str("service_ref", serviceRef).
				Str("resolved_url", resolvedLogURL).
				Int("preflight_bytes", result.bytes).
				Str("preflight_reason", "fallback_url_invalid").
				Int64("preflight_latency_ms", result.latencyMs).
				Int("http_status", result.httpStatus).
				Int("resolved_port", result.resolvedPort).
				Msg("preflight failed and fallback url was invalid")
			return "", &ports.PreflightError{Reason: "fallback_url_invalid"}
		}
		fallbackURL = a.injectCredentialsIfAllowed(fallbackURL)
		fallbackLogURL := sanitizeURLForLog(fallbackURL)
		a.Logger.Warn().
			Str("event", "fallback_to_8001_activated").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
			Str("resolved_url", resolvedLogURL).
			Str("fallback_url", fallbackLogURL).
			Int("preflight_bytes", result.bytes).
			Str("preflight_reason", reason).
			Int64("preflight_latency_ms", result.latencyMs).
			Int("http_status", result.httpStatus).
			Int("resolved_port", result.resolvedPort).
			Msg("fallback to 8001 activated after streamrelay preflight failure")

		fallbackResult, fallbackErr := preflight(ctx, fallbackURL)
		if fallbackErr == nil && fallbackResult.ok {
			return fallbackURL, nil
		}
		a.Logger.Warn().Str("url", fallbackLogURL).Msg("fallback 8001 failed, trying original WebIF URL")

		if a.E2 != nil && a.E2.BaseURL != "" {
			u, _ := url.Parse(a.E2.BaseURL)
			u.Path = "/web/stream.m3u"
			q := u.Query()
			q.Set("ref", serviceRef)
			u.RawQuery = q.Encode()
			origURL := u.String()

			origRes, origErr := preflight(ctx, origURL)
			if (origErr == nil && origRes.httpStatus == 200) || (origRes.httpStatus == 200 && origRes.bytes > 0) {
				a.Logger.Info().Str("url", sanitizeURLForLog(origURL)).Msg("fallback to original URL succeeded (M3U)")
				return origURL, nil
			}
		}

		a.Logger.Error().
			Str("event", "all_fallbacks_failed").
			Str("sessionId", sessionID).
			Msg("all stream source fallbacks failed")
		return "", &ports.PreflightError{Reason: "fallback_failed_all"}
	}

	a.Logger.Error().
		Str("event", "preflight_failed_no_valid_ts").
		Str("sessionId", sessionID).
		Str("service_ref", serviceRef).
		Str("resolved_url", resolvedLogURL).
		Int("preflight_bytes", result.bytes).
		Str("preflight_reason", reason).
		Int64("preflight_latency_ms", result.latencyMs).
		Int("http_status", result.httpStatus).
		Int("resolved_port", result.resolvedPort).
		Msg("preflight failed for resolved stream url")
	return "", &ports.PreflightError{Reason: reason}
}

func (a *LocalAdapter) preflightTS(ctx context.Context, rawURL string) (result preflightResult, err error) {
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		result.latencyMs = latency.Milliseconds()
		metrics.ObservePreflightLatency(result.resolvedPort, latency)
	}()

	if strings.TrimSpace(rawURL) == "" {
		result.reason = "empty_url"
		return result, fmt.Errorf("preflight url empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		result.reason = "invalid_url"
		return result, err
	}

	port := parsed.Port()
	if port == "" {
		port = defaultPortForScheme(parsed.Scheme)
	}
	if port != "" {
		if portInt, portErr := strconv.Atoi(port); portErr == nil {
			result.resolvedPort = portInt
		}
	}

	timeout := a.PreflightTimeout
	if timeout <= 0 {
		timeout = preflightTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, _, err := buildAuthenticatedRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		result.reason = "request_build_failed"
		return result, err
	}

	client := a.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				ResponseHeaderTimeout: timeout,
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		result.reason = "request_failed"
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()

	result.httpStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		result.reason = fmt.Sprintf("http_status_%d", resp.StatusCode)
		return result, fmt.Errorf("preflight http status %d", resp.StatusCode)
	}

	buf := make([]byte, preflightMinBytes)
	n, err := io.ReadAtLeast(resp.Body, buf, preflightMinBytes)
	result.bytes = n

	latency := time.Since(start)
	a.Logger.Info().
		Str("url", sanitizeURLForLog(rawURL)).
		Int("bytes", n).
		Dur("latency", latency).
		Int64("preflight_latency_ms", latency.Milliseconds()).
		Int("http_status", result.httpStatus).
		Int("resolved_port", result.resolvedPort).
		Msg("preflight read completed")

	if err != nil {
		result.reason = "short_read"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			result.reason = "timeout"
		}
		return result, err
	}

	if !hasTSSync(buf) {
		result.reason = "sync_miss"
		return result, fmt.Errorf("preflight ts sync missing")
	}

	result.ok = true
	return result, nil
}

func preflightReason(result preflightResult, err error) string {
	if result.reason != "" {
		return result.reason
	}
	if err != nil {
		return "request_failed"
	}
	return "unknown"
}

type streamWarmupResult struct {
	bytes      int
	httpStatus int
	latencyMs  int64
}

func buildAuthenticatedRequest(ctx context.Context, method, rawURL string) (*http.Request, *url.URL, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, nil, fmt.Errorf("request url empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}

	reqURL := *parsed
	user := reqURL.User
	reqURL.User = nil

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	if user != nil {
		username := user.Username()
		password, _ := user.Password()
		if username != "" || password != "" {
			req.SetBasicAuth(username, password)
		}
	}

	return req, parsed, nil
}

func isHTTPInputURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func (a *LocalAdapter) warmupInputStream(ctx context.Context, rawURL string, duration time.Duration) (result streamWarmupResult, err error) {
	start := time.Now()
	defer func() {
		result.latencyMs = time.Since(start).Milliseconds()
	}()

	if duration <= 0 {
		return result, nil
	}

	warmupCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	req, _, err := buildAuthenticatedRequest(warmupCtx, http.MethodGet, rawURL)
	if err != nil {
		return result, err
	}

	client := a.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: duration,
			Transport: &http.Transport{
				ResponseHeaderTimeout: duration,
			},
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		if warmupCtx.Err() != nil {
			return result, nil
		}
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()

	result.httpStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("warmup http status %d", resp.StatusCode)
	}

	buf := make([]byte, preflightMinBytes)
	n, readErr := resp.Body.Read(buf)
	result.bytes = n
	if readErr != nil && !errors.Is(readErr, io.EOF) && warmupCtx.Err() == nil {
		return result, readErr
	}
	return result, nil
}
