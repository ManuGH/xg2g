// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func analyzeDurationArg(t *testing.T, args []string) string {
	t.Helper()
	v, ok := valueAfter(args, "-analyzeduration")
	if !ok {
		t.Fatalf("no -analyzeduration in args: %v", args)
	}
	return v
}

func relayTranscodeSpec() ports.StreamSpec {
	return ports.StreamSpec{
		Mode:    ports.ModeLive,
		Source:  ports.StreamSource{Type: ports.SourceTuner, ID: "1:0:19:72:D:85:C00000:0:0:0"},
		Profile: model.ProfileSpec{TranscodeVideo: true, VideoCodec: "av1"},
	}
}

const relayTestURL = "http://10.10.55.64:17999/1:0:19:72:D:85:C00000:0:0:0"

// An explicit XG2G_STREAMRELAY_ANALYZE_DURATION override must win on the
// relay-transcode path (which previously hardcoded 10s), so a fleet that has
// verified a faster probe can cut the visible startup penalty.
func TestPlanInput_RelayTranscodeHonorsAnalyzeOverride(t *testing.T) {
	a := &LocalAdapter{StreamRelayAnalyzeDuration: "3000000", StreamRelayProbeSize: "5M"}
	plan, err := a.planInput(relayTranscodeSpec(), relayTestURL)
	if err != nil {
		t.Fatalf("planInput: %v", err)
	}
	if got := analyzeDurationArg(t, plan.args); got != "3000000" {
		t.Fatalf("analyzeduration = %q, want the override 3000000", got)
	}
	if got, _ := valueAfter(plan.args, "-probesize"); got != "5M" {
		t.Fatalf("probesize = %q, want the override 5M", got)
	}
}

// The configured deep-probe value (NewLocalAdapter defaults it to 10s) flows
// through to the relay-transcode path. The shipped default staying at 10s is
// the negative control already covered by
// TestBuildArgs_StreamRelayTranscodeUsesRobustLiveProbe.
func TestPlanInput_RelayTranscodeUsesConfiguredDeepProbe(t *testing.T) {
	a := &LocalAdapter{StreamRelayAnalyzeDuration: "10000000", StreamRelayProbeSize: "20M"}
	plan, err := a.planInput(relayTranscodeSpec(), relayTestURL)
	if err != nil {
		t.Fatalf("planInput: %v", err)
	}
	if got := analyzeDurationArg(t, plan.args); got != "10000000" {
		t.Fatalf("analyzeduration = %q, want configured 10000000", got)
	}
}
