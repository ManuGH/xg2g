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

// Priority levels for GPU queue scheduling
type Priority int

const (
	PriorityPreview Priority = 0 // Thumbnails, previews (lowest priority)
	PriorityNormal  Priority = 1 // New client requests
	PriorityActive  Priority = 2 // Active streaming clients (highest priority)
)

// String returns the string representation of the priority level.
func (p Priority) String() string {
	switch p {
	case PriorityPreview:
		return "preview"
	case PriorityNormal:
		return "normal"
	case PriorityActive:
		return "active"
	default:
		return "unknown"
	}
}

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
	MaxQueueSize int           // Maximum queue size per priority level
	Workers      int           // Number of concurrent GPU workers
	MaxWaitTime  time.Duration // Maximum wait time before rejection
}

// DefaultConfig returns sensible defaults for GPU queue configuration
func DefaultConfig() Config {
	return Config{
		MaxQueueSize: 100,              // 100 requests per priority level
		Workers:      4,                // Most GPUs can handle 4 parallel encodes
		MaxWaitTime:  30 * time.Second, // 30 second timeout
	}
}

// Queue manages GPU transcode requests with priority scheduling
type Queue struct {
	config Config
	logger zerolog.Logger

	// Priority queues (0=preview, 1=normal, 2=active)
	queues [3]chan *TranscodeRequest

	// Worker management
	workerSem chan struct{} // Semaphore for worker pool
	wg        sync.WaitGroup

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Transcoder function (injected for testing)
	transcoder func(context.Context, *TranscodeRequest) (*TranscodeResult, error)
}

