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
	"github.com/rs/zerolog"
)

const (
	preflightMinBytes          = 188 * 3  // sync floor: never raised, keeps preflight latency/timeout behaviour unchanged
	preflightScanBytes         = 188 * 48 // best-effort upper bound scanned for scrambling (9024B) on a source that is not lock-prone
	preflightTimeout           = 2 * time.Second
	preflightMaxTries          = 3
	preflightScrambleTries     = 5
	preflightDirectWarmupTries = 8

	// A lock-prone source — the stream relay on 17999 and the receiver's own tuner
	// ports (see isTunerPort) — emits MPEG-TS packets with the
	// transport_scrambling_control bits set until the control word locks, and only
	// clears afterwards. Sampling just the first 48 packets can land entirely
	// inside that lock-in and misclassify the whole stream, and each retry opens a
	// fresh connection that lands in it again. These sources are therefore read
	// far enough that the classified window sits past the lock-in; classifyScramble
	// picks the window.
	preflightLockProneScanBytes = 188 * 4096 // ~752KB: past observed descrambler/ECM-lock (~367KB / ~2000 pkt on a real icam channel) + margin. 188*1024 landed INSIDE the lock -> false R_UPSTREAM_SCRAMBLED while VLC (waits longer) played fine. A genuinely scrambled channel stays scrambled throughout, so the classified window still flags it.
	preflightLockProneTimeout   = 4 * time.Second
)

// Scramble detection thresholds — deliberately conservative so a clear channel
// (or a tiny sample) can never be falsely classified as encrypted.
const (
	tsScrambleMinPackets = 24  // below this the sample is inconclusive, never "clear"
	tsScrambleThreshold  = 0.5 // majority of classified packets must carry the scrambling bits
	// Leading packets skipped on a lock-prone source before classifying, so the
	// verdict describes the steady state rather than the control-word lock-in.
	// Measured lock ~2000 packets (~367KB) on a real icam channel; +500 margin.
	// Capped at half the sample (see classifyScramble), so on a production-sized
	// read the cap is what binds and the trailing half is classified.
	tsScrambleLockPrefixPackets = 2500
	// Smallest classification window with margin against PSI/EPG bursts, measured
	// on two 13MB captures of real encrypted services (86.7% and 92.9% scrambled
	// overall). Minimum scrambled fraction across every window of that size:
	//
	//	window    capture A   capture B      windows reading below the threshold
	//	    48       0.0417      0.0208      1344 / 1087
	//	   256       0.2148      0.1758      1232 / 1084
	//	  1024       0.6348      0.4121         0 /  427   <- still flips
	//	  2048       0.6406      0.6758         0 /    0
	//
	// Anything smaller can be flipped by a clear burst; 2048 holds a ~0.14 margin.
	tsScrambleMinConfidentWindow = 2048
	// Below this fraction a sample is clear no matter how small the window: a
	// stream that is not encrypted carries transport_scrambling_control 00 on
	// every packet, so its fraction is exactly 0. The lowest fraction any window
	// of an encrypted capture reached was 0.0208 (48 packets) — an order of
	// magnitude above this — so no burst can reach down here.
	tsScrambleStrictClearFraction = 0.002

	// Consecutive clear verdicts required to overturn earlier scrambled evidence
	// within one preflight round (see runPreflightWithRetry).
	preflightClearCorroborations = 2
	// Extra reads granted beyond the base try budget to corroborate a contested
	// clear verdict, so corroboration cannot be starved by the budget.
	preflightCorroborationTries = 1
)

var ffmpegURLPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^'"\s]+`)
var preflightRetryDelay = 750 * time.Millisecond

type preflightFn func(context.Context, string) (ports.PreflightResult, error)

