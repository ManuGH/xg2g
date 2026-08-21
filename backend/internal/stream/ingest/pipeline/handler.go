// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// Handler serves live, paced MPEG-TS streams via HTTP using the unified Live Ingest Pipeline.
type Handler struct {
	manager      *session.Manager
	receiverHost string
	streamPort   int
}

// NewHandler creates an HTTP handler bound to the session.Manager.
func NewHandler(manager *session.Manager) *Handler {
	return NewHandlerWithReceiver(manager, "10.10.55.64", 8001)
}

// NewHandlerWithReceiver creates an HTTP handler with explicit receiver host/port defaults.
func NewHandlerWithReceiver(manager *session.Manager, receiverHost string, streamPort int) *Handler {
	if receiverHost == "" {
		receiverHost = "10.10.55.64"
	}
	if streamPort <= 0 {
		streamPort = 8001
	}
	return &Handler{
		manager:      manager,
		receiverHost: receiverHost,
		streamPort:   streamPort,
	}
}

// ServeHTTP handles GET /api/v3/stream/live/* requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	const prefix = "/api/v3/stream/live/"
	serviceRef := strings.TrimPrefix(path, prefix)
	if serviceRef == "" || serviceRef == path {
		serviceRef = r.URL.Query().Get("sref")
	}

	if serviceRef == "" {
		http.Error(w, "missing serviceRef in stream path", http.StatusBadRequest)
		return
	}

	if unescaped, err := url.PathUnescape(serviceRef); err == nil {
		serviceRef = unescaped
	}

	key := session.NewSessionKey(h.receiverHost, h.streamPort, serviceRef)
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
		Logger()

	// Acquire or coalesce live session lease
	lease, err := h.manager.Acquire(r.Context(), key)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to acquire live ingest lease")
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

	pipe, ok := pipelinePayload.(*SessionPipeline)
	if !ok {
		logger.Error().Msg("invalid pipeline payload type in session")
		http.Error(w, "internal pipeline type error", http.StatusInternalServerError)
		return
	}

	// Capture atomic PrimedAttachPoint and dedicated subscriber reader (waits for first keyframe if stream just started)
	attach, reader, err := pipe.PrimedAttachWithTimeout(r.Context(), 3*time.Second)
	if err != nil {
		runErr := pipe.Err()
		logger.Warn().Err(err).AnErr("runErr", runErr).Msg("failed to perform primed attach to stream")
		http.Error(w, fmt.Sprintf("failed to attach to live stream: %v (runErr: %v)", err, runErr), http.StatusBadGateway)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// 1. Deliver authoritative PAT/PMT preamble first
	if len(attach.Preamble) > 0 {
		if _, err := w.Write(attach.Preamble); err != nil {
			return
		}
		flusher.Flush()
	}

	// 2. Stream time-regulated packets from subscriber reader
	chunk := make([]byte, 32*1024)
	for {
		if err := r.Context().Err(); err != nil {
			return
		}

		n, err := reader.Read(chunk)
		if n > 0 {
			if _, writeErr := w.Write(chunk[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}

		if err != nil {
			if err == io.EOF {
				return
			}
			logger.Debug().Err(err).Msg("subscriber reader exited")
			return
		}
	}
}
