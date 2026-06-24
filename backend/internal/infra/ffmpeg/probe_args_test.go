package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func probeArgsContain(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

// TestBuildProbeArgs_HTTPGetsReconnectTolerance is the load-bearing assertion:
// an HTTP capability probe must carry the live-style reconnect flags so it can
// survive the cold-tune / descramble premature-EOF. Remove the reconnect block
// and this goes red.
func TestBuildProbeArgs_HTTPGetsReconnectTolerance(t *testing.T) {
	args := buildProbeArgs("http://10.0.0.1:17999/1:0:19:8E:B:85:C00000:0:0:0:", "Connection: close\r\n", ProbeOptions{})

	for _, pair := range [][2]string{
		{"-reconnect", "1"},
		{"-reconnect_at_eof", "1"},
		{"-reconnect_streamed", "1"},
		{"-reconnect_on_network_error", "1"},
	} {
		if !probeArgsContain(args, pair[0], pair[1]) {
			t.Fatalf("http probe args missing %s %s; got: %v", pair[0], pair[1], args)
		}
	}

	// Reconnect options must precede the input URL (they are input options).
	reIdx, urlIdx := -1, -1
	for i, a := range args {
		if a == "-reconnect_at_eof" {
			reIdx = i
		}
		if strings.HasPrefix(a, "http://") {
			urlIdx = i
		}
	}
	if reIdx < 0 || urlIdx < 0 || reIdx > urlIdx {
		t.Fatalf("reconnect flags must come before the input URL: reIdx=%d urlIdx=%d args=%v", reIdx, urlIdx, args)
	}
}

// TestBuildProbeArgs_NonHTTPHasNoReconnect: local-file probes must not get the
// HTTP reconnect options.
func TestBuildProbeArgs_NonHTTPHasNoReconnect(t *testing.T) {
	args := buildProbeArgs("/var/lib/xg2g/testdata/sample.ts", "", ProbeOptions{})
	if probeArgsContain(args, "-reconnect", "1") || probeArgsContain(args, "-reconnect_at_eof", "1") {
		t.Fatalf("non-http probe must not get reconnect flags; got: %v", args)
	}
}

// TestBuildProbeArgs_PreservesProbeBudgetAndInput: the extraction must not drop
// the analyze/probe budget or the trailing input path.
func TestBuildProbeArgs_PreservesProbeBudgetAndInput(t *testing.T) {
	args := buildProbeArgs("http://x/y", "h", ProbeOptions{AnalyzeDuration: 3 * time.Second, ProbeSizeBytes: 5 << 20})
	if !probeArgsContain(args, "-analyzeduration", "3000000") {
		t.Fatalf("analyzeduration not preserved: %v", args)
	}
	if !probeArgsContain(args, "-probesize", "5242880") {
		t.Fatalf("probesize not preserved: %v", args)
	}
	if args[len(args)-1] != "http://x/y" {
		t.Fatalf("input path must be the last arg, got: %v", args)
	}
}
