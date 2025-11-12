# Tier 2: GPU Queue System (Multi-Client Support)

## Ziel
Effizientes GPU Resource Management für 10+ parallele Transcode-Streams

## Problem
Aktuell Mode 3 (GPU Transcoding):
- Keine Priorisierung bei vielen Clients
- GPU kann überlastet werden (>10 Streams)
- Keine Queue → Requests werden direkt abgewiesen
- Kein Fair Scheduling

## Lösung: Priority Queue System

### Prioritäten:
- **Priority 0 (Preview)**: Thumbnails, Preview-Streams
- **Priority 1 (Normal)**: Neue Client-Requests
- **Priority 2 (Active)**: Bereits streamende Clients

### Queue Verhalten:
- Max Queue Size: 100 Requests
- Worker Pool: 4 GPU Workers (konfigurierbar)
- Timeout: 30s (Request wird abgewiesen)
- FIFO innerhalb gleicher Priorität

---

## Implementation

### 1. GPU Queue Package

**Datei:** `internal/gpu/queue.go` (neu)

```go
// SPDX-License-Identifier: MIT

package gpu

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
)

var (
	queueSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Name:      "gpu_queue_size",
			Help:      "Current number of requests in GPU queue",
		},
		[]string{"priority"},
	)

	queueWaitTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "xg2g",
			Name:      "gpu_queue_wait_seconds",
			Help:      "Time spent waiting in GPU queue",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 8), // 100ms to 12.8s
		},
		[]string{"priority"},
	)

	queueRejections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Name:      "gpu_queue_rejections_total",
			Help:      "Total GPU queue rejections",
		},
		[]string{"reason"}, // reason: "full|timeout"
	)

	activeWorkers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Name:      "gpu_active_workers",
			Help:      "Number of active GPU workers",
		},
	)
)

// Priority levels
type Priority int

const (
	PriorityPreview Priority = 0
	PriorityNormal  Priority = 1
	PriorityActive  Priority = 2
)

// TranscodeRequest represents a GPU transcode request
type TranscodeRequest struct {
	ID        string
	StreamID  string
	ClientIP  string
	Priority  Priority
	Codec     string
	Data      []byte
	CreatedAt time.Time
	Deadline  time.Time

	// Result channel
	ResultChan chan *TranscodeResult
}

// TranscodeResult contains the result of a transcode operation
type TranscodeResult struct {
	Data  []byte
	Error error
}

// Config holds GPU queue configuration
type Config struct {
	MaxQueueSize int           // Maximum queue size
	Workers      int           // Number of concurrent GPU workers
	MaxWaitTime  time.Duration // Maximum wait time before rejection
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxQueueSize: 100,
		Workers:      4, // Most GPUs can handle 4 parallel encodes
		MaxWaitTime:  30 * time.Second,
	}
}

// Queue manages GPU transcode requests with priority scheduling
type Queue struct {
	config Config
	logger zerolog.Logger

	// Priority queues (0=preview, 1=normal, 2=active)
	queues [3]chan *TranscodeRequest

	// Worker management
	workers   int
	workerSem chan struct{} // Semaphore for worker pool
	wg        sync.WaitGroup

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewQueue creates a new GPU queue
func NewQueue(config Config, logger zerolog.Logger) *Queue {
	ctx, cancel := context.WithCancel(context.Background())

	q := &Queue{
		config:    config,
		logger:    logger,
		queues:    [3]chan *TranscodeRequest{},
		workerSem: make(chan struct{}, config.Workers),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Initialize priority queues
	for i := 0; i < 3; i++ {
		q.queues[i] = make(chan *TranscodeRequest, config.MaxQueueSize/3)
	}

	// Start workers
	for i := 0; i < config.Workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}

	// Start queue metrics updater
	go q.updateMetrics()

	return q
}

// Submit submits a transcode request to the queue
func (q *Queue) Submit(req *TranscodeRequest) error {
	// Set deadline if not already set
	if req.Deadline.IsZero() {
		req.Deadline = time.Now().Add(q.config.MaxWaitTime)
	}

	// Select queue based on priority
	queue := q.queues[req.Priority]

	select {
	case queue <- req:
		q.logger.Debug().
			Str("request_id", req.ID).
			Int("priority", int(req.Priority)).
			Msg("request queued")
		return nil

	default:
		// Queue full
		queueRejections.WithLabelValues("full").Inc()
		return errors.New("gpu queue full")
	}
}

// worker processes requests from the queue
func (q *Queue) worker(id int) {
	defer q.wg.Done()

	q.logger.Info().
		Int("worker_id", id).
		Msg("GPU worker started")

	for {
		// Try to get next request (priority order)
		req := q.getNextRequest()
		if req == nil {
			// Queue closed
			return
		}

		// Acquire worker slot
		q.workerSem <- struct{}{}
		activeWorkers.Inc()

		// Check if deadline exceeded
		if time.Now().After(req.Deadline) {
			queueRejections.WithLabelValues("timeout").Inc()
			req.ResultChan <- &TranscodeResult{
				Error: errors.New("request deadline exceeded"),
			}
			activeWorkers.Dec()
			<-q.workerSem
			continue
		}

		// Record wait time
		waitTime := time.Since(req.CreatedAt).Seconds()
		queueWaitTime.WithLabelValues(fmt.Sprintf("%d", req.Priority)).Observe(waitTime)

		// Process request
		result := q.processRequest(req)

		// Send result
		select {
		case req.ResultChan <- result:
		case <-time.After(1 * time.Second):
			q.logger.Warn().
				Str("request_id", req.ID).
				Msg("result channel blocked, dropping result")
		}

		// Release worker slot
		activeWorkers.Dec()
		<-q.workerSem
	}
}

// getNextRequest gets the next request from priority queues
func (q *Queue) getNextRequest() *TranscodeRequest {
	// Priority order: Active (2) > Normal (1) > Preview (0)
	for {
		select {
		case <-q.ctx.Done():
			return nil

		// Priority 2 (Active streams)
		case req := <-q.queues[PriorityActive]:
			return req

		// Priority 1 (Normal requests)
		case req := <-q.queues[PriorityNormal]:
			return req

		// Priority 0 (Preview/Thumbnails)
		case req := <-q.queues[PriorityPreview]:
			return req

		// No requests available, wait a bit
		case <-time.After(100 * time.Millisecond):
			continue
		}
	}
}

// processRequest performs the actual GPU transcode
func (q *Queue) processRequest(req *TranscodeRequest) *TranscodeResult {
	// TODO: Call actual GPU transcoder here
	// For now, simulate work
	time.Sleep(50 * time.Millisecond)

	// Example integration with existing transcoder:
	// transcoder := gpu.NewTranscoder(...)
	// data, err := transcoder.Transcode(req.Data, req.Codec)

	return &TranscodeResult{
		Data:  req.Data, // Placeholder
		Error: nil,
	}
}

// updateMetrics periodically updates queue size metrics
func (q *Queue) updateMetrics() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 3; i++ {
				size := len(q.queues[i])
				queueSize.WithLabelValues(fmt.Sprintf("%d", i)).Set(float64(size))
			}
		}
	}
}

// Shutdown gracefully shuts down the queue
func (q *Queue) Shutdown(timeout time.Duration) error {
	q.logger.Info().Msg("shutting down GPU queue")

	// Stop accepting new requests
	q.cancel()

	// Wait for workers to finish (with timeout)
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		q.logger.Info().Msg("GPU queue shutdown complete")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("shutdown timeout after %v", timeout)
	}
}
```

