// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// PipelineStreamWrapper wraps the active SessionPipeline as an io.ReadCloser and session.PipelineHolder.
type PipelineStreamWrapper struct {
	pipeline *SessionPipeline
}

func (w *PipelineStreamWrapper) Read(p []byte) (int, error) {
	<-w.pipeline.Done()
	return 0, io.EOF
}

func (w *PipelineStreamWrapper) Close() error {
	w.pipeline.Close()
	return nil
}

func (w *PipelineStreamWrapper) Pipeline() any {
	return w.pipeline
}

func (w *PipelineStreamWrapper) OnDone(callback func(err error)) {
	w.pipeline.OnDone(callback)
}

// DialFunc connects to an upstream source for the specified session key.
type DialFunc func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error)

// ConnectorConfig configures the live ingest pipeline connector.
type ConnectorConfig struct {
	ReceiverBaseURL string
	StreamPort      int
	NormConfig      normalizer.Config
	RingCapacity    int
	DialFn          DialFunc // Optional custom dialer (for testing or proxying)
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
func (c *LivePipelineConnector) Connect(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
	var upstream io.ReadCloser
	var err error

	if c.cfg.DialFn != nil {
		upstream, err = c.cfg.DialFn(ctx, key)
		if err != nil {
			return nil, err
		}
	} else {
		upstream, err = c.dialHTTP(ctx, key)
		if err != nil {
			return nil, err
		}
	}

	pipeline, err := NewSessionPipeline(c.cfg.NormConfig, c.cfg.RingCapacity, key.TargetProgram)
	if err != nil {
		_ = upstream.Close()
		return nil, err
	}

	pipeline.Start(context.Background(), upstream)

	return &PipelineStreamWrapper{pipeline: pipeline}, nil
}

func (c *LivePipelineConnector) dialHTTP(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
	host := "10.10.55.64"
	if c.cfg.ReceiverBaseURL != "" {
		if u, err := url.Parse(c.cfg.ReceiverBaseURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	}

	targetURL := fmt.Sprintf("http://%s:%d/%s", host, c.cfg.StreamPort, key.ServiceRef)
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
