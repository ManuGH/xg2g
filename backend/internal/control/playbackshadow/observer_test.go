package playbackshadow

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObserver_Noop(t *testing.T) {
	n := NoopObserver{}
	obs := ShadowObservation{}
	assert.False(t, n.TryObserve(obs))
}

func TestObserver_TryObserve(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 1}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	obs := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "live"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}

	// First should succeed (queue capacity 1)
	assert.True(t, w.TryObserve(obs))

	// Second should drop
	assert.False(t, w.TryObserve(obs))

	// Verify dropped metric
	count := testutil.ToFloat64(w.queueDroppedTotal)
	assert.Equal(t, float64(1), count)
}

func TestObserver_ProcessObservation(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 10}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	w.planner = func(ev playbackplanner.PlaybackEvidence) (playbackplanner.PlanningResult, error) {
		return playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Outcome: "allow",
				Mode:    "copy",
			},
		}, nil
	}

	obs := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "live"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow", Mode: "remux"}, // diff on Mode
	}

	w.processOne(obs)

	// Verify observations count
	obsCount := testutil.ToFloat64(w.observationsTotal.WithLabelValues("allow", "allow", "live"))
	assert.Equal(t, float64(1), obsCount)

	// Verify diffs count (mode_mismatch)
	diffCount := testutil.ToFloat64(w.diffsTotal.WithLabelValues("mode_mismatch", "live"))
	assert.Equal(t, float64(1), diffCount)
}

func TestObserver_ProcessError(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 10}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	w.planner = func(ev playbackplanner.PlaybackEvidence) (playbackplanner.PlanningResult, error) {
		return playbackplanner.PlanningResult{}, playbackplanner.ErrRuleNotImplemented
	}

	obs := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "live"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}

	w.processOne(obs)

	// Verify error metric
	errCount := testutil.ToFloat64(w.errorsTotal.WithLabelValues("rule_not_implemented", "live"))
	assert.Equal(t, float64(1), errCount)
}

func TestObserver_ProcessPanicRecovery(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 10}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	w.Start(context.Background())
	processed := make(chan struct{})

	w.planner = func(ev playbackplanner.PlaybackEvidence) (playbackplanner.PlanningResult, error) {
		if ev.Scope == "vod" {
			defer close(processed)
		}
		if ev.Scope == "live" {
			panic("test panic")
		}
		return playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Outcome: "allow",
				Mode:    "copy",
			},
		}, nil
	}

	obs1 := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "live"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}
	obs2 := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "vod"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}

	assert.True(t, w.TryObserve(obs1))
	assert.True(t, w.TryObserve(obs2))

	<-processed // Wait for second to process

	require.NoError(t, w.Close(context.Background()))

	// Verify panic error metric
	errCount := testutil.ToFloat64(w.errorsTotal.WithLabelValues("panic", "live"))
	assert.Equal(t, float64(1), errCount)

	// Verify obs2 succeeded
	obsCount := testutil.ToFloat64(w.observationsTotal.WithLabelValues("allow", "allow", "vod"))
	assert.Equal(t, float64(1), obsCount)
}

func TestObserver_TryObserveAfterShutdown(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 10}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	w.Start(context.Background())
	require.NoError(t, w.Close(context.Background()))

	obs := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "live"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}

	// Should safely return false since the done channel is closed
	assert.False(t, w.TryObserve(obs))
}
