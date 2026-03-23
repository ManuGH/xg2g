package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/metrics"
)

const (
	preflightMinBytes = 188 * 3
	preflightTimeout  = 2 * time.Second
)

var ffmpegURLPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^'"\s]+`)

type preflightFn func(context.Context, string) (ports.PreflightResult, error)

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
	result = normalizeAdapterPreflightResult(result, err)
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "input_preflight_finished").
		Str("resolved_url", sanitizeURLForLog(streamURL)).
		Bool("ok", err == nil && result.OK).
		Int("bytes", result.Bytes).
		Int64("latency_ms", result.LatencyMs).
		Int("http_status", result.HTTPStatus).
		Msg("stream input preflight finished")
	reason := preflightReason(result)
	if err == nil && result.OK {
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
			Int("preflight_bytes", result.Bytes).
			Str("preflight_reason", reason).
			Str("preflight_detail", result.FailureDetail()).
			Int64("preflight_latency_ms", result.LatencyMs).
			Int("http_status", result.HTTPStatus).
			Int("resolved_port", result.ResolvedPort).
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
				Int("preflight_bytes", result.Bytes).
				Str("preflight_reason", string(ports.PreflightReasonFallbackURLInvalid)).
				Str("preflight_detail", "fallback_url_invalid").
				Int64("preflight_latency_ms", result.LatencyMs).
				Int("http_status", result.HTTPStatus).
				Int("resolved_port", result.ResolvedPort).
				Msg("preflight failed and fallback url was invalid")
			return "", ports.NewPreflightError(ports.PreflightResult{
				Reason:       ports.PreflightReasonFallbackURLInvalid,
				Detail:       "fallback_url_invalid",
				HTTPStatus:   result.HTTPStatus,
				Bytes:        result.Bytes,
				LatencyMs:    result.LatencyMs,
				ResolvedPort: result.ResolvedPort,
			})
		}
		fallbackURL = a.injectCredentialsIfAllowed(fallbackURL)
		fallbackLogURL := sanitizeURLForLog(fallbackURL)
		a.Logger.Warn().
			Str("event", "fallback_to_8001_activated").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
			Str("resolved_url", resolvedLogURL).
			Str("fallback_url", fallbackLogURL).
			Int("preflight_bytes", result.Bytes).
			Str("preflight_reason", reason).
			Str("preflight_detail", result.FailureDetail()).
			Int64("preflight_latency_ms", result.LatencyMs).
			Int("http_status", result.HTTPStatus).
			Int("resolved_port", result.ResolvedPort).
			Msg("fallback to 8001 activated after streamrelay preflight failure")

		fallbackResult, fallbackErr := preflight(ctx, fallbackURL)
		fallbackResult = normalizeAdapterPreflightResult(fallbackResult, fallbackErr)
		if fallbackErr == nil && fallbackResult.OK {
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
			origRes = normalizeAdapterPreflightResult(origRes, origErr)
			if origErr == nil && origRes.OK {
				a.Logger.Info().Str("url", sanitizeURLForLog(origURL)).Msg("fallback to original URL succeeded (M3U)")
				return origURL, nil
			}
		}

		a.Logger.Error().
			Str("event", "all_fallbacks_failed").
			Str("sessionId", sessionID).
			Msg("all stream source fallbacks failed")
		return "", ports.NewPreflightError(ports.PreflightResult{
			Reason:       ports.PreflightReasonFallbackFailed,
			Detail:       "fallback_failed_all",
			HTTPStatus:   result.HTTPStatus,
			Bytes:        result.Bytes,
			LatencyMs:    result.LatencyMs,
			ResolvedPort: result.ResolvedPort,
		})
	}

	a.Logger.Error().
		Str("event", "preflight_failed_no_valid_ts").
		Str("sessionId", sessionID).
		Str("service_ref", serviceRef).
		Str("resolved_url", resolvedLogURL).
		Int("preflight_bytes", result.Bytes).
		Str("preflight_reason", reason).
		Str("preflight_detail", result.FailureDetail()).
		Int64("preflight_latency_ms", result.LatencyMs).
		Int("http_status", result.HTTPStatus).
		Int("resolved_port", result.ResolvedPort).
		Msg("preflight failed for resolved stream url")
	return "", ports.NewPreflightError(result)
}