// NewQueue creates a new GPU queue with the given configuration
func NewQueue(config Config, logger zerolog.Logger) *Queue {
	ctx, cancel := context.WithCancel(context.Background())

	q := &Queue{
		config:    config,
		logger:    logger,
		workerSem: make(chan struct{}, config.Workers),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Initialize priority queues with buffer size
	for i := range q.queues {
		q.queues[i] = make(chan *TranscodeRequest, config.MaxQueueSize)
	}

	return q
}

// SetTranscoder sets the transcoder function for processing requests
func (q *Queue) SetTranscoder(fn func(context.Context, *TranscodeRequest) (*TranscodeResult, error)) {
	q.transcoder = fn
}

// Start begins processing GPU queue requests
func (q *Queue) Start() {
	q.logger.Info().
		Int("workers", q.config.Workers).
		Int("max_queue_size", q.config.MaxQueueSize).
		Dur("max_wait_time", q.config.MaxWaitTime).
		Msg("starting GPU queue")

	// Start single dispatcher that respects priority
	q.wg.Add(1)
	go q.dispatchRequests()
}

// dispatchRequests dispatches requests from priority queues to workers
func (q *Queue) dispatchRequests() {
	defer q.wg.Done()

	for {
		var req *TranscodeRequest
		var priority Priority

		// Try to get request from highest priority queue first
		select {
		case req = <-q.queues[PriorityActive]:
			priority = PriorityActive
		case <-q.ctx.Done():
			return
		default:
			// No active priority requests, try normal
			select {
			case req = <-q.queues[PriorityActive]:
				priority = PriorityActive
			case req = <-q.queues[PriorityNormal]:
				priority = PriorityNormal
			case <-q.ctx.Done():
				return
			default:
				// No active or normal requests, try preview
				select {
				case req = <-q.queues[PriorityActive]:
					priority = PriorityActive
				case req = <-q.queues[PriorityNormal]:
					priority = PriorityNormal
				case req = <-q.queues[PriorityPreview]:
					priority = PriorityPreview
				case <-q.ctx.Done():
					return
				}
			}
		}

		if req == nil {
			continue
		}

		// Check if request has expired
		if time.Now().After(req.Deadline) {
			queueRejections.WithLabelValues("timeout").Inc()
			queueSize.WithLabelValues(priority.String()).Dec()
			if req.ResultChan != nil {
				req.ResultChan <- &TranscodeResult{
					Error: errors.New("request timeout in queue"),
				}
				close(req.ResultChan)
			}
			continue
		}

		// Acquire worker slot (blocks if all workers busy)
		q.workerSem <- struct{}{}
		activeWorkers.Inc()

		// Process request in goroutine
		go q.processRequest(req, priority)
	}
}

// Stop stops the GPU queue and waits for all workers to finish
func (q *Queue) Stop() error {
	q.logger.Info().Msg("stopping GPU queue")
	q.cancel()

	// Close all queues
	for i := range q.queues {
		close(q.queues[i])
	}

	// Wait for all workers to finish
	q.wg.Wait()

	q.logger.Info().Msg("GPU queue stopped")
	return nil
}

// Submit submits a transcode request to the GPU queue
func (q *Queue) Submit(req *TranscodeRequest) error {
	if req.Priority < PriorityPreview || req.Priority > PriorityActive {
		return fmt.Errorf("invalid priority: %d", req.Priority)
	}

	req.CreatedAt = time.Now()
	req.Deadline = req.CreatedAt.Add(q.config.MaxWaitTime)
	req.ResultChan = make(chan *TranscodeResult, 1)

	// Try to enqueue with timeout
	select {
	case q.queues[req.Priority] <- req:
		queueSize.WithLabelValues(req.Priority.String()).Inc()
		q.logger.Debug().
			Str("id", req.ID).
			Str("stream_id", req.StreamID).
			Str("priority", req.Priority.String()).
			Msg("request queued")
		return nil

	case <-time.After(1 * time.Second):
		queueRejections.WithLabelValues("full").Inc()
		q.logger.Warn().
			Str("id", req.ID).
			Str("priority", req.Priority.String()).
			Msg("queue full, request rejected")
		return errors.New("queue full")

	case <-q.ctx.Done():
		return errors.New("queue shutting down")
	}
}

// processRequest processes a single transcode request
func (q *Queue) processRequest(req *TranscodeRequest, priority Priority) {
	defer func() {
		<-q.workerSem // Release worker slot
		activeWorkers.Dec()
		queueSize.WithLabelValues(priority.String()).Dec()
	}()

	// Record queue wait time
	waitTime := time.Since(req.CreatedAt)
	queueWaitTime.WithLabelValues(priority.String()).Observe(waitTime.Seconds())

	q.logger.Debug().
		Str("id", req.ID).
		Str("stream_id", req.StreamID).
		Str("priority", priority.String()).
		Dur("wait_time", waitTime).
		Msg("processing transcode request")

	// Execute transcode with timeout
	ctx, cancel := context.WithTimeout(q.ctx, req.Deadline.Sub(time.Now()))
	defer cancel()

	var result *TranscodeResult
	if q.transcoder != nil {
		var err error
		result, err = q.transcoder(ctx, req)
		if err != nil {
			result = &TranscodeResult{Error: err}
		}
	} else {
		// No transcoder configured (testing mode)
		result = &TranscodeResult{
			Data: req.Data, // Echo back input data
		}
	}

	// Send result
	select {
	case req.ResultChan <- result:
		q.logger.Debug().
			Str("id", req.ID).
			Bool("success", result.Error == nil).
			Msg("transcode completed")
	case <-ctx.Done():
		q.logger.Warn().
			Str("id", req.ID).
			Msg("result channel closed or timeout")
	}

	close(req.ResultChan)
}

// Stats returns current queue statistics
func (q *Queue) Stats() map[string]interface{} {
	stats := make(map[string]interface{})

	for priority := PriorityPreview; priority <= PriorityActive; priority++ {
		stats[priority.String()+"_queue_size"] = len(q.queues[priority])
	}

	stats["active_workers"] = q.config.Workers - len(q.workerSem)
	stats["max_workers"] = q.config.Workers

	return stats
}
