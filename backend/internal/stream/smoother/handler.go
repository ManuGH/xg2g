// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
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

// Handler serves paced, smoothed TS streams from the upstream Enigma2 receiver.
type Handler struct {
	receiverHost string
	streamPort   int
	cfg          Config
}

// NewHandler creates a new TS smoothing HTTP handler.
func NewHandler(receiverBaseURL string, streamPort int, cfg Config) *Handler {
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
		receiverHost: host,
		streamPort:   streamPort,
		cfg:          cfg,
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

	targetURL := fmt.Sprintf("http://%s:%d/%s", h.receiverHost, h.streamPort, serviceRef)
	logger := log.L().With().
		Str("serviceRef", serviceRef).
		Str("targetURL", targetURL).
		Float64("reservoirMs", h.cfg.StartupReservoirMs).
		Logger()

	logger.Info().Msg("starting smoothed TS stream session")

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create upstream request: %v", err), http.StatusInternalServerError)
		return
	}

	client := &http.Client{
		Timeout: 0, // continuous streaming
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to upstream receiver")
		http.Error(w, fmt.Sprintf("upstream receiver unavailable: %v", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		logger.Warn().Int("status", resp.StatusCode).Msg("upstream receiver returned non-200")
		http.Error(w, fmt.Sprintf("upstream error: %d %s", resp.StatusCode, resp.Status), resp.StatusCode)
		return
	}

	// Prepare streaming response headers
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")
	w.Header().Set("X-Smoother-Reservoir-Ms", fmt.Sprintf("%.0f", h.cfg.StartupReservoirMs))
	w.WriteHeader(http.StatusOK)

	var outWriter io.Writer = w
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
		outWriter = &FlusherWriter{w: w, flusher: flusher}
	}

	report, err := SmoothStream(r.Context(), resp.Body, outWriter, h.cfg)
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