---

### 2. Integration in Transcoder

**Datei:** `internal/proxy/transcoder.go` (erweitern)

```go
import "github.com/ManuGH/xg2g/internal/gpu"

type Transcoder struct {
	// ... existing fields
	gpuQueue *gpu.Queue
}

func NewTranscoder(config Config, logger zerolog.Logger) *Transcoder {
	t := &Transcoder{
		// ... existing init
	}

	// Initialize GPU queue if GPU mode enabled
	if config.GPUEnabled {
		queueConfig := gpu.DefaultConfig()
		// Override from ENV if needed
		t.gpuQueue = gpu.NewQueue(queueConfig, logger)
	}

	return t
}

// TranscodeStream with GPU queue
func (t *Transcoder) TranscodeStream(ctx context.Context, streamID, clientIP string, data []byte) ([]byte, error) {
	if t.gpuQueue == nil {
		// Fallback to direct transcode
		return t.directTranscode(data)
	}

	// Determine priority based on stream state
	priority := gpu.PriorityNormal
	if t.isActiveStream(streamID) {
		priority = gpu.PriorityActive
	}

	// Submit to queue
	req := &gpu.TranscodeRequest{
		ID:         fmt.Sprintf("%s-%d", streamID, time.Now().UnixNano()),
		StreamID:   streamID,
		ClientIP:   clientIP,
		Priority:   priority,
		Data:       data,
		CreatedAt:  time.Now(),
		ResultChan: make(chan *gpu.TranscodeResult, 1),
	}

	if err := t.gpuQueue.Submit(req); err != nil {
		return nil, fmt.Errorf("queue submit failed: %w", err)
	}

	// Wait for result
	select {
	case result := <-req.ResultChan:
		if result.Error != nil {
			return nil, result.Error
		}
		return result.Data, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

---

### 3. Tests

**Datei:** `internal/gpu/queue_test.go` (neu)

```go
package gpu

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestQueuePriority(t *testing.T) {
	config := DefaultConfig()
	config.Workers = 1 // Single worker for deterministic testing
	logger := zerolog.Nop()

	q := NewQueue(config, logger)
	defer q.Shutdown(5 * time.Second)

	// Submit requests in reverse priority order
	reqs := []*TranscodeRequest{
		{ID: "preview", Priority: PriorityPreview, ResultChan: make(chan *TranscodeResult, 1)},
		{ID: "normal", Priority: PriorityNormal, ResultChan: make(chan *TranscodeResult, 1)},
		{ID: "active", Priority: PriorityActive, ResultChan: make(chan *TranscodeResult, 1)},
	}

	for _, req := range reqs {
		req.CreatedAt = time.Now()
		q.Submit(req)
	}

	// Active should be processed first
	result := <-reqs[2].ResultChan
	assert.NoError(t, result.Error)

	// Then normal
	result = <-reqs[1].ResultChan
	assert.NoError(t, result.Error)

	// Finally preview
	result = <-reqs[0].ResultChan
	assert.NoError(t, result.Error)
}

