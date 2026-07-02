package llhls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTrackerEndToEnd simulates the FFmpeg write pattern: a leaked init, a
// bare first segment, a playlist, and a growing current segment. The tracker
// must repair the init, index parts as they appear, and unblock a blocking
// playlist request the moment the awaited part is flushed.
func TestTrackerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ftyp := mkBox("ftyp", make([]byte, 16))
	moov := mkBox("moov", make([]byte, 32))
	leak := mkFragment(true, 64)
	styp := mkBox("styp", make([]byte, 16))

	// FFmpeg 8.x layout right after the first segment completes:
	write("init.mp4", append(append(append([]byte{}, ftyp...), moov...), leak...))
	write("seg_000000.m4s", styp)
	write("index.m3u8", []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:7",
		"#EXT-X-TARGETDURATION:2",
		"#EXT-X-MEDIA-SEQUENCE:0",
		`#EXT-X-MAP:URI="init.mp4"`,
		"#EXTINF:2.000000,",
		"seg_000000.m4s",
		"",
	}, "\n")))

	// The current segment starts with one flushed fragment.
	part0 := mkFragment(true, 80)
	write("seg_000001.m4s", append(append([]byte{}, styp...), part0...))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := NewTracker(ctx, dir, 500)

	// Non-blocking render must eventually show part 0 of msn 1.
	waitFor(t, 3*time.Second, func() bool {
		out, err := tr.AwaitAndRender(ctx, -1, -1, time.Now())
		return err == nil && strings.Contains(out, `URI="seg_000001.m4s"`) && strings.Contains(out, "BYTERANGE=")
	})

	// The init must have been repaired before parts were advertised.
	initData, err := os.ReadFile(filepath.Join(dir, "init.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if len(initData) != len(ftyp)+len(moov) {
		t.Fatalf("init not repaired: len=%d want=%d", len(initData), len(ftyp)+len(moov))
	}

	// Blocking reload: ask for part 1 (not flushed yet), append it while the
	// request waits, and expect the render to include it after unblocking.
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := tr.AwaitAndRender(ctx, 1, 1, time.Now().Add(5*time.Second))
		done <- result{out, err}
	}()

	time.Sleep(300 * time.Millisecond) // let the request block
	part1 := mkFragment(false, 40)
	segPath := filepath.Join(dir, "seg_000001.m4s")
	existing, _ := os.ReadFile(segPath)
	if err := os.WriteFile(segPath, append(existing, part1...), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("blocking render failed: %v", r.err)
		}
		if strings.Count(r.out, "#EXT-X-PART:") < 2 {
			t.Fatalf("expected both parts after unblock:\n%s", r.out)
		}
		if !strings.Contains(r.out, "INDEPENDENT=YES") {
			t.Fatalf("first part must be independent:\n%s", r.out)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("blocking reload never unblocked")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}
