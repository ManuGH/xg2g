package ffmpeg

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTimeoutTestAdapter(t *testing.T, startTimeout time.Duration) *LocalAdapter {
	t.Helper()
	return NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 2,
		startTimeout, 30*time.Second, "",
	)
}

func cpuTranscodeSpec() ports.StreamSpec {
	return ports.StreamSpec{
		Source: ports.StreamSource{Type: ports.SourceTuner},
		Profile: ports.ProfileSpec{
			Name:           "safari",
			TranscodeVideo: true,
			VideoCodec:     "libx264",
			Container:      "fmp4",
		},
	}
}

// TestStartTimeoutForSpec_ClampsToCallerReadyDeadline is the fix for a watchdog
// that could only ever fire after someone else had already failed the session.
//
// The profile allowance for a CPU transcode is 30s here (60s for HQ50, capped at
// 120s), while a live caller stops waiting when its startup budget runs out. If
// the allowance outlives the caller's deadline the watchdog is unreachable code,
// and with it the specific diagnosis it produces — DetailTranscodeStalled, which
// the recovery policy treats as recoverable by a lighter profile.
func TestStartTimeoutForSpec_ClampsToCallerReadyDeadline(t *testing.T) {
	adapter := startTimeoutTestAdapter(t, 15*time.Second)
	spec := cpuTranscodeSpec()

	// Unclamped, this profile gets the extended CPU-transcode allowance.
	require.Equal(t, 30*time.Second, adapter.startTimeoutForSpec(spec))

	// With a caller that gives up in 10s, the watchdog must fire first.
	spec.ReadyDeadline = time.Now().Add(10 * time.Second)
	clamped := adapter.startTimeoutForSpec(spec)
	assert.Less(t, clamped, 10*time.Second, "the watchdog must fire before the caller's deadline")
	assert.Greater(t, clamped, 7*time.Second, "and not so early that a slow-but-healthy start is killed")
}

// TestStartTimeoutForSpec_KeepsProfileAllowanceWhenItFitsFirst guards the other
// direction: a caller with plenty of budget must not extend a short profile
// allowance, or the watchdog stops being a watchdog.
func TestStartTimeoutForSpec_KeepsProfileAllowanceWhenItFitsFirst(t *testing.T) {
	adapter := startTimeoutTestAdapter(t, 15*time.Second)
	spec := cpuTranscodeSpec()
	spec.ReadyDeadline = time.Now().Add(5 * time.Minute)

	assert.Equal(t, 30*time.Second, adapter.startTimeoutForSpec(spec))
}

// TestStartTimeoutForSpec_NeverGoesNonPositive pins the contract boundary: zero
// and below mean "fire on the first tick" to the watchdog, which is a different
// thing than "fire early". A caller deadline that has already passed must not
// silently change what a configured timeout means.
func TestStartTimeoutForSpec_NeverGoesNonPositive(t *testing.T) {
	adapter := startTimeoutTestAdapter(t, 15*time.Second)
	spec := cpuTranscodeSpec()
	spec.ReadyDeadline = time.Now().Add(-time.Minute)

	assert.Equal(t, minWatchdogStartTimeout, adapter.startTimeoutForSpec(spec))
}

// TestStartTimeoutForSpec_UnconfiguredTimeoutIsUntouched keeps the "no start
// timeout" configuration meaning exactly what it meant before the clamp existed.
func TestStartTimeoutForSpec_UnconfiguredTimeoutIsUntouched(t *testing.T) {
	adapter := startTimeoutTestAdapter(t, 0)
	spec := cpuTranscodeSpec()
	spec.ReadyDeadline = time.Now().Add(10 * time.Second)

	assert.LessOrEqual(t, adapter.startTimeoutForSpec(spec), time.Duration(0))
}

// tsSourceServer serves packet-aligned unscrambled MPEG-TS so preflight accepts
// the input without a receiver.
func tsSourceServer(t *testing.T) *httptest.Server {
	t.Helper()
	packet := make([]byte, 188)
	packet[0] = 0x47
	payload := make([]byte, 0, 188*512)
	for i := 0; i < 512; i++ {
		payload = append(payload, packet...)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sleepingFFmpeg writes a stand-in binary that ignores its arguments and stays
// alive, so Start's process lifecycle can be observed without a real encoder.
func sleepingFFmpeg(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-ffmpeg")
	script := "#!/bin/sh\nexec sleep 30\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700)) //nolint:gosec // test fixture must be executable
	return path
}

// TestStart_PrepareDeadlineDoesNotReachTheProcess is the safety property of
// bounding the pre-spawn work.
//
// Tuning, URL resolution, preflight and the plan probes now run under the
// caller's PrepareDeadline so they cannot consume the encode time behind them.
// The process must NOT inherit that deadline — it would kill the very stream the
// preparation was for, seconds after it started.
func TestStart_PrepareDeadlineDoesNotReachTheProcess(t *testing.T) {
	if _, err := net.LookupPort("tcp", "0"); err != nil {
		t.Skip("network stack unavailable")
	}
	srv := tsSourceServer(t)

	adapter := NewLocalAdapter(
		sleepingFFmpeg(t), "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 2*time.Second, false, 2*time.Second, 2,
		30*time.Second, 30*time.Second, "",
	)

	const prepareWindow = 700 * time.Millisecond
	spec := ports.StreamSpec{
		SessionID:       "sess-prepare-deadline",
		Mode:            ports.ModeLive,
		Format:          ports.FormatHLS,
		Source:          ports.StreamSource{Type: ports.SourceURL, ID: srv.URL},
		Profile:         ports.ProfileSpec{Name: "default", Container: "mpegts"},
		PrepareDeadline: time.Now().Add(prepareWindow),
		ReadyDeadline:   time.Now().Add(30 * time.Second),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := adapter.Start(ctx, spec)
	require.NoError(t, err)
	require.NotEmpty(t, handle)
	t.Cleanup(func() { _ = adapter.Stop(context.Background(), handle) })

	require.True(t, adapter.Health(ctx, handle).Healthy, "precondition: the process starts healthy")

	// Well past the prepare deadline: if it had reached the process, the process
	// would be gone by now.
	time.Sleep(prepareWindow + 800*time.Millisecond)
	assert.True(t, adapter.Health(ctx, handle).Healthy,
		"the prepare deadline must bound preparation only, never the pipeline it prepared")
}