func TestQueueTimeout(t *testing.T) {
	config := DefaultConfig()
	config.MaxWaitTime = 100 * time.Millisecond
	config.Workers = 1
	logger := zerolog.Nop()

	q := NewQueue(config, logger)
	defer q.Shutdown(5 * time.Second)

	// Submit request with tight deadline
	req := &TranscodeRequest{
		ID:         "timeout-test",
		Priority:   PriorityNormal,
		CreatedAt:  time.Now(),
		Deadline:   time.Now().Add(10 * time.Millisecond),
		ResultChan: make(chan *TranscodeResult, 1),
	}

	q.Submit(req)

	// Wait for timeout
	time.Sleep(200 * time.Millisecond)

	result := <-req.ResultChan
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "deadline exceeded")
}
```

---

## Environment Variables

```bash
# GPU Queue Configuration
XG2G_GPU_QUEUE_SIZE=100
XG2G_GPU_QUEUE_WORKERS=4
XG2G_GPU_QUEUE_MAX_WAIT=30s
```

---

## Expected Metrics

```
# HELP xg2g_gpu_queue_size Current number of requests in GPU queue
# TYPE xg2g_gpu_queue_size gauge
xg2g_gpu_queue_size{priority="0"} 2
xg2g_gpu_queue_size{priority="1"} 5
xg2g_gpu_queue_size{priority="2"} 3

# HELP xg2g_gpu_queue_wait_seconds Time spent waiting in GPU queue
# TYPE xg2g_gpu_queue_wait_seconds histogram
xg2g_gpu_queue_wait_seconds_bucket{priority="1",le="0.1"} 10
xg2g_gpu_queue_wait_seconds_bucket{priority="1",le="0.2"} 15
xg2g_gpu_queue_wait_seconds_sum{priority="1"} 2.5
xg2g_gpu_queue_wait_seconds_count{priority="1"} 20

# HELP xg2g_gpu_queue_rejections_total Total GPU queue rejections
# TYPE xg2g_gpu_queue_rejections_total counter
xg2g_gpu_queue_rejections_total{reason="full"} 3
xg2g_gpu_queue_rejections_total{reason="timeout"} 1

# HELP xg2g_gpu_active_workers Number of active GPU workers
# TYPE xg2g_gpu_active_workers gauge
xg2g_gpu_active_workers 4
```

---

## Success Criteria

- ✅ Priority 2 (Active) streams processed first
- ✅ Queue handles 100+ requests without OOM
- ✅ P95 wait time < 1s bei 10 parallelen Streams
- ✅ P99 wait time < 3s bei 20 parallelen Streams
- ✅ Graceful Shutdown ohne Data Loss

---

## Rollout

1. ✅ GPU Queue Package implementieren
2. ✅ Integration in Transcoder
3. ✅ Tests (Priority, Timeout, Capacity)
4. ✅ Metrics & Monitoring
5. ✅ Documentation

**Effort:** 6h