func (a *LocalAdapter) preflightTS(ctx context.Context, rawURL string) (result ports.PreflightResult, err error) {
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		result.LatencyMs = latency.Milliseconds()
		metrics.ObservePreflightLatency(result.ResolvedPort, latency)
		result = result.Normalized()
	}()

	if strings.TrimSpace(rawURL) == "" {
		result.Detail = "empty_url"
		return result, fmt.Errorf("preflight url empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		result.Detail = "invalid_url"
		return result, err
	}

	port := parsed.Port()
	if port == "" {
		port = defaultPortForScheme(parsed.Scheme)
	}
	if port != "" {
		if portInt, portErr := strconv.Atoi(port); portErr == nil {
			result.ResolvedPort = portInt
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
		result.Detail = "request_build_failed"
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
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			result.Detail = "timeout"
		} else {
			result.Detail = "request_failed"
		}
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()

	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		result.Detail = fmt.Sprintf("http_status_%d", resp.StatusCode)
		return result, fmt.Errorf("preflight http status %d", resp.StatusCode)
	}

	buf := make([]byte, preflightMinBytes)
	n, err := io.ReadAtLeast(resp.Body, buf, preflightMinBytes)
	result.Bytes = n

	latency := time.Since(start)
	a.Logger.Info().
		Str("url", sanitizeURLForLog(rawURL)).
		Int("bytes", n).
		Dur("latency", latency).
		Int64("preflight_latency_ms", latency.Milliseconds()).
		Int("http_status", result.HTTPStatus).
		Int("resolved_port", result.ResolvedPort).
		Msg("preflight read completed")

	if err != nil {
		result.Detail = "short_read"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			result.Detail = "timeout"
		}
		return result, err
	}

	if !hasTSSync(buf) {
		result.Detail = "sync_miss"
		return result, fmt.Errorf("preflight ts sync missing")
	}

	result = ports.NewSuccessfulPreflightResult(n, latency.Milliseconds(), result.ResolvedPort)
	return result, nil
}

func normalizeAdapterPreflightResult(result ports.PreflightResult, err error) ports.PreflightResult {
	if result.OK {
		return result.Normalized()
	}
	detail := result.FailureDetail()
	if detail == "" {
		if err != nil {
			detail = "request_failed"
		} else {
			detail = "unknown"
		}
	}
	return ports.NewPreflightResult(detail, result.HTTPStatus, result.Bytes, result.LatencyMs, result.ResolvedPort)
}

func preflightReason(result ports.PreflightResult) string {
	normalized := result.Normalized()
	if normalized.Reason != "" {
		return string(normalized.Reason)
	}
	return string(ports.PreflightReasonUnknown)
}

func hasTSSync(buf []byte) bool {
	if len(buf) < preflightMinBytes {
		return false
	}
	return buf[0] == 0x47 && buf[188] == 0x47 && buf[376] == 0x47
}

func buildFallbackURL(resolvedURL, serviceRef string) (string, error) {
	u, err := url.Parse(resolvedURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing host in resolved url")
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	u.Scheme = scheme
	u.Host = fmt.Sprintf("%s:%d", host, 8001)
	u.Path = "/" + serviceRef
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String(), nil
}

func isStreamRelayURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(u.Scheme)
	}
	return port == "17999"
}

func defaultPortForScheme(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func sanitizeURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

func sanitizeFFmpegLogLine(line string) string {
	return ffmpegURLPattern.ReplaceAllStringFunc(line, sanitizeURLForLog)
}

func (a *LocalAdapter) injectCredentialsIfAllowed(streamURL string) string {
	if a.E2 == nil {
		return streamURL
	}
	if a.E2.Username == "" && a.E2.Password == "" {
		return streamURL
	}

	u, err := url.Parse(streamURL)
	if err != nil {
		return streamURL
	}

	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(u.Scheme)
	}

	if port == "80" || port == "443" || port == "8001" || port == "8002" {
		if a.E2.Username != "" {
			u.User = url.UserPassword(a.E2.Username, a.E2.Password)
		}
		return u.String()
	}

	return streamURL
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
