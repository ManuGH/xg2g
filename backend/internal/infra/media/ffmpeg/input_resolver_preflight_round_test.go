package ffmpeg

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

func newRoundTestAdapter(t *testing.T) *LocalAdapter {
	t.Helper()
	orig := preflightRetryDelay
	preflightRetryDelay = time.Millisecond
	t.Cleanup(func() { preflightRetryDelay = orig })
	return NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")
}

func scrambledSample() (ports.PreflightResult, error) {
	r := ports.NewPreflightResult("scrambled", http.StatusOK, 188*4096, 0, 8001)
	r.Scramble = ports.ScrambleEvidence{
		Verdict: ports.ScrambleVerdictScrambled, Fraction: 0.86, Classified: 1596, Aligned: 4096, Window: "post_lock",
	}
	return r, errors.New("preflight stream is scrambled (encrypted, not descrambled)")
}

func clearSample() (ports.PreflightResult, error) {
	r := ports.NewSuccessfulPreflightResult(188*4096, 0, 8001)
	r.Scramble = ports.ScrambleEvidence{
		Verdict: ports.ScrambleVerdictClear, Fraction: 0.02, Classified: 1596, Aligned: 4096, Window: "post_lock",
	}
	return r, nil
}

func inconclusiveSample() (ports.PreflightResult, error) {
	r := ports.NewSuccessfulPreflightResult(188*30, 0, 8001)
	r.Scramble = ports.ScrambleEvidence{
		Verdict: ports.ScrambleVerdictInconclusive, Fraction: 1.0, Classified: 12, Aligned: 24, Window: "post_lock",
	}
	return r, nil
}

// scripted returns a preflightFn that replays the given samples in order and
// repeats the last one once the script is exhausted.
func scripted(calls *int, samples ...func() (ports.PreflightResult, error)) preflightFn {
	return func(context.Context, string) (ports.PreflightResult, error) {
		*calls++
		i := *calls - 1
		if i >= len(samples) {
			i = len(samples) - 1
		}
		return samples[i]()
	}
}

// TestPreflightRound_LoneClearDoesNotOverturnScrambled is the incident, pinned.
//
// Live session 1619cee0 (2026-08-08): two samples read the source as scrambled at
// 0.85 and 0.98, the third did not, and last-sample-wins let it through. ffmpeg then
// ran 30s on an undecodable stream until the stall watchdog killed it, a pointless
// profile fallback fired, and the user got a generic "session not ready" timeout
// 52s later instead of "this channel is encrypted".
func TestPreflightRound_LoneClearDoesNotOverturnScrambled(t *testing.T) {
	adapter := newRoundTestAdapter(t)
	calls := 0
	preflight := scripted(&calls, scrambledSample, scrambledSample, clearSample, scrambledSample)

	result, err := adapter.runPreflightWithRetry(context.Background(), "sid-incident", "http://127.0.0.1:8001/ref", preflight)
	if err == nil || result.OK {
		t.Fatalf("a lone clear sample must not overturn two scrambled ones; got OK=%v err=%v", result.OK, err)
	}
	if got := result.Normalized().Reason; got != ports.PreflightReasonScrambled {
		t.Fatalf("expected the round to report scrambled, got %q", got)
	}
}

// TestPreflightRound_CorroboratedClearWins is the counterweight: a source that is
// scrambled only until the control word locks must still play. Two consecutive clear
// reads are accepted — otherwise this fix would make working channels unplayable,
// which is the worse failure.
func TestPreflightRound_CorroboratedClearWins(t *testing.T) {
	adapter := newRoundTestAdapter(t)
	calls := 0
	preflight := scripted(&calls, scrambledSample, clearSample, clearSample)

	result, err := adapter.runPreflightWithRetry(context.Background(), "sid-late-lock", "http://127.0.0.1:8001/ref", preflight)
	if err != nil || !result.OK {
		t.Fatalf("two consecutive clear reads must be accepted; got OK=%v err=%v", result.OK, err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 reads (scrambled, clear, corroborating clear), got %d", calls)
	}
}

// TestPreflightRound_InconclusiveNeverOverturnsScrambled pins the fail-open at the
// round level: a sample too small to judge is not counter-evidence, however many of
// them follow.
func TestPreflightRound_InconclusiveNeverOverturnsScrambled(t *testing.T) {
	adapter := newRoundTestAdapter(t)
	calls := 0
	preflight := scripted(&calls, scrambledSample, inconclusiveSample)

	result, err := adapter.runPreflightWithRetry(context.Background(), "sid-inconclusive", "http://127.0.0.1:8001/ref", preflight)
	if err == nil || result.OK {
		t.Fatalf("inconclusive samples must not overturn scrambled evidence; got OK=%v err=%v", result.OK, err)
	}
	if got := result.Normalized().Reason; got != ports.PreflightReasonScrambled {
		t.Fatalf("expected scrambled to stand, got %q", got)
	}
}

// TestPreflightRound_HealthySourceCostsOneRead guards the common path: with no
// scrambled evidence the first good sample is accepted, so corroboration never taxes
// a normal zap.
func TestPreflightRound_HealthySourceCostsOneRead(t *testing.T) {
	adapter := newRoundTestAdapter(t)
	calls := 0
	preflight := scripted(&calls, clearSample)

	result, err := adapter.runPreflightWithRetry(context.Background(), "sid-healthy", "http://127.0.0.1:8001/ref", preflight)
	if err != nil || !result.OK {
		t.Fatalf("expected immediate success, got OK=%v err=%v", result.OK, err)
	}
	if calls != 1 {
		t.Fatalf("a healthy source must cost exactly one read, got %d", calls)
	}
}

// TestPreflightRound_ScrambledThroughoutIsBounded keeps the failure path cheap: a
// genuinely encrypted channel must be rejected within the try budget rather than
// looping.
func TestPreflightRound_ScrambledThroughoutIsBounded(t *testing.T) {
	adapter := newRoundTestAdapter(t)
	calls := 0
	preflight := scripted(&calls, scrambledSample)

	result, err := adapter.runPreflightWithRetry(context.Background(), "sid-scrambled", "http://127.0.0.1:8001/ref", preflight)
	if err == nil || result.OK {
		t.Fatalf("expected scrambled failure, got OK=%v err=%v", result.OK, err)
	}
	if calls != preflightMaxTries {
		t.Fatalf("expected exactly %d reads for a source scrambled throughout, got %d", preflightMaxTries, calls)
	}
}

func transient500Sample() (ports.PreflightResult, error) {
	r := ports.NewPreflightResult("http_status_500", http.StatusInternalServerError, 0, 0, 8001)
	return r, errors.New("preflight http status 500")
}

func TestPreflightRound_Transient500RetriedAndSucceeds(t *testing.T) {
	adapter := newRoundTestAdapter(t)
	calls := 0
	// 1st attempt: 500 Internal Server Error (socket teardown in progress on port 8001)
	// 2nd attempt: Clear Sample -> succeeds
	preflight := scripted(&calls, transient500Sample, clearSample)

	result, err := adapter.runPreflightWithRetry(context.Background(), "sid-zap-500", "http://127.0.0.1:8001/ref", preflight)
	if err != nil || !result.OK {
		t.Fatalf("expected retry after transient 500 to succeed, got OK=%v err=%v", result.OK, err)
	}
	if calls != 2 {
		t.Fatalf("expected exactly 2 attempts (1 failed 500 + 1 success), got %d", calls)
	}
}
