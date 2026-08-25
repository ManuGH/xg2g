// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
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

// defaultConnectTimeout bounds how long the upstream may take to accept the connection and
// return response headers before the dial is abandoned.
const defaultConnectTimeout = 5 * time.Second

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
	pipeline *SessionPipeline
	topLease TopologyLease
	// upstreamCancel tears down the upstream HTTP request. The request must outlive the
	// subscriber that triggered the dial and the connect deadline, but it stays owned by
	// this wrapper so session teardown, warm-hold expiry and manager shutdown all reach it.
	upstreamCancel context.CancelFunc
	closeOnce      sync.Once
}

func (w *PipelineStreamWrapper) Read(p []byte) (int, error) {
	<-w.pipeline.Done()
	return 0, io.EOF
}

func (w *PipelineStreamWrapper) Close() error {
	w.closeOnce.Do(func() {
		w.pipeline.Close()
		if w.upstreamCancel != nil {
			w.upstreamCancel()
		}
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
	Username        string
	Password        string
	NormConfig      normalizer.Config
	RingCapacity    int
	TopologyService TopologyService // Optional (if nil, topology admission is skipped unless RequireTopology is true)
	RequireTopology bool            // If true, missing TopologyService fails-closed immediately with ErrAdmissionDenied
	DialFn          DialFunc        // Optional custom dialer (for testing or proxying)
	// ConnectTimeout bounds the upstream connect and response-header phase. It cannot be
	// expressed through the caller's context, because that context dies with the connect
	// attempt while the body must keep streaming, so it is enforced on the transport.
	ConnectTimeout time.Duration
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
		ConnectTimeout:  defaultConnectTimeout,
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
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = defaultConnectTimeout
	}
	return &LivePipelineConnector{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 0, // Continuous streaming: no ceiling on the body
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext: (&net.Dialer{
					Timeout:   cfg.ConnectTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ResponseHeaderTimeout: cfg.ConnectTimeout,
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
	} else if c.cfg.RequireTopology {
		return nil, fmt.Errorf("%w: topology service is required for production live stream but is not configured", ErrAdmissionDenied)
	}

	// 2. Dial upstream
	var upstream io.ReadCloser
	var upstreamCancel context.CancelFunc
	var err error
	if c.cfg.DialFn != nil {
		upstream, err = c.cfg.DialFn(ctx, key)
	} else {
		upstream, upstreamCancel, err = c.dialHTTP(ctx, key)
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
		if upstreamCancel != nil {
			upstreamCancel()
		}
		if topLease != nil {
			topLease.Release()
		}
		return nil, err
	}

	// The pipeline derives its own cancellable context; the parent is deliberately detached from
	// the connect context so ingest survives the connect deadline.
	pipeline.Start(context.WithoutCancel(ctx), upstream)

	return &PipelineStreamWrapper{
		pipeline:       pipeline,
		topLease:       topLease,
		upstreamCancel: upstreamCancel,
	}, nil
}

func (c *LivePipelineConnector) dialHTTP(ctx context.Context, key session.SessionKey) (io.ReadCloser, context.CancelFunc, error) {
	host := "10.10.55.64"
	port := c.cfg.StreamPort
	if port <= 0 {
		port = 8001
	}
	scheme := "http"
	if c.cfg.ReceiverBaseURL != "" {
		if u, err := url.Parse(c.cfg.ReceiverBaseURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
			if u.Scheme != "" {
				scheme = u.Scheme
			}
			if u.Port() != "" && u.Port() != "80" && u.Port() != "443" && (c.cfg.StreamPort <= 0 || c.cfg.StreamPort == 8001) {
				if parsedPort, err := strconv.Atoi(u.Port()); err == nil {
					port = parsedPort
				}
			}
		}
	}

	targetURL := fmt.Sprintf("%s://%s:%d/%s", scheme, host, port, key.ServiceRef)

	// The shared ingest must not hang off the subscriber request that happened to trigger it,
	// nor off the connect deadline: both die long before the broadcast does. It gets its own
	// context, whose cancel is handed to the wrapper that owns the session's lifetime.
	streamCtx, streamCancel := context.WithCancel(context.WithoutCancel(ctx))

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		streamCancel()
		return nil, nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	req.Close = true
	req.Header.Set("User-Agent", "curl/8.10.1")
	req.Header.Set("Accept", "*/*")

	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		streamCancel()
		return nil, nil, fmt.Errorf("failed to connect to upstream receiver %s: %w", targetURL, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		streamCancel()
		return nil, nil, fmt.Errorf("upstream receiver returned HTTP %d for %s", resp.StatusCode, targetURL)
	}

	return resp.Body, streamCancel, nil
}
