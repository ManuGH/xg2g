// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

var (
	// ErrAdmissionDenied indicates that the physical tuner topology rejected stream admission.
	ErrAdmissionDenied = errors.New("tuner topology admission denied")
)

// TopologyLease represents a physical tuner / demodulator reservation lease.
type TopologyLease interface {
	Release()
	Decision() receivertopology.AllocationDecision
}

// TopologyService defines the interface for hardware tuner admission and transponder multiplex allocation.
type TopologyService interface {
	ReserveStreamLeaseAtomic(serviceRef string, sessionID string, priority receivertopology.Priority, ttl time.Duration) (*receivertopology.Lease, receivertopology.AllocationDecision, error)
	ReleaseStream(sessionID string) bool
}

type topologyLeaseWrapper struct {
	service   TopologyService
	sessionID string
	decision  receivertopology.AllocationDecision
	released  atomic.Bool
}

func (l *topologyLeaseWrapper) Release() {
	if l.released.CompareAndSwap(false, true) {
		if l.service != nil {
			l.service.ReleaseStream(l.sessionID)
		}
	}
}

func (l *topologyLeaseWrapper) Decision() receivertopology.AllocationDecision {
	return l.decision
}

// PipelineStreamWrapper wraps the active SessionPipeline as an io.ReadCloser and session.PipelineHolder.
type PipelineStreamWrapper struct {
	pipeline  *SessionPipeline
	topLease  TopologyLease
	closeOnce sync.Once
}

func (w *PipelineStreamWrapper) Read(p []byte) (int, error) {
	<-w.pipeline.Done()
	return 0, io.EOF
}

func (w *PipelineStreamWrapper) Close() error {
	w.closeOnce.Do(func() {
		w.pipeline.Close()
		if w.topLease != nil {
			w.topLease.Release()
		}
	})
	return nil
}

func (w *PipelineStreamWrapper) Pipeline() any {
	return w.pipeline
}

func (w *PipelineStreamWrapper) OnDone(callback func(err error)) {
	w.pipeline.OnDone(callback)
}

func (w *PipelineStreamWrapper) TopologyLease() TopologyLease {
	return w.topLease
}

// DialFunc connects to an upstream source for the specified session key.
type DialFunc func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error)

// ConnectorConfig configures the live ingest pipeline connector.
type ConnectorConfig struct {
	ReceiverBaseURL string
	StreamPort      int
	NormConfig      normalizer.Config
	RingCapacity    int
	TopologyService TopologyService // Optional (if nil, topology admission is skipped / pass-through)
	DialFn          DialFunc        // Optional custom dialer (for testing or proxying)
}

// DefaultConnectorConfig returns production defaults for pipeline connector.
func DefaultConnectorConfig(receiverBaseURL string, streamPort int) ConnectorConfig {
	if streamPort <= 0 {
		streamPort = 8001
	}
	return ConnectorConfig{
		ReceiverBaseURL: receiverBaseURL,
		StreamPort:      streamPort,
		NormConfig:      normalizer.DefaultConfig(),
		RingCapacity:    20000 * ring.TSPacketSize, // ~3.76 MB (approx 6-8 seconds buffer)
	}
}

// LivePipelineConnector implements session.UpstreamConnector.
type LivePipelineConnector struct {
	cfg        ConnectorConfig
	httpClient *http.Client
}

// NewLivePipelineConnector creates a new connector for live broadcast pipelines.
func NewLivePipelineConnector(cfg ConnectorConfig) *LivePipelineConnector {
	if cfg.RingCapacity < 5*ring.TSPacketSize {
		cfg.RingCapacity = 20000 * ring.TSPacketSize
	}
	return &LivePipelineConnector{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 0, // Continuous streaming
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       30 * time.Second,
			},
		},
	}
}

// Connect dials the upstream tuner and initializes the SessionPipeline.
// It executes the strict sequence: Topology Admission -> Upstream Dial -> SessionPipeline Start.
func (c *LivePipelineConnector) Connect(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
	// 1. Check & reserve topology lease BEFORE dialing upstream
	var topLease TopologyLease
	if c.cfg.TopologyService != nil {
		sessionID := fmt.Sprintf("live-ingest:%s", key.String())
		_, decision, err := c.cfg.TopologyService.ReserveStreamLeaseAtomic(key.ServiceRef, sessionID, receivertopology.PriorityLive, 0)
		if err != nil || !decision.Allowed {
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrAdmissionDenied, err)
			}
			return nil, fmt.Errorf("%w: %s", ErrAdmissionDenied, decision.Reason)
		}
		topLease = &topologyLeaseWrapper{
			service:   c.cfg.TopologyService,
			sessionID: sessionID,
			decision:  decision,
		}
	}

	// 2. Dial upstream
	var upstream io.ReadCloser
	var err error
	if c.cfg.DialFn != nil {
		upstream, err = c.cfg.DialFn(ctx, key)
	} else {
		upstream, err = c.dialHTTP(ctx, key)
	}
	if err != nil {
		if topLease != nil {
			topLease.Release() // Release lease immediately if dial fails
		}
		return nil, err
	}

	// 3. Create and start SessionPipeline
	pipeline, err := NewSessionPipeline(c.cfg.NormConfig, c.cfg.RingCapacity, key.TargetProgram)
	if err != nil {
		_ = upstream.Close()
		if topLease != nil {
			topLease.Release()
		}
		return nil, err
	}

	pipeline.Start(context.Background(), upstream)

	return &PipelineStreamWrapper{
		pipeline: pipeline,
		topLease: topLease,
	}, nil
}

func (c *LivePipelineConnector) dialHTTP(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
	targetURL := fmt.Sprintf("http://10.10.55.64:%d/%s", c.cfg.StreamPort, key.ServiceRef)
	if c.cfg.ReceiverBaseURL != "" {
		if u, err := url.Parse(c.cfg.ReceiverBaseURL); err == nil && u.Host != "" {
			if u.Port() != "" {
				scheme := u.Scheme
				if scheme == "" {
					scheme = "http"
				}
				targetURL = fmt.Sprintf("%s://%s/%s", scheme, u.Host, key.ServiceRef)
			} else {
				scheme := u.Scheme
				if scheme == "" {
					scheme = "http"
				}
				targetURL = fmt.Sprintf("%s://%s:%d/%s", scheme, u.Hostname(), c.cfg.StreamPort, key.ServiceRef)
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to upstream receiver %s: %w", targetURL, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream receiver returned HTTP %d for %s", resp.StatusCode, targetURL)
	}

	return resp.Body, nil
}
