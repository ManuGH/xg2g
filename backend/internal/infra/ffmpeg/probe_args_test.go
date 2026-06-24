package ffmpeg

import (
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

// TestBuildProbeArgs_NoReconnect: after reverting #607, HTTP capability probes
// must NOT carry reconnect flags — the approach was disproven on staging.
func TestBuildProbeArgs_NoReconnect(t *testing.T) {
	args := buildProbeArgs("http://10.0.0.1:17999/1:0:19:8E:B:85:C00000:0:0:0:", "Connection: close\r\n", ProbeOptions{})
	if probeArgsContain(args, "-reconnect", "1") || probeArgsContain(args, "-reconnect_at_eof", "1") {
		t.Fatalf("probe must not get reconnect flags; got: %v", args)
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
