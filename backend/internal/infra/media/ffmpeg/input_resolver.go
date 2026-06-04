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
	preflightMinBytes          = 188 * 3  // sync floor: never raised, keeps preflight latency/timeout behaviour unchanged
	preflightScanBytes         = 188 * 48 // best-effort upper bound scanned for scrambling (9024B); only what the relay delivers for free is used
	preflightTimeout           = 2 * time.Second
	preflightMaxTries          = 3
	preflightDirectWarmupTries = 8

	// A stream-relay source (port 17999) can emit MPEG-TS packets with the
	// transport_scrambling_control bits set for a brief interval at the start of a
	// freshly-started stream; they clear once the stream stabilizes. Sampling only
	// the first 48 packets (preflightScanBytes) can land entirely inside that initial
	// interval and misclassify the whole stream — and each retry opens a fresh
	// connection that lands in it again. For relay sources we read further and
	// classify on the trailing window instead (see scrambleFractionForSource).
	preflightRelayScanBytes = 188 * 4096 // ~752KB: past observed descrambler/ECM-lock (~367KB / ~2000 pkt on a real icam channel) + margin. 188*1024 landed INSIDE the lock -> false R_UPSTREAM_SCRAMBLED while VLC (waits longer) played fine. A genuinely scrambled channel stays scrambled throughout, so the trailing window still flags it.
	preflightRelayTimeout   = 4 * time.Second
)

// Scramble detection thresholds — deliberately conservative so a clear channel
// (or a tiny sample) can never be falsely classified as encrypted.
const (
	tsScrambleMinPackets  = 24  // require a meaningful sample before classifying
	tsScrambleThreshold   = 0.5 // majority of aligned packets must carry the scrambling bits
	tsScrambleTailPackets = 48  // relay streams are classified on this trailing window, past the brief initial interval
)

var ffmpegURLPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^'"\s]+`)
var preflightRetryDelay = 750 * time.Millisecond

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
	result, err := a.runPreflightWithRetry(ctx, sessionID, streamURL, preflight)
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

	if result.Normalized().Reason == ports.PreflightReasonScrambled {
		a.Logger.Warn().
			Str("event", "preflight_scrambled").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
			Str("resolved_url", resolvedLogURL).
			Int("preflight_bytes", result.Bytes).
			Int64("preflight_latency_ms", result.LatencyMs).
			Int("resolved_port", result.ResolvedPort).
			Msg("stream is scrambled (encrypted, control word missing) — receiver could not descramble it; not falling back, the same service stays scrambled on every port")
		return "", ports.NewPreflightError(ports.PreflightResult{
			Reason:       ports.PreflightReasonScrambled,
			Detail:       "scrambled",
			HTTPStatus:   result.HTTPStatus,
			Bytes:        result.Bytes,
			LatencyMs:    result.LatencyMs,
			ResolvedPort: result.ResolvedPort,
		})
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

		fallbackResult, fallbackErr := a.runPreflightWithRetry(ctx, sessionID, fallbackURL, preflight)
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

			origRes, origErr := a.runPreflightWithRetry(ctx, sessionID, origURL, preflight)
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

	relay := isStreamRelayURL(rawURL)
	timeout := a.PreflightTimeout
	if timeout <= 0 {
		timeout = preflightTimeout
	}
	scanBytes := preflightScanBytes
	if relay {
		// A relay source can show the scrambling bits set briefly at the start of a
		// fresh stream; read further and allow more time so the trailing window lands
		// past that initial interval.
		scanBytes = preflightRelayScanBytes
		if timeout < preflightRelayTimeout {
			timeout = preflightRelayTimeout
		}
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

	buf := make([]byte, scanBytes)
	n, err := io.ReadFull(io.LimitReader(resp.Body, int64(scanBytes)), buf)
	// ReadFull tries to fill the entire scan window; if the body ends
	// earlier that is fine — we classify on whatever packets are present.
	// The alternative (ReadAtLeast with a minimum) returns after only a few
	// packets and leaves scramble detection dead for most streaming sources.
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	// If we got at least the minimum sample, classify what we have even
	// when the full scan window wasn't filled (e.g. timeout on a slow or
	// bursty live source).
	// For relay streams we must have read far enough to clear the
	// descrambler lock window (~2000 packets), otherwise the trailing
	// 48-packet sample still lands inside the lock and a healthy stream
	// is falsely flagged R_UPSTREAM_SCRAMBLED.
	minRequiredBytes := preflightMinBytes
	if relay {
		minRequiredBytes = 188 * 2500 // ~2000 pkt lock + 48 pkt trailing window + margin
	}
	if n >= minRequiredBytes && err != nil {
		err = nil
	}
	// If we got fewer bytes than the minimum required for sync/scramble
	// classification, treat it as a short read rather than a sync miss so
	// the caller can distinguish a truncated body from a valid stream that
	// happens to lack the sync byte.
	if n < preflightMinBytes && err == nil {
		err = io.ErrUnexpectedEOF
	}
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

	if fraction, packets := scrambleFractionForSource(buf[:n], relay); packets >= tsScrambleMinPackets && fraction >= tsScrambleThreshold {
		result.Detail = "scrambled"
		result.Reason = ports.PreflightReasonScrambled
		a.Logger.Warn().
			Str("url", sanitizeURLForLog(rawURL)).
			Int("scanned_packets", packets).
			Str("scrambled_fraction", fmt.Sprintf("%.2f", fraction)).
			Int("resolved_port", result.ResolvedPort).
			Msg("preflight sample is scrambled (encrypted payload, transport_scrambling_control set)")
		return result, fmt.Errorf("preflight stream is scrambled (encrypted, not descrambled)")
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

func (a *LocalAdapter) runPreflightWithRetry(ctx context.Context, sessionID, rawURL string, preflight preflightFn) (ports.PreflightResult, error) {
	result, err := preflight(ctx, rawURL)
	result = normalizeAdapterPreflightResult(result, err)

	for attempt := 2; shouldRetryTSPreflight(result); attempt++ {
		maxTries := maxPreflightTries(result)
		if attempt > maxTries {
			break
		}
		if waitErr := sleepWithContext(ctx, preflightRetryDelay); waitErr != nil {
			break
		}
		a.Logger.Warn().
			Str("event", "input_preflight_retry").
			Str("sessionId", sessionID).
			Str("url", sanitizeURLForLog(rawURL)).
			Int("attempt", attempt).
			Int("max_attempts", maxTries).
			Int("preflight_bytes", result.Bytes).
			Str("preflight_reason", preflightReason(result)).
			Str("preflight_detail", result.FailureDetail()).
			Int("http_status", result.HTTPStatus).
			Int("resolved_port", result.ResolvedPort).
			Msg("retrying transient stream input preflight")

		result, err = preflight(ctx, rawURL)
		result = normalizeAdapterPreflightResult(result, err)
		if err == nil && result.OK {
			return result, nil
		}
	}

	return result, err
}

func maxPreflightTries(result ports.PreflightResult) int {
	normalized := result.Normalized()
	if normalized.ResolvedPort == 8001 || normalized.ResolvedPort == 8002 {
		if normalized.HTTPStatus == 0 || normalized.HTTPStatus == http.StatusOK {
			if normalized.Reason == ports.PreflightReasonCorruptInput && normalized.Bytes < preflightMinBytes {
				return preflightDirectWarmupTries
			}
		}
	}
	return preflightMaxTries
}

func shouldRetryTSPreflight(result ports.PreflightResult) bool {
	normalized := result.Normalized()
	if normalized.OK {
		return false
	}

	switch normalized.ResolvedPort {
	case 17999, 8001, 8002:
	default:
		return false
	}

	if normalized.HTTPStatus != 0 && normalized.HTTPStatus != http.StatusOK {
		return false
	}

	switch normalized.Reason {
	case ports.PreflightReasonTimeout:
		return true
	case ports.PreflightReasonScrambled:
		// Retry within the bounded window: a freshly tuned relay can forward a
		// brief scrambled prefix before the control word locks. If it is still
		// scrambled after the retries it is genuinely undescramblable.
		return true
	case ports.PreflightReasonCorruptInput, ports.PreflightReasonInvalidTS:
		return normalized.Bytes < preflightMinBytes
	default:
		return false
	}
}

func hasTSSync(buf []byte) bool {
	if len(buf) < preflightMinBytes {
		return false
	}
	return buf[0] == 0x47 && buf[188] == 0x47 && buf[376] == 0x47
}

// tsScrambledFraction reports the fraction of 188-byte MPEG-TS packets in buf
// whose transport_scrambling_control bits (the top two bits of byte 3) are set,
// i.e. the payload is encrypted and was not descrambled by the receiver. buf
// must be packet-aligned at offset 0 — callers gate this on hasTSSync. Scanning
// stops at the first packet that loses 0x47 alignment so a mid-buffer glitch
// cannot inflate the count. Returns (0, 0) when no aligned packet is found.
func tsScrambledFraction(buf []byte) (fraction float64, packets int) {
	const pktLen = 188
	scrambled, total := 0, 0
	for off := 0; off+pktLen <= len(buf); off += pktLen {
		if buf[off] != 0x47 {
			break // lost packet alignment; only trust the aligned prefix
		}
		total++
		if buf[off+3]&0xC0 != 0 { // transport_scrambling_control != 00
			scrambled++
		}
	}
	if total == 0 {
		return 0, 0
	}
	return float64(scrambled) / float64(total), total
}

// scrambleFractionForSource picks the scramble-classification window by source type.
// A direct source carries clear packets from the first one, so the whole sample is
// scanned. A stream-relay source can show the transport_scrambling_control bits set
// for a brief interval at the start of a freshly-started stream that clears once it
// stabilizes, so it is classified on the TRAILING window — past that initial interval
// — while a stream that stays flagged throughout (its tail is flagged too) is still
// classified as scrambled.
func scrambleFractionForSource(buf []byte, relay bool) (fraction float64, packets int) {
	if relay {
		return tsScrambledTailFraction(buf, tsScrambleTailPackets)
	}
	return tsScrambledFraction(buf)
}

// tsScrambledTailFraction reports the scrambled fraction over the last tailPackets
// aligned 188-byte MPEG-TS packets in buf (or all of them if fewer). Like
// tsScrambledFraction it scans only the aligned prefix (stops at the first packet
// that loses 0x47 alignment); buf must be packet-aligned at offset 0 — callers gate
// on hasTSSync. Returns (0, 0) when no aligned packet is found.
func tsScrambledTailFraction(buf []byte, tailPackets int) (fraction float64, packets int) {
	const pktLen = 188
	flags := make([]bool, 0, len(buf)/pktLen+1)
	for off := 0; off+pktLen <= len(buf); off += pktLen {
		if buf[off] != 0x47 {
			break // lost alignment; only trust the aligned prefix
		}
		flags = append(flags, buf[off+3]&0xC0 != 0)
	}
	if len(flags) == 0 {
		return 0, 0
	}
	start := 0
	if len(flags) > tailPackets {
		start = len(flags) - tailPackets
	}
	tail := flags[start:]
	scrambled := 0
	for _, s := range tail {
		if s {
			scrambled++
		}
	}
	return float64(scrambled) / float64(len(tail)), len(tail)
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

func sleepWithContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
