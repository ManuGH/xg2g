package ffmpeg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Existing tests...
func TestRunner_Lifecycle(t *testing.T) {
	// ... (content same as original file, omitted for brevity)
	// Mock Enigma2 Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/web/stream.m3u" {
			// Return a dummy playlist
			_, _ = w.Write([]byte("#EXTM3U\nhttp://127.0.0.1:8001/stream\n")) // Dummy stream URL
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	// Use "sleep_test" profile to invoke "sleep 10"
	runner := NewRunner("run_test", "/tmp/hls", enigma2.NewClient(ts.URL, time.Second), time.Second) // binPath override in Start

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Start
	err := runner.Start(ctx, "test1", "1:0:1", model.ProfileSpec{Name: "sleep_test"}, 0)
	require.NoError(t, err)

	// 2. Stop (Signal)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = runner.Stop(ctx)
	}()

	// 3. Wait
	status, err := runner.Wait(ctx)

	// Sleep was SIGTERMed.
	// Exit code should be non-zero (usually 143 or similar for SIGTERM)
	// Error handles exit error
	assert.Error(t, err)
	assert.NotEqual(t, 0, status.Code)
	// Reason: "error" (since ctx wasn't cancelled yet, just Run stopped)
	// Or did we check cmd.ProcessState?
	// The runner logic sets reason="error" if err != nil and ctx not Done.
	assert.Equal(t, "error", status.Reason)
}

func TestRunner_ContextCancel(t *testing.T) {
	// ... (content same as original file, omitted for brevity)
	// Mock Enigma2 Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/web/stream.m3u" {
			_, _ = w.Write([]byte("#EXTM3U\nhttp://127.0.0.1:8001/stream\n"))
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	runner := NewRunner("run_test", "/tmp/hls", enigma2.NewClient(ts.URL, time.Second), time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := runner.Start(ctx, "test2", "1:0:1", model.ProfileSpec{Name: "sleep_test"}, 0)
	require.NoError(t, err)

	status, err := runner.Wait(ctx)

	// Should be killed by context
	assert.Error(t, err)
	assert.Equal(t, "ctx_cancel", status.Reason)
}

func TestRunner_StopKillsAfterTimeout(t *testing.T) {
	// ... (content same as original file, omitted for brevity)
	if runtime.GOOS == "windows" {
		t.Skip("ignore_term_test uses sh, unsupported on windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}

	killTimeout := 200 * time.Millisecond
	runner := NewRunner("run_test", "/tmp/hls", enigma2.NewClient("http://127.0.0.1", time.Second), killTimeout)

	ctx := context.Background()
	err := runner.Start(ctx, "test3", "/dev/null", model.ProfileSpec{Name: "ignore_term_test"}, 0)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	require.NoError(t, runner.Stop(ctx))
	status, err := runner.Wait(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Equal(t, "error", status.Reason)
	if elapsed < killTimeout {
		t.Fatalf("expected kill after timeout, elapsed %s < %s", elapsed, killTimeout)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected stop within 2s, got %s", elapsed)
	}
}

func TestRunner_RestartLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh, unsupported on windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}

	runner := NewRunner("run_test", "/tmp/hls", enigma2.NewClient("http://127.0.0.1", time.Second), time.Second)

	ctx := context.Background()
	// Use restart_test profile which triggers "non-existing PPS" and exit 1
	err := runner.Start(ctx, "restart_verify", "/dev/null", model.ProfileSpec{Name: "restart_test"}, 0)
	require.NoError(t, err)

	// Wait for completion (it should retry 3 times then fail)
	// Each retry has 1s backoff + overhead. Minimal ~2-3s.
	start := time.Now()
	status, err := runner.Wait(ctx)
	elapsed := time.Since(start)

	// Should fail eventually
	require.Error(t, err)
	assert.Equal(t, 1, status.Code)

	// Verify it retried (took longer than single run)
	// 3 attempts with 1s backoff (attempts 1, 2) = ~2s delays minimum.
	if elapsed < 2*time.Second {
		t.Fatalf("runner finished too fast (%s), expected retries (backoff > 2s)", elapsed)
	}

	// We can't validly check r.LastLogLines for "restarting pipeline" because that message
	// is logged by the application logger (zerolog), not written to the ffmpeg stderr ring buffer.
	// The elapsed time check > 2s confirms that the backoff loop executed.
}
