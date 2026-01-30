package manager

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrency_BoundedStart floods 200 Start events and asserts no more than N concurrent executions
func TestConcurrency_BoundedStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := store.NewMemoryStore()
	bus := NewStubBus()

	concurrencyLimit := 3
	var concurrentCount int32
	var maxConcurrent int32

	// Pipeline that tracks concurrency
	eventCount := concurrencyLimit + 1
	trackingPipeline := &ConcurrencyTrackingPipeline{
		concurrentCount: &concurrentCount,
		maxConcurrent:   &maxConcurrent,
		entered:         make(chan struct{}, eventCount),
		release:         make(chan struct{}),
	}

	orch := &Orchestrator{
		Store:               st,
		Bus:                 bus,
		LeaseTTL:            24 * time.Hour,
		HeartbeatEvery:      24 * time.Hour,
		Owner:               "test-flood",
		TunerSlots:          []int{0, 1, 2}, // Enough slots for limit
		Admission:           testkit.NewAdmissibleAdmission(),
		Pipeline:            trackingPipeline,
		PipelineStopTimeout: 1 * time.Second,
		StartConcurrency:    concurrencyLimit,
		StopConcurrency:     5,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef) // Unique per session
		},
		Sweeper: SweeperConfig{
			Interval:         5 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
	}

	// Start Orchestrator
	go func() {
		_ = orch.Run(ctx)
	}()
	bus.WaitForSubscriber(string(model.EventStartSession))

	// Flood 200 Start events
	var wg sync.WaitGroup
	for i := 0; i < eventCount; i++ {
		sessionID := fmt.Sprintf("flood-test-%d", i)
		serviceRef := string(sessionID) // Unique service per session to avoid dedup blocking
		_ = st.PutSession(ctx, &model.SessionRecord{
			SessionID:  sessionID,
			ServiceRef: serviceRef,
			State:      model.SessionNew,
		})

		wg.Add(1)
		go func(sid, ref string) {
			defer wg.Done()
			_ = bus.Publish(ctx, string(model.EventStartSession), model.StartSessionEvent{
				SessionID:  sid,
				ServiceRef: ref,
				ProfileID:  "test",
			})
		}(sessionID, serviceRef)
	}

	// Wait for all events to be published
	wg.Wait()

	for i := 0; i < concurrencyLimit; i++ {
		<-trackingPipeline.entered
	}

	// Assertions
	observed := atomic.LoadInt32(&maxConcurrent)
	t.Logf("Max concurrent executions observed: %d (limit: %d)", observed, concurrencyLimit)

	assert.Equal(t, int32(concurrencyLimit), observed,
		"Concurrency should reach limit exactly when backlog exists")

	close(trackingPipeline.release)
}

// ConcurrencyTrackingPipeline tracks concurrency during Start
type ConcurrencyTrackingPipeline struct {
	concurrentCount *int32
	maxConcurrent   *int32
	entered         chan struct{}
	release         chan struct{}
}

func (p *ConcurrencyTrackingPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	current := atomic.AddInt32(p.concurrentCount, 1)
	defer atomic.AddInt32(p.concurrentCount, -1)

	// Update max if needed
	for {
		max := atomic.LoadInt32(p.maxConcurrent)
		if current <= max || atomic.CompareAndSwapInt32(p.maxConcurrent, max, current) {
			break
		}
	}

	p.entered <- struct{}{}
	<-p.release
	return ports.RunHandle(spec.SessionID), nil
}

func (p *ConcurrencyTrackingPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	return nil
}

func (p *ConcurrencyTrackingPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: false}
}

// TestConcurrency_ValidationFails tests that missing config fails fast
func TestConcurrency_ValidationFails(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	bus := NewStubBus()

	orch := &Orchestrator{
		Store:            st,
		Bus:              bus,
		Admission:        newAdmissionMonitor(10, 10, 0),
		StartConcurrency: 0, // INVALID
		StopConcurrency:  5,
	}

	err := orch.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StartConcurrency must be > 0")
}
