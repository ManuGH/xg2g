package hardware

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestAssessHostPressure_CPUBands(t *testing.T) {
	cases := []struct {
		name       string
		snapshot   playbackprofile.HostRuntimeSnapshot
		wantBand   playbackprofile.HostPressureBand
		wantSignal playbackprofile.HostPressureSignal
	}{
		{
			name: "cpu unknown becomes constrained",
			snapshot: playbackprofile.HostRuntimeSnapshot{
				CPU: playbackprofile.HostCPUSnapshot{CoreCount: 8, SampleCount: 0},
			},
			wantBand:   playbackprofile.HostPressureConstrained,
			wantSignal: playbackprofile.HostPressureSignalCPUUnknown,
		},
		{
			name: "cpu elevated",
			snapshot: playbackprofile.HostRuntimeSnapshot{
				CPU: playbackprofile.HostCPUSnapshot{Load1m: 6.4, CoreCount: 8, SampleCount: 15},
			},
			wantBand:   playbackprofile.HostPressureElevated,
			wantSignal: playbackprofile.HostPressureSignalCPUElevated,
		},
		{
			name: "cpu critical",
			snapshot: playbackprofile.HostRuntimeSnapshot{
				CPU: playbackprofile.HostCPUSnapshot{Load1m: 12.4, CoreCount: 8, SampleCount: 15},
			},
			wantBand:   playbackprofile.HostPressureCritical,
			wantSignal: playbackprofile.HostPressureSignalCPUCritical,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessHostPressure(tc.snapshot, playbackprofile.HostPressureState{})
			if got.RawBand != tc.wantBand || got.EffectiveBand != tc.wantBand {
				t.Fatalf("expected band %q, got raw=%q effective=%q", tc.wantBand, got.RawBand, got.EffectiveBand)
			}
			if len(got.Signals) == 0 || got.Signals[0] != tc.wantSignal {
				t.Fatalf("expected first signal %q, got %#v", tc.wantSignal, got.Signals)
			}
		})
	}
}

func TestAssessHostPressure_ConcurrencyElevatesBand(t *testing.T) {
	got := AssessHostPressure(playbackprofile.HostRuntimeSnapshot{
		CPU: playbackprofile.HostCPUSnapshot{Load1m: 1, CoreCount: 8, SampleCount: 15},
		Concurrency: playbackprofile.HostConcurrencySnapshot{
			SessionsActive:    7,
			MaxSessions:       8,
			ActiveVAAPITokens: 1,
			MaxVAAPITokens:    1,
		},
	}, playbackprofile.HostPressureState{})

	if got.RawBand != playbackprofile.HostPressureConstrained || got.EffectiveBand != playbackprofile.HostPressureConstrained {
		t.Fatalf("expected constrained band from saturated concurrency, got %#v", got)
	}
}

func TestAssessHostPressure_HysteresisDelaysRecovery(t *testing.T) {
	snapshot := playbackprofile.HostRuntimeSnapshot{
		CPU: playbackprofile.HostCPUSnapshot{Load1m: 0.2, CoreCount: 8, SampleCount: 15},
	}

	first := AssessHostPressure(snapshot, playbackprofile.HostPressureState{
		Band: playbackprofile.HostPressureConstrained,
	})
	if first.RawBand != playbackprofile.HostPressureNormal {
		t.Fatalf("expected raw normal after recovery, got %q", first.RawBand)
	}
	if first.EffectiveBand != playbackprofile.HostPressureConstrained {
		t.Fatalf("expected hysteresis to hold constrained band, got %q", first.EffectiveBand)
	}
	if first.State.RecoveryCount != 1 {
		t.Fatalf("expected recovery count 1, got %d", first.State.RecoveryCount)
	}

	second := AssessHostPressure(snapshot, first.State)
	if second.EffectiveBand != playbackprofile.HostPressureConstrained {
		t.Fatalf("expected second healthy sample to still hold constrained, got %q", second.EffectiveBand)
	}
	if second.State.RecoveryCount != 2 {
		t.Fatalf("expected recovery count 2, got %d", second.State.RecoveryCount)
	}

	third := AssessHostPressure(snapshot, second.State)
	if third.EffectiveBand != playbackprofile.HostPressureNormal {
		t.Fatalf("expected third healthy sample to release to normal, got %q", third.EffectiveBand)
	}
	if third.State.RecoveryCount != 0 {
		t.Fatalf("expected recovery count reset after release, got %d", third.State.RecoveryCount)
	}
}

func TestPressureTracker_EvaluatePersistsState(t *testing.T) {
	tracker := NewPressureTracker()

	critical := tracker.Evaluate(playbackprofile.HostRuntimeSnapshot{
		CPU: playbackprofile.HostCPUSnapshot{Load1m: 16, CoreCount: 8, SampleCount: 15},
	})
	if critical.EffectiveBand != playbackprofile.HostPressureCritical {
		t.Fatalf("expected critical band, got %#v", critical)
	}

	healthy := tracker.Evaluate(playbackprofile.HostRuntimeSnapshot{
		CPU: playbackprofile.HostCPUSnapshot{Load1m: 0.1, CoreCount: 8, SampleCount: 15},
	})
	if healthy.EffectiveBand != playbackprofile.HostPressureCritical {
		t.Fatalf("expected tracker hysteresis to hold critical on first recovery sample, got %#v", healthy)
	}
	if tracker.State().Band != playbackprofile.HostPressureCritical {
		t.Fatalf("expected tracker to persist critical band, got %#v", tracker.State())
	}
}
