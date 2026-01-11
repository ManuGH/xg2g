package v3

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/types"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resolverMockProber struct {
	probeCalls int32
	delay      time.Duration
	err        error
	duration   float64
	mu         sync.Mutex
	lastCtx    context.Context
}

func (m *resolverMockProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	atomic.AddInt32(&m.probeCalls, 1)
	m.mu.Lock()
	m.lastCtx = ctx
	m.mu.Unlock()

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.err != nil {
		return nil, m.err
	}
	return &vod.StreamInfo{
		Video: vod.VideoStreamInfo{Duration: m.duration},
	}, nil
}

type mockMapper struct {
	localPath string
}

func (m *mockMapper) ResolveLocalExisting(receiverPath string) (string, bool) {
	return m.localPath, m.localPath != ""
}

func setupTestResolver(prober vod.Prober, clock vod.Clock, mapper recordings.Mapper) (*DefaultVODResolver, *vod.Manager) {
	mgr := vod.NewManager(nil, prober, nil)
	store, _ := library.NewStore(":memory:")

	resolver := NewVODResolver(context.Background(), mgr, store, mapper, nil, clock)
	return resolver, mgr
}

func encodeServiceRef(path string) string {
	ref := "1:0:0:0:0:0:0:0:0:0:" + path
	return base64.RawURLEncoding.EncodeToString([]byte(ref))
}

func TestVODPlayback_ThunderingHerd(t *testing.T) {
	prober := &resolverMockProber{duration: 3600, delay: 50 * time.Millisecond}
	clock := vod.NewMockClock(time.Now())
	mapper := &mockMapper{localPath: "/tmp/test.ts"}
	resolver, _ := setupTestResolver(prober, clock, mapper)

	serviceRef := encodeServiceRef("/media/hdd/movie/test.ts")
	profile := playback.ClientProfile{UserAgent: "Generic"}
	intent := types.IntentMetadata

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	results := make([]error, workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := resolver.ResolveVOD(context.Background(), serviceRef, intent, profile)
			results[idx] = err
		}(i)
	}
	wg.Wait()

	for _, err := range results {
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&prober.probeCalls), "Probe should be called exactly once")
}

func TestVODPlayback_ZombiePrevention(t *testing.T) {
	prober := &resolverMockProber{duration: 3600, delay: 1 * time.Second}
	clock := vod.NewMockClock(time.Now())
	mapper := &mockMapper{localPath: "/tmp/test.ts"}
	resolver, _ := setupTestResolver(prober, clock, mapper)

	serviceRef := encodeServiceRef("/media/hdd/movie/test.ts")
	profile := playback.ClientProfile{UserAgent: "Generic"}
	intent := types.IntentMetadata
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveVOD(ctx, serviceRef, intent, profile)
		errCh <- err
	}()

	// Wait for probe to start
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&prober.probeCalls) > 0
	}, 1*time.Second, 10*time.Millisecond, "Probe should have started")

	// Cancel the only waiter
	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for resolver error")
	}

	prober.mu.Lock()
	pCtx := prober.lastCtx
	prober.mu.Unlock()

	require.NotNil(t, pCtx, "Probe context must not be nil")
	assert.Error(t, pCtx.Err(), "Underlying probe context must be cancelled when waiters drop to zero")
}

func TestVODPlayback_NegativeCacheTTL(t *testing.T) {
	prober := &resolverMockProber{err: errors.New("corrupt media")}
	clock := vod.NewMockClock(time.Now())
	mapper := &mockMapper{localPath: "/tmp/test.ts"}
	resolver, _ := setupTestResolver(prober, clock, mapper)

	serviceRef := encodeServiceRef("/media/hdd/movie/test.ts")
	profile := playback.ClientProfile{UserAgent: "Generic"}
	intent := types.IntentMetadata

	// 1. Initial failure
	_, err := resolver.ResolveVOD(context.Background(), serviceRef, intent, profile)
	assert.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&prober.probeCalls))

	// 2. Immediate second call -> Negative Cache Hit
	_, err = resolver.ResolveVOD(context.Background(), serviceRef, intent, profile)
	assert.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&prober.probeCalls), "Should NOT re-probe within TTL")

	// 3. Advance time past TTL (Corrupt TTL = 120m)
	clock.Advance(121 * time.Minute)

	// 4. Call after TTL -> Should re-probe
	_, err = resolver.ResolveVOD(context.Background(), serviceRef, intent, profile)
	assert.Error(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&prober.probeCalls), "Should re-probe after TTL expiry")
}
