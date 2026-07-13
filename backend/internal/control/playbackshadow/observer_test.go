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

func TestObserver_CloseBeforeStart(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 10}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	// Close immediately before Start
	require.NoError(t, w.Close(context.Background()))

	// TryObserve should return false immediately
	obs := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "live"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}
	assert.False(t, w.TryObserve(obs))

	// Start after Close should not panic or start goroutines
	w.Start(context.Background())
}

func TestObserver_ContextRebinding(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := ObserverConfig{Enabled: true, QueueCapacity: 10}
	w, err := NewWorker(cfg, reg, zerolog.Nop())
	require.NoError(t, err)

	ctx1, cancel1 := context.WithCancel(context.Background())
	w.Start(ctx1)

	// Cancel first context
	cancel1()
	// Allow goroutine to notice cancellation and terminate
	w.wg.Wait()

	processed := make(chan struct{})
	w.planner = func(ev playbackplanner.PlaybackEvidence) (playbackplanner.PlanningResult, error) {
		defer close(processed)
		return playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Outcome: "allow",
				Mode:    "copy",
			},
		}, nil
	}

	// Rebind with new context
	ctx2 := context.Background()
	w.Start(ctx2)

	obs := ShadowObservation{
		Evidence: playbackplanner.PlaybackEvidence{Scope: "vod"},
		Legacy:   ComparablePlaybackPlan{IsValid: true, Outcome: "allow"},
	}
	assert.True(t, w.TryObserve(obs))
	<-processed

	require.NoError(t, w.Close(ctx2))
	obsCount := testutil.ToFloat64(w.observationsTotal.WithLabelValues("allow", "allow", "vod"))
	assert.Equal(t, float64(1), obsCount)
}

func TestObserver_RegisterOrGetMismatch(t *testing.T) {
	reg := prometheus.NewRegistry()
	// Register a Counter directly with exact same descriptor as observationsTotal
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_planner_shadow_queue_dropped_total",
		Help: "Total number of playback shadow observations dropped due to full or closed queue.",
	})
	require.NoError(t, reg.Register(counter))

	// Attempting to register OrGet with a Gauge of the exact same descriptor should hit AlreadyRegisteredError and fail the type assertion cleanly without panicking
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_planner_shadow_queue_dropped_total",
		Help: "Total number of playback shadow observations dropped due to full or closed queue.",
	})
	_, err := registerOrGet(reg, gauge, gauge)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collector registered under different type")
}
