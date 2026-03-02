package recordings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingPersistor struct {
	called chan struct{}
}

func (p *failingPersistor) PersistDuration(ctx context.Context, serviceRef string, duration int64) error {
	select {
	case p.called <- struct{}{}:
	default:
	}
	return errors.New("persist unavailable")
}

func TestProbeManager_ExpiredBlockedEntryIsRetried(t *testing.T) {
	probeCalled := make(chan struct{}, 1)
	mgr := &mockManager{
		data: make(map[string]vod.Metadata),
		ProbeHook: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
			select {
			case probeCalled <- struct{}{}:
			default:
			}
			return &vod.StreamInfo{
				Container: "mp4",
				Video:     vod.VideoStreamInfo{CodecName: "h264", Duration: 120},
				Audio:     vod.AudioStreamInfo{CodecName: "aac"},
			}, nil
		},
	}

	pm := newProbeManager(context.Background(), mgr, nil, nil)
	serviceRef := "service:ref"

	pm.mu.Lock()
	pm.progress[serviceRef] = &probeEntry{
		state:      ProbeStateBlocked,
		until:      time.Now().Add(-1 * time.Second),
		retryAfter: 64,
		failures:   4,
	}
	pm.mu.Unlock()

	state, _ := pm.ensureProbed(context.Background(), serviceRef, "file:///tmp/movie.ts", "/tmp/movie.ts")
	assert.Equal(t, ProbeStateQueued, state)

	select {
	case <-probeCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected probe retry after blocked entry expired")
	}
}

func TestProbeManager_UsesRootContextForCancellation(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	started := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	mgr := &mockManager{
		data: make(map[string]vod.Metadata),
		ProbeHook: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			select {
			case canceled <- struct{}{}:
			default:
			}
			return nil, ctx.Err()
		},
	}

	pm := newProbeManager(rootCtx, mgr, nil, nil)
	_, _ = pm.ensureProbed(context.Background(), "service:ref", "file:///tmp/movie.ts", "/tmp/movie.ts")

	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("probe did not start")
	}

	cancelRoot()

	select {
	case <-canceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("probe context was not canceled by root context")
	}
}

func TestProbeManager_PersistFailureFailClosed(t *testing.T) {
	beforeFinishedPersist := testutil.ToFloat64(recordingsProbeFinishedTotal.WithLabelValues(probeResultPersistError))
	beforeBlockedPersist := testutil.ToFloat64(recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonPersistError))

	persistor := &failingPersistor{called: make(chan struct{}, 1)}
	mgr := &mockManager{
		data: make(map[string]vod.Metadata),
		ProbeHook: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
			return &vod.StreamInfo{
				Container: "mp4",
				Video:     vod.VideoStreamInfo{CodecName: "h264", Duration: 123},
				Audio:     vod.AudioStreamInfo{CodecName: "aac"},
			}, nil
		},
	}

	pm := newProbeManager(context.Background(), mgr, nil, persistor)
	serviceRef := "service:ref"
	_, _ = pm.ensureProbed(context.Background(), serviceRef, "file:///tmp/movie.ts", "/tmp/movie.ts")

	select {
	case <-persistor.called:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("persistor was not called")
	}

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(recordingsProbeFinishedTotal.WithLabelValues(probeResultPersistError)) > beforeFinishedPersist
	}, time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonPersistError)) > beforeBlockedPersist
	}, time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		state, _ := pm.GetProbeState(serviceRef)
		return state == ProbeStateBlocked
	}, time.Second, 20*time.Millisecond)

	_, exists := mgr.GetMetadata(serviceRef)
	assert.False(t, exists, "metadata must not be marked ready when persistence fails")
}

func TestProbeManager_CooldownBlockedMetric(t *testing.T) {
	beforeCooldown := testutil.ToFloat64(recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonCooldown))
	pm := newProbeManager(context.Background(), &mockManager{data: make(map[string]vod.Metadata)}, nil, nil)

	serviceRef := "service:ref"
	pm.mu.Lock()
	pm.progress[serviceRef] = &probeEntry{
		state:      ProbeStateBlocked,
		until:      time.Now().Add(2 * time.Second),
		retryAfter: 10,
		failures:   2,
	}
	pm.mu.Unlock()

	state, _ := pm.ensureProbed(context.Background(), serviceRef, "file:///tmp/movie.ts", "/tmp/movie.ts")
	assert.Equal(t, ProbeStateBlocked, state)
	assert.Greater(t, testutil.ToFloat64(recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonCooldown)), beforeCooldown)
}

func TestProbeManager_DedupedMetric(t *testing.T) {
	beforeDedup := testutil.ToFloat64(recordingsProbeDedupedTotal)
	beforeInflight := testutil.ToFloat64(recordingsProbeInflight)

	probeEntered := make(chan struct{}, 1)
	releaseProbe := make(chan struct{})
	mgr := &mockManager{
		data: make(map[string]vod.Metadata),
		ProbeHook: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
			select {
			case probeEntered <- struct{}{}:
			default:
			}
			<-releaseProbe
			return &vod.StreamInfo{
				Container: "mp4",
				Video:     vod.VideoStreamInfo{CodecName: "h264", Duration: 100},
				Audio:     vod.AudioStreamInfo{CodecName: "aac"},
			}, nil
		},
	}

	pm := newProbeManager(context.Background(), mgr, nil, nil)
	_, _ = pm.ensureProbed(context.Background(), "service:ref:1", "file:///tmp/shared.ts", "/tmp/shared.ts")

	select {
	case <-probeEntered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first probe did not start")
	}

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(recordingsProbeInflight) >= beforeInflight+1
	}, time.Second, 20*time.Millisecond)

	_, _ = pm.ensureProbed(context.Background(), "service:ref:2", "file:///tmp/shared.ts", "/tmp/shared.ts")
	time.Sleep(40 * time.Millisecond)
	close(releaseProbe)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(recordingsProbeDedupedTotal) > beforeDedup
	}, time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(recordingsProbeInflight) <= beforeInflight
	}, time.Second, 20*time.Millisecond)
}
