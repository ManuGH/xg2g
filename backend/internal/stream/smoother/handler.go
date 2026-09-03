// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// FlusherWriter wraps an http.ResponseWriter and http.Flusher.
type FlusherWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// Write writes data and immediately flushes the TCP socket.
func (fw *FlusherWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err == nil && fw.flusher != nil {
		fw.flusher.Flush()
	}
	return n, err
}

// Handler serves paced, smoothed TS streams from the unified Live Ingest Pipeline.
type Handler struct {
	manager      *session.Manager
	receiverHost string
	streamPort   int
	cfg          Config
	client       *http.Client
}

// AttachablePipeline provides subscriber attachment into the active live ingest session.
type AttachablePipeline interface {
	PrimedAttachWithTimeout(ctx context.Context, timeout time.Duration) (ring.PrimedAttachPoint, *ring.SubscriberReader, error)
}

// NewHandler creates a new TS smoothing HTTP handler without a session manager (fail-closed in production).
func NewHandler(receiverBaseURL string, streamPort int, cfg Config) *Handler {
	return NewHandlerWithManager(nil, receiverBaseURL, streamPort, cfg)
}

// NewHandlerWithManager creates a TS smoothing HTTP handler backed by the unified session.Manager.
func NewHandlerWithManager(manager *session.Manager, receiverBaseURL string, streamPort int, cfg Config) *Handler {
	host := "10.10.55.64"
	if receiverBaseURL != "" {
		if u, err := url.Parse(receiverBaseURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	}
	if streamPort <= 0 {
		streamPort = 8001
	}

	return &Handler{
		manager:      manager,
		receiverHost: host,
		streamPort:   streamPort,
		cfg:          cfg,
		client: &http.Client{
			Timeout: 0, // continuous streaming
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ServeHTTP handles GET /api/v3/stream/smooth/* requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract service reference from wildcard or query param
	path := r.URL.Path
	const prefix = "/api/v3/stream/smooth/"
	serviceRef := strings.TrimPrefix(path, prefix)
	if serviceRef == "" || serviceRef == path {
		serviceRef = r.URL.Query().Get("sref")
	}

	if serviceRef == "" {
		http.Error(w, "missing serviceRef in stream path", http.StatusBadRequest)
		return
	}

	// Clean/unescape serviceRef
	if unescaped, err := url.PathUnescape(serviceRef); err == nil {
		serviceRef = unescaped
	}

	// Canonical Live Ref Validation (strictly forbid path traversal, slashes, control chars, query modifiers)
	if err := recordings.ValidateLiveRef(serviceRef); err != nil {
		http.Error(w, fmt.Sprintf("invalid serviceRef: %v", err), http.StatusBadRequest)
		return
	}

	// Smoother MUST be backed by the shared session manager to prevent unmanaged dials and tuner exhaustion.
	if h.manager == nil {
		http.Error(w, "streaming service unavailable: session manager not configured", http.StatusServiceUnavailable)
		return
	}

	key := session.NewSessionKey(h.receiverHost, h.streamPort, serviceRef)
	parts := strings.Split(serviceRef, ":")
	if len(parts) >= 4 {
		if val, err := strconv.ParseUint(parts[3], 16, 16); err == nil && val > 0 {
			key.TargetProgram = uint16(val)
		}
	}
	if err := key.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid serviceRef: %v", err), http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported by response writer", http.StatusInternalServerError)
		return
	}

	logger := log.L().With().
		Str("serviceRef", key.ServiceRef).
		Uint16("targetProgram", key.TargetProgram).
		Float64("reservoirMs", h.cfg.StartupReservoirMs).
		Logger()

	logger.Info().Msg("starting smoothed TS stream session via shared ingest")

	// Acquire or coalesce live session lease (hardware tuner admission & session sharing)
	lease, err := h.manager.Acquire(r.Context(), key)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to acquire live ingest lease for smoother")
		http.Error(w, fmt.Sprintf("upstream stream unavailable: %v", err), http.StatusBadGateway)
		return
	}
	defer lease.Release()

	pipelinePayload := lease.Session().Payload()
	if pipelinePayload == nil {
		logger.Error().Msg("active session holds nil pipeline payload")
		http.Error(w, "internal pipeline error", http.StatusInternalServerError)
		return
	}

	pipe, ok := pipelinePayload.(AttachablePipeline)
	if !ok {
		logger.Error().Msg("invalid pipeline payload type in session")
		http.Error(w, "internal pipeline type error", http.StatusInternalServerError)
		return
	}

	_, reader, err := pipe.PrimedAttachWithTimeout(r.Context(), 5*time.Second)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to attach to live stream pipeline for smoother")
		http.Error(w, fmt.Sprintf("stream attach failed: %v", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = reader.Close() }()

	// Prepare streaming response headers
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")
	w.Header().Set("X-Smoother-Reservoir-Ms", fmt.Sprintf("%.0f", h.cfg.StartupReservoirMs))
	w.WriteHeader(http.StatusOK)

	var outWriter io.Writer = w
	if flusher != nil {
		flusher.Flush()
		outWriter = &FlusherWriter{w: w, flusher: flusher}
	}

	report, err := SmoothStream(r.Context(), reader, outWriter, h.cfg)
	if err != nil && r.Context().Err() == nil {
		logger.Warn().Err(err).Msg("smoothed stream terminated with error")
	} else if report != nil {
		logger.Info().
			Float64("durationSec", report.DurationSeconds).
			Int64("packetsOut", report.OutputPackets).
			Int64("underruns", report.Underruns).
			Float64("firstByteDelayMs", report.FirstByteDelayMs).
			Float64("steadyStateLagMs", report.SteadyStateDelayMs).
			Msg("smoothed TS session finished cleanly")
	}
}
