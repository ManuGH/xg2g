package mediafacts

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

func capture(t *testing.T, name string) []byte {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(self), "..", "..", "..", "..", "testdata", "segments")
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Skipf("capture %s unavailable: %v", name, err)
	}
	return b
}

// The deadline is a number with a derivation, and a derivation stops being true
// when the thing it was derived from changes. This keeps the two attached: if the
// parser ever slows down enough that a worst-case chunk eats a meaningful part of
// DefaultIngestDeadline, the headroom the number was chosen for is gone and this
// says so before a stalled core starts looking like a slow one.
func TestIngestStaysFarInsideItsDeadline(t *testing.T) {
	// A wall-clock guard measures the parser. Under coverage it measures the
	// counters coverage inserted into the parser, which is a different thing and
	// not one DefaultIngestDeadline was derived from - atomic counters on every
	// block turn the 4 MiB chunk from ~15ms into ~54ms without the parser having
	// changed at all.
	//
	// Skipping here is not a loosened gate. The property is still checked on every
	// ordinary run and on the race job; what is dropped is a timing claim from the
	// one run that cannot make it honestly. The alternative - raising the limit
	// until instrumented timings fit under it - would fit the contract to the
	// measuring instrument and leave the real headroom unguarded.
	if testing.CoverMode() != "" {
		t.Skip("wall-clock ingest headroom is not meaningful under coverage instrumentation")
	}

	for _, name := range []string{"test_hevc_stream.ts", "verify_aac.ts", "verify_seg.ts"} {
		data := capture(t, name)
		data = data[:len(data)/TSPacketSize*TSPacketSize]

		for _, chunk := range []int{64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024} {
			chunk = chunk / TSPacketSize * TSPacketSize
			if len(data) < chunk {
				continue
			}
			c := NewGoCore(1)
			var samples []time.Duration
			for off := 0; off+chunk <= len(data); off += chunk {
				start := time.Now()
				if _, err := c.Ingest(context.Background(), int64(off), data[off:off+chunk]); err != nil {
					t.Fatalf("ingest: %v", err)
				}
				samples = append(samples, time.Since(start))
			}
			if len(samples) == 0 {
				continue
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			p50 := samples[len(samples)*50/100]
			p99 := samples[len(samples)*99/100]
			max := samples[len(samples)-1]
			t.Logf("%-22s chunk=%4dKiB n=%3d  p50=%-10v p99=%-10v max=%v",
				name, chunk/1024, len(samples), p50, p99, max)

			// A tenth of the deadline. Chosen so the assertion fires on a real
			// regression rather than on a loaded machine: the measured worst case
			// is around 3% of it.
			if budget := DefaultIngestDeadline / 10; max > budget {
				t.Errorf("%s at %d KiB took %v, more than %v - the deadline no longer has the headroom it was derived from",
					name, chunk/1024, max, budget)
			}
		}
	}
}
