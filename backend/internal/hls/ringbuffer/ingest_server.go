// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

// IngestServer listens for HTTP PUT/POST requests from FFmpeg and stores ingested
// HLS segments and playlists in the in-memory ring buffer.
type IngestServer struct {
	listener     net.Listener
	server       *http.Server
	port         int
	hlsRoot      string
	registry     *Registry
	logger       zerolog.Logger
	shouldRecord func(sessionID string) bool
}

// NewIngestServer creates a new IngestServer. If port is 0, an OS-assigned port is used.
func NewIngestServer(port int, hlsRoot string, registry *Registry, logger zerolog.Logger, shouldRecord func(string) bool) (*IngestServer, error) {
	if registry == nil {
		registry = DefaultRegistry
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind hls ingest server on %s: %w", addr, err)
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port

	s := &IngestServer{
		listener:     ln,
		port:         actualPort,
		hlsRoot:      hlsRoot,
		registry:     registry,
		logger:       logger.With().Str("component", "hls_ingest_server").Int("port", actualPort).Logger(),
		shouldRecord: shouldRecord,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ingest/", s.handleIngest)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s, nil
}

// Start runs the HTTP server in a background goroutine.
func (s *IngestServer) Start() {
	s.logger.Info().Msg("starting in-memory hls ingest server")
	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("hls ingest server error")
		}
	}()
}

// Stop gracefully shuts down the server.
func (s *IngestServer) Stop(ctx context.Context) error {
	s.logger.Info().Msg("stopping in-memory hls ingest server")
	return s.server.Shutdown(ctx)
}

// Port returns the actual listening port of the server.
func (s *IngestServer) Port() int {
	return s.port
}

// Registry returns the underlying ringbuffer registry used by this ingest server.
func (s *IngestServer) Registry() *Registry {
	return s.registry
}

// URL returns the base ingest URL for a given session ID.
func (s *IngestServer) URL(sessionID string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/ingest/%s", s.port, sessionID)
}

func (s *IngestServer) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// URL format: /ingest/{sessionID}/{filename}
	path := strings.TrimPrefix(r.URL.Path, "/ingest/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "invalid ingest path", http.StatusBadRequest)
		return
	}

	sessionID := parts[0]
	filename := sanitizeIngestFilename(parts[1])
	if filename == "" {
		s.logger.Warn().Str("session_id", sessionID).Str("raw_filename", parts[1]).Msg("rejected ingest path traversal")
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Warn().Err(err).Str("session_id", sessionID).Str("filename", filename).Msg("failed to read ingest body")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	var dvrCb DVRCallback
	if s.shouldRecord != nil && s.shouldRecord(sessionID) {
		dvrCb = s.persistToDisk
	}

	buf := s.registry.GetOrCreate(sessionID, dvrCb)
	buf.Put(filename, data)

	w.WriteHeader(http.StatusOK)
}

// sanitizeIngestFilename rejects path-traversal filenames. It returns an empty string
// if the filename contains any path separators or parent-directory references.
func sanitizeIngestFilename(name string) string {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return ""
	}
	// Ensure the cleaned result is identical to the input (no hidden traversal).
	cleaned := filepath.Base(name)
	if cleaned != name {
		return ""
	}
	return name
}

func (s *IngestServer) persistToDisk(sessionID, filename string, data []byte) {
	if s.hlsRoot == "" {
		return
	}
	sessionDir := ports.SessionHLSDir(s.hlsRoot, sessionID)
	_ = os.MkdirAll(sessionDir, 0755) // #nosec G301
	filePath := filepath.Join(sessionDir, filename)
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err == nil { // #nosec G306
		_ = os.Rename(tmpPath, filePath)
	} else {
		s.logger.Error().Err(err).Str("path", filePath).Msg("async dvr write failed")
	}
}
