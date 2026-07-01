package recordings

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLiveRef = "1:0:1:2B66:3F3:1:C00000:0:0:0:"

func liveProbeTestConfig() config.AppConfig {
	return config.AppConfig{
		FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
		HLS:    config.HLSConfig{Root: "/tmp/hls"},
	}
}

func okLiveCapability() scan.Capability {
	now := time.Now().UTC()
	return scan.Capability{
		ServiceRef:  testLiveRef,
		State:       scan.CapabilityStateOK,
		Container:   "ts",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Codec:       "h264",
		Resolution:  "1920x1080",
		Width:       1920,
		Height:      1080,
		FPS:         25,
		LastScan:    now,
		LastSuccess: now,
	}
}

// TestService_ResolvePlaybackInfo_EpgBadgeNeverProbes is the load-bearing assertion
// for decoupling the badge grid from the relay: an epg_badge request for a channel
// with no cached truth must NOT trigger a synchronous probe (it would storm the
// tuner across the whole list). Remove the probeAllowedForContext gate and this
// goes red.
func TestService_ResolvePlaybackInfo_EpgBadgeNeverProbes(t *testing.T) {
	truthSource := &stubTruthSource{
		getCapabilityFn: func(string) (scan.Capability, bool) {
			return scan.Capability{}, false // missing_scan_truth -> would probe in interactive context
		},
		probeCapabilityFn: func(context.Context, string) (scan.Capability, bool, error) {
			t.Error("ProbeCapability must not be called for epg_badge context")
			return scan.Capability{}, false, nil
		},
	}

	svc := NewService(stubDeps{svc: &stubRecordingsService{}, truthSource: truthSource, cfg: liveProbeTestConfig()})

	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   testLiveRef,
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-badge-no-probe",
		Headers:     map[string]string{PlaybackInfoContextHeader: PlaybackInfoContextEpgBadge},
	})

	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Equal(t, 0, truthSource.probeCalls)
}

// TestService_ResolvePlaybackInfo_LiveColdProbeBoundedByBudget proves an interactive
// play request returns near the budget instead of blocking for the full cold probe,
// and that the probe keeps running in the background to populate the cache. With the
// old unbounded ProbeCapability call the request would block ~400ms (> assertion).
func TestService_ResolvePlaybackInfo_LiveColdProbeBoundedByBudget(t *testing.T) {
	setLiveInteractiveProbeBudget(80 * time.Millisecond)
	t.Cleanup(func() { setLiveInteractiveProbeBudget(defaultLiveInteractiveProbeBudget) })

	var probeCalls int32
	probeReturned := make(chan struct{})
	truthSource := &stubTruthSource{
		getCapabilityFn: func(string) (scan.Capability, bool) {
			return scan.Capability{}, false
		},
		probeCapabilityFn: func(context.Context, string) (scan.Capability, bool, error) {
			atomic.AddInt32(&probeCalls, 1)
			time.Sleep(400 * time.Millisecond) // slow cold relay
			close(probeReturned)
			return okLiveCapability(), true, nil
		},
	}

	svc := NewService(stubDeps{svc: &stubRecordingsService{}, truthSource: truthSource, cfg: liveProbeTestConfig()})

	start := time.Now()
	_, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   testLiveRef,
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "live",
		RequestID:   "req-cold-bounded",
		Headers:     map[string]string{PlaybackInfoContextHeader: PlaybackInfoContextPlayerStart},
	})
	elapsed := time.Since(start)

	require.NotNil(t, err)
	assert.Equal(t, PlaybackInfoErrorUnverified, err.Kind)
	assert.Less(t, elapsed, 250*time.Millisecond, "interactive request must return near the budget, not wait for the full probe")

	select {
	case <-probeReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("detached probe did not complete in the background")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&probeCalls))
}

// concurrentTruthSource is a race-safe truth source for the dedup test (the shared
// stubTruthSource increments unsynchronized counters and is not safe under the
// concurrent GetCapability calls this test makes).
type concurrentTruthSource struct {
	probeCalls int32
	release    chan struct{}
}

func (s *concurrentTruthSource) GetCapability(string) (scan.Capability, bool) {
	return scan.Capability{}, false // missing_scan_truth -> interactive probe
}

func (s *concurrentTruthSource) ProbeCapability(context.Context, string) (scan.Capability, bool, error) {
	atomic.AddInt32(&s.probeCalls, 1)
	<-s.release // hold all callers inside the single shared probe
	return okLiveCapability(), true, nil
}

// TestService_ResolvePlaybackInfo_LiveConcurrentProbesDeduped proves concurrent
// interactive requests for the same channel collapse into a single relay probe.
// Without singleflight each request would call ProbeCapability (probeCalls == n).
func TestService_ResolvePlaybackInfo_LiveConcurrentProbesDeduped(t *testing.T) {
	setLiveInteractiveProbeBudget(2 * time.Second)
	t.Cleanup(func() { setLiveInteractiveProbeBudget(defaultLiveInteractiveProbeBudget) })

	truthSource := &concurrentTruthSource{release: make(chan struct{})}

	svc := NewService(stubDeps{svc: &stubRecordingsService{}, truthSource: truthSource, cfg: liveProbeTestConfig()})

	const n = 5
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
				SubjectID:   testLiveRef,
				SubjectKind: PlaybackSubjectLive,
				APIVersion:  "v3.1",
				SchemaType:  "live",
				Headers:     map[string]string{PlaybackInfoContextHeader: PlaybackInfoContextPlayerStart},
			})
		}()
	}

	time.Sleep(150 * time.Millisecond) // let all goroutines enter the singleflight
	close(truthSource.release)
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&truthSource.probeCalls), "concurrent interactive requests must share a single probe")
}