// scrambleObserver returns the adapter's descrambling-outcome memory, built on
// first use so no constructor signature has to change.
func (a *LocalAdapter) scrambleObserver() *ports.ScrambleObserver {
	a.scrambleObserverOnce.Do(func() {
		a.scrambleObs = ports.NewScrambleObserver(0, 0)
	})
	return a.scrambleObs
}

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

	// Record what descrambling did for this service, so a scrambled upstream can
	// later be attributed to the service or to the receiver as a whole.
	scrambled := result.Normalized().Reason == ports.PreflightReasonScrambled
	if err == nil && result.OK {
		a.scrambleObserver().Observe(serviceRef, false, time.Now())
		return streamURL, nil
	}
	if scrambled {
		a.scrambleObserver().Observe(serviceRef, true, time.Now())
		result.Scramble.Scope = a.scrambleObserver().Scope(time.Now())
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

	if scrambled {
		scope := result.Scramble.Scope
		evt := a.Logger.Warn().
			Str("event", "preflight_scrambled").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
			Str("resolved_url", resolvedLogURL).
			Str("scramble_scope", string(scope)).
			Int("preflight_bytes", result.Bytes).
			Int64("preflight_latency_ms", result.LatencyMs).
			Int("resolved_port", result.ResolvedPort)
		if scope == ports.ScrambleScopeReceiver {
			// Several distinct services have failed and none has descrambled: this
			// is the receiver, not the service, and no retry or profile change
			// touches it. Said plainly so it is actionable at a glance.
			evt.Str("event", "descrambler_down").
				Msg("receiver is not descrambling ANY service — check the CAM or softcam on the receiver; retrying or switching channels will not help")
			metrics.RecordDescramblerDown()
		} else {
			evt.Msg("stream is scrambled (encrypted, control word missing) — receiver could not descramble it; not falling back, the same service stays scrambled on every port")
		}
		return "", ports.NewPreflightError(ports.PreflightResult{
			Reason:       ports.PreflightReasonScrambled,
			Detail:       "scrambled",
			HTTPStatus:   result.HTTPStatus,
			Bytes:        result.Bytes,
			LatencyMs:    result.LatencyMs,
			ResolvedPort: result.ResolvedPort,
			Scramble:     result.Scramble,
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

// preflightLockProneLeadProbeBytes is the leading sample read for the relay clear-lead
// fast path: enough packets (>> tsScrambleMinPackets) to tell an FTA/clear head from
// a scrambled one, yet tiny vs the full descrambler-lock scan window.
const preflightLockProneLeadProbeBytes = 188 * 128 // ~24KB / 128 packets

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
	isTunerOrRelay := relay || isTunerPort(result.ResolvedPort)
	timeout := a.PreflightTimeout
	if timeout <= 0 {
		timeout = preflightTimeout
	}
	scanBytes := preflightScanBytes
	if isTunerOrRelay {
		// Tuner (8000/8001/8002) and relay (17999) sources show the scrambling bits
		// set at the start of a fresh stream until the control word locks; read far
		// enough that the classified window lands past that lock-in.
		scanBytes = preflightLockProneScanBytes
		if timeout < preflightLockProneTimeout {
			timeout = preflightLockProneTimeout
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
	bodyReader := io.LimitReader(resp.Body, int64(scanBytes))

	// Clear-lead fast path (stream-relay only): a channel that needs descrambling
	// carries the transport_scrambling_control bits from its first packet until the
	// ECM locks, so if the LEADING window is already clear the source is FTA/passthrough
	// and we accept it without draining the full ~752KB descrambler-lock window, which
	// cuts ~4s off startup for clear channels. A scrambled head (descrambling in
	// progress or genuinely scrambled) fails this check and falls through to the
	// unchanged trailing-window classification below.
	n := 0
	if isTunerOrRelay {
		lead := preflightLockProneLeadProbeBytes
		if lead > scanBytes {
			lead = scanBytes
		}
		nLead, leadErr := io.ReadFull(bodyReader, buf[:lead])
		n = nLead
		if leadErr == io.EOF || leadErr == io.ErrUnexpectedEOF {
			leadErr = nil
		}
		if leadErr != nil {
			result.Bytes = nLead
			return result, leadErr
		}
		if nLead >= preflightMinBytes && hasTSSync(buf[:nLead]) {
			// Classified on the lead window itself (lockProne=false): the whole
			// point is to read the head, which on a descrambling source is the
			// lock-in and therefore not clear.
			if lead := classifyScramble(buf[:nLead], false); lead.Verdict == ports.ScrambleVerdictClear {
				latency := time.Since(start)
				a.Logger.Info().
					Str("url", sanitizeURLForLog(rawURL)).
					Int("bytes", nLead).
					Dur("latency", latency).
					Int("resolved_port", result.ResolvedPort).
					Str("preflight_fast_path", "clear_lead").
					Int("scramble_classified_packets", lead.Classified).
					Str("scramble_fraction", fmt.Sprintf("%.4f", lead.Fraction)).
					Msg("preflight clear-lead fast path (FTA/passthrough relay source)")
				result = ports.NewSuccessfulPreflightResult(nLead, latency.Milliseconds(), result.ResolvedPort)
				result.Scramble = lead
				return result, nil
			}
		}
	}

	m, err := io.ReadFull(bodyReader, buf[n:])
	n += m
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
	if isTunerOrRelay {
		// Enough packets that the classified window still clears the lock prefix.
		minRequiredBytes = 188 * (tsScrambleLockPrefixPackets + tsScrambleMinPackets)
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

	scramble := classifyScramble(buf[:n], isTunerOrRelay)
	result.Scramble = scramble
	scrambleLog := func(e *zerolog.Event) *zerolog.Event {
		return e.
			Str("url", sanitizeURLForLog(rawURL)).
			Str("scramble_verdict", string(scramble.Verdict)).
			Str("scramble_window", scramble.Window).
			Int("scramble_aligned_packets", scramble.Aligned).
			Int("scramble_classified_packets", scramble.Classified).
			Str("scramble_fraction", fmt.Sprintf("%.4f", scramble.Fraction)).
			Int("resolved_port", result.ResolvedPort)
	}

	switch scramble.Verdict {
	case ports.ScrambleVerdictScrambled:
		result.Detail = "scrambled"
		result.Reason = ports.PreflightReasonScrambled
		scrambleLog(a.Logger.Warn()).
			Msg("preflight sample is scrambled (encrypted payload, transport_scrambling_control set)")
		return result, fmt.Errorf("preflight stream is scrambled (encrypted, not descrambled)")
	case ports.ScrambleVerdictInconclusive:
		// Not evidence of a clear stream: too few packets survived window
		// selection to judge. The sample passes (absence of proof is not proof
		// of encryption) but carries the verdict, so a later scrambled sample in
		// the same round is not overturned by it.
		scrambleLog(a.Logger.Warn()).
			Msg("preflight scramble classification inconclusive — sample too small to judge, not treated as clear")
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

// runPreflightWithRetry samples the source up to a bounded number of times and
// decides on the accumulated evidence, not on whichever sample happened to be last.
//
// The distinction matters because each sample is a short read of a live stream, so
// one sample can be unrepresentative in either direction. Two rules follow:
//
//   - A scrambled verdict is remembered for the whole round. Once the source has
//     been observed encrypted, an inconclusive sample cannot wave it through — an
//     unjudgeable sample is not counter-evidence.
//   - A clear verdict that contradicts earlier scrambled evidence is accepted only
//     once a second consecutive clear read confirms it. On a tuner source the later
//     sample is genuinely the more informative one (the control word has had more
//     time to lock), so a corroborated clear must still win; a lone one must not.
//     Corroboration gets its own extra read so the try budget cannot starve it.
//
// If the round ends without an accepted clear, the remembered scrambled result is
// returned, so the caller reports the encryption rather than the last sample's
// incidental outcome.
func (a *LocalAdapter) runPreflightWithRetry(ctx context.Context, sessionID, rawURL string, preflight preflightFn) (ports.PreflightResult, error) {
	var (
		result          ports.PreflightResult
		err             error
		scrambledResult ports.PreflightResult
		scrambledErr    error
		sawScrambled    bool
		clearRun        int
	)

	for attempt := 1; ; attempt++ {
		result, err = preflight(ctx, rawURL)
		result = normalizeAdapterPreflightResult(result, err)

		ok := err == nil && result.OK
		switch {
		case ok && !sawScrambled:
			return result, nil
		case ok && result.Scramble.Verdict == ports.ScrambleVerdictClear:
			clearRun++
			if clearRun >= preflightClearCorroborations {
				a.Logger.Info().
					Str("event", "input_preflight_clear_corroborated").
					Str("sessionId", sessionID).
					Str("url", sanitizeURLForLog(rawURL)).
					Int("clear_reads", clearRun).
					Str("scramble_fraction", fmt.Sprintf("%.4f", result.Scramble.Fraction)).
					Int("scramble_classified_packets", result.Scramble.Classified).
					Msg("clear preflight verdict corroborated after an earlier scrambled sample; control word locked")
				return result, nil
			}
		default:
			clearRun = 0
			if result.Normalized().Reason == ports.PreflightReasonScrambled {
				sawScrambled, scrambledResult, scrambledErr = true, result, err
			}
		}

		// An OK sample that reached here is contested: it did not satisfy the
		// accept rules above and is only worth another read for corroboration.
		corroborating := ok
		if !corroborating && !shouldRetryTSPreflight(result) {
			break
		}
		budget := maxPreflightTries(result)
		if corroborating {
			budget += preflightCorroborationTries
		}
		if attempt >= budget {
			break
		}
		if waitErr := sleepWithContext(ctx, preflightRetryDelay); waitErr != nil {
			break
		}
		a.Logger.Warn().
			Str("event", "input_preflight_retry").
			Str("sessionId", sessionID).
			Str("url", sanitizeURLForLog(rawURL)).
			Int("attempt", attempt+1).
			Int("max_attempts", budget).
			Bool("corroborating_clear", corroborating).
			Int("preflight_bytes", result.Bytes).
			Str("preflight_reason", preflightReason(result)).
			Str("preflight_detail", result.FailureDetail()).
			Str("scramble_verdict", string(result.Scramble.Verdict)).
			Int("http_status", result.HTTPStatus).
			Int("resolved_port", result.ResolvedPort).
			Msg("retrying transient stream input preflight")
	}

	if sawScrambled {
		a.Logger.Warn().
			Str("event", "input_preflight_scrambled_stands").
			Str("sessionId", sessionID).
			Str("url", sanitizeURLForLog(rawURL)).
			Str("final_scramble_verdict", string(result.Scramble.Verdict)).
			Msg("preflight round ends scrambled: no corroborated clear verdict overturned it")
		return scrambledResult, scrambledErr
	}
	return result, err
}

func maxPreflightTries(result ports.PreflightResult) int {
	normalized := result.Normalized()
	if isTunerPort(normalized.ResolvedPort) || normalized.ResolvedPort == 17999 {
		if normalized.HTTPStatus == 0 || normalized.HTTPStatus == http.StatusOK || isTransientEnigma2HTTPStatus(normalized.HTTPStatus) {
			if (normalized.Reason == ports.PreflightReasonCorruptInput && normalized.Bytes < preflightMinBytes) ||
				isTransientEnigma2HTTPStatus(normalized.HTTPStatus) {
				return preflightDirectWarmupTries
			}
			if normalized.Reason == ports.PreflightReasonScrambled {
				return preflightScrambleTries
			}
		}
	}
	return preflightMaxTries
}

func isTransientEnigma2HTTPStatus(status int) bool {
	switch status {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
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
		return isTransientEnigma2HTTPStatus(normalized.HTTPStatus)
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

// tsScrambleFlags reports, per 188-byte MPEG-TS packet in buf, whether the
// transport_scrambling_control bits (the top two bits of byte 3) are set, i.e.
// the payload is encrypted and was not descrambled by the receiver. buf must be
// packet-aligned at offset 0 — callers gate this on hasTSSync. Scanning stops at
// the first packet that loses 0x47 alignment so a mid-buffer glitch cannot
// inflate the count.
func tsScrambleFlags(buf []byte) []bool {
	const pktLen = 188
	flags := make([]bool, 0, len(buf)/pktLen+1)
	for off := 0; off+pktLen <= len(buf); off += pktLen {
		if buf[off] != 0x47 {
			break // lost packet alignment; only trust the aligned prefix
		}
		flags = append(flags, buf[off+3]&0xC0 != 0)
	}
	return flags
}

// tsScrambledFraction reports the fraction of aligned packets in buf carrying the
// scrambling bits. Returns (0, 0) when no aligned packet is found.
//
//nolint:unused // test-only helper in package ffmpeg
func tsScrambledFraction(buf []byte) (fraction float64, packets int) {
	return scrambledFractionOf(tsScrambleFlags(buf))
}

func scrambledFractionOf(flags []bool) (fraction float64, packets int) {
	if len(flags) == 0 {
		return 0, 0
	}
	scrambled := 0
	for _, s := range flags {
		if s {
			scrambled++
		}
	}
	return float64(scrambled) / float64(len(flags)), len(flags)
}

// classifyScramble decides whether a TS sample is encrypted, and says so in three
// states — clear, scrambled, or inconclusive. Inconclusive is a first-class answer:
// a sample too small to judge is not evidence of a clear stream, and the caller
// must not treat it as one.
//
// Window selection. A lock-prone source (tuner or stream relay) carries the
// scrambling bits from its first packet until the control word locks, so the
// leading packets describe the lock-in, not the stream. Those are skipped and the
// verdict is taken on everything that follows — the steady state.
//
// The skipped prefix is capped at half the sample so a short read still gets a
// verdict from its more recent half, and the remainder must still hold
// tsScrambleMinPackets or the answer is inconclusive.
//
// Why the whole remainder and not a trailing peephole: EPG/PSI packets (EIT on
// PID 0x12, PAT, PMT) are never scrambled, so on a fully encrypted service they
// form clear bursts. A short window landing in one reads as clear. Replaying every
// preflight-sized read of a 13MB capture of a real encrypted service through the
// old trailing-48 window produced 1110 clear verdicts out of 65053 (1.71%) — with
// three reads per round that is roughly one round in twenty starting a transcode
// on an undecodable stream. The same replay through this window: 0 of 65053.
//
// A source whose control word locks late (past the midpoint of the sample) reads
// scrambled here where the old peephole read clear. That is deliberate: the round
// in runPreflightWithRetry re-reads, and on the second read the receiver is
// already tuned and descrambling, so two clear reads corroborate and the source is
// accepted. It costs a working channel a couple of extra reads; the inverse error
// costs 52 seconds and a wrong failure message.
func classifyScramble(buf []byte, lockProne bool) ports.ScrambleEvidence {
	flags := tsScrambleFlags(buf)
	evidence := ports.ScrambleEvidence{Aligned: len(flags), Window: "full"}

	classified := flags
	if lockProne && len(flags) > 0 {
		prefix := min(tsScrambleLockPrefixPackets, len(flags)/2)
		if prefix > 0 {
			classified = flags[prefix:]
			evidence.Window = "post_lock"
		}
	}

	fraction, packets := scrambledFractionOf(classified)
	evidence.Fraction = fraction
	evidence.Classified = packets

	// The two verdicts do not need the same amount of evidence, because a small
	// window only lies in one direction. A high fraction is trustworthy at any
	// size: a clear stream carries no scrambling bits at all, so nothing can push
	// its fraction up. A low fraction is only trustworthy on a large window, or
	// when it is low enough that no clear burst could account for it.
	switch {
	case packets < tsScrambleMinPackets:
		evidence.Verdict = ports.ScrambleVerdictInconclusive
	case fraction >= tsScrambleThreshold:
		evidence.Verdict = ports.ScrambleVerdictScrambled
	case packets >= tsScrambleMinConfidentWindow, fraction <= tsScrambleStrictClearFraction:
		evidence.Verdict = ports.ScrambleVerdictClear
	default:
		// Low fraction on a window too small to trust it — the PSI/EPG burst zone.
		evidence.Verdict = ports.ScrambleVerdictInconclusive
	}
	return evidence
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

// isTunerPort reports whether the port streams straight off a receiver tuner.
// Like the relay these sources descramble in-line, so their first packets can
// carry the scrambling bits until the control word locks.
func isTunerPort(port int) bool {
	switch port {
	case 8000, 8001, 8002:
		return true
	default:
		return false
	}
}

// isStreamRelayURL reports whether an input URL points at the relay port.
//
// DEPRECATED: the relay input path is scheduled for removal after
// config.RelayInputRemoveAfter. A receiver that serves the same content on its
// standard streaming port never produces such a URL, so on current setups this
// returns false for every input and the relay-specific branches it guards are
// inert. Do not add new callers; new input handling belongs on the default
// path. Note the port alone was never a reliable marker of a distinct upstream
// mode — a receiver can be configured to hand out the relay port for all
// services, including ones that need no relay at all.
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
