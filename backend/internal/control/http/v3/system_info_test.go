package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

func TestConvertTunersSuppressesStaleStreamingWhenGlobalSignalsIdle(t *testing.T) {
	tuners := []openwebif.AboutTuner{
		{
			Name:   "Tuner A",
			Type:   "DVB-S2",
			Stream: "1:0:1:ABCD:0:0:0:0:0:0:",
		},
	}

	status := &openwebif.StatusInfo{IsStreaming: "false"}
	got := convertTuners(tuners, []any{}, status)

	if len(got) != 1 {
		t.Fatalf("expected 1 tuner, got %d", len(got))
	}
	if got[0].Status != "idle" {
		t.Fatalf("expected stale streaming tuner to be suppressed to idle, got %q", got[0].Status)
	}
}

func TestConvertTunersPreservesStreamingWhenGlobalStatusIsActive(t *testing.T) {
	tuners := []openwebif.AboutTuner{
		{
			Name:   "Tuner A",
			Type:   "DVB-S2",
			Stream: "1:0:1:ABCD:0:0:0:0:0:0:",
		},
	}

	status := &openwebif.StatusInfo{IsStreaming: "true"}
	got := convertTuners(tuners, []any{}, status)

	if len(got) != 1 {
		t.Fatalf("expected 1 tuner, got %d", len(got))
	}
	if got[0].Status != "streaming" {
		t.Fatalf("expected active streaming tuner to remain streaming, got %q", got[0].Status)
	}
}

func TestConvertTunersPreservesLegacyBehaviorWithoutGlobalSignals(t *testing.T) {
	tuners := []openwebif.AboutTuner{
		{
			Name:   "Tuner A",
			Type:   "DVB-S2",
			Stream: "1:0:1:ABCD:0:0:0:0:0:0:",
		},
	}

	got := convertTuners(tuners, nil, nil)

	if len(got) != 1 {
		t.Fatalf("expected 1 tuner, got %d", len(got))
	}
	if got[0].Status != "streaming" {
		t.Fatalf("expected streaming tuner to remain streaming without cross-check signals, got %q", got[0].Status)
	}
}

func TestConvertTunersPreservesRecordingAndLivePriority(t *testing.T) {
	tuners := []openwebif.AboutTuner{
		{
			Name:   "Tuner A",
			Type:   "DVB-S2",
			Rec:    "recording-ref",
			Stream: "stale-stream-ref",
		},
		{
			Name:   "Tuner B",
			Type:   "DVB-S2",
			Live:   "live-ref",
			Stream: "stale-stream-ref",
		},
	}

	status := &openwebif.StatusInfo{IsStreaming: "false"}
	got := convertTuners(tuners, []any{}, status)

	if len(got) != 2 {
		t.Fatalf("expected 2 tuners, got %d", len(got))
	}
	if got[0].Status != "recording" {
		t.Fatalf("expected recording tuner to stay recording, got %q", got[0].Status)
	}
	if got[1].Status != "live" {
		t.Fatalf("expected live tuner to stay live, got %q", got[1].Status)
	}
}

func TestParseOWIBoolString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKnown bool
		wantValue bool
	}{
		{name: "true lowercase", input: "true", wantKnown: true, wantValue: true},
		{name: "true padded uppercase", input: " TRUE ", wantKnown: true, wantValue: true},
		{name: "false lowercase", input: "false", wantKnown: true, wantValue: false},
		{name: "false numeric", input: "0", wantKnown: true, wantValue: false},
		{name: "unknown empty", input: "", wantKnown: false, wantValue: false},
		{name: "unknown garbage", input: "maybe", wantKnown: false, wantValue: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotKnown, gotValue := parseOWIBoolString(tc.input)
			if gotKnown != tc.wantKnown || gotValue != tc.wantValue {
				t.Fatalf("parseOWIBoolString(%q) = (%t,%t), want (%t,%t)", tc.input, gotKnown, gotValue, tc.wantKnown, tc.wantValue)
			}
		})
	}
}
