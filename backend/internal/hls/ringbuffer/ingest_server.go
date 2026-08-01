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
	"strconv"
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

	if strings.HasSuffix(filename, ".m3u8") {
		data = injectDiscontinuities(data)
	}

	var dvrCb DVRCallback
	if s.shouldRecord != nil && s.shouldRecord(sessionID) {
		dvrCb = s.persistToDisk
	}

	serviceRef := r.Header.Get("X-Service-Ref")
	if serviceRef == "" {
		serviceRef = sessionID
	}

	buf := s.registry.GetOrCreateService(serviceRef, sessionID, dvrCb)
	buf.Put(filename, data)

	w.WriteHeader(http.StatusOK)
}

// injectDiscontinuities parses a playlist, looks for #EXT-X-PROGRAM-DATE-TIME jumps
// greater than 2x the target duration, and automatically injects #EXT-X-DISCONTINUITY
// to prevent player stalls during source signal drops.
func injectDiscontinuities(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string

	var targetDuration float64
	var lastTime time.Time

	for _, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			td, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:")), 64)
			targetDuration = td
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:") {
			tsStr := strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:"))
			t, err := time.Parse(time.RFC3339Nano, tsStr)
			if err == nil {
				if !lastTime.IsZero() && targetDuration > 0 {
					diff := t.Sub(lastTime).Seconds()
					if diff < 0 {
						diff = -diff
					}
					if diff > 2.0*targetDuration {
						if len(out) > 0 && out[len(out)-1] != "#EXT-X-DISCONTINUITY" {
							out = append(out, "#EXT-X-DISCONTINUITY")
						}
					}
				}
				lastTime = t
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
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
	filename = sanitizeIngestFilename(filename)
	if filename == "" {
		s.logger.Warn().Str("session_id", sessionID).Str("original", filename).Msg("persistToDisk: rejected path traversal")
		return
	}
	sessionDir := ports.SessionHLSDir(s.hlsRoot, sessionID)
	_ = os.MkdirAll(sessionDir, 0755) // #nosec G301
	filePath := filepath.Join(sessionDir, filename)
	tmpPath := filePath + ".tmp"

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.Error().Err(err).Str("path", filePath).Msg("async dvr write open failed")
		return
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return
	}
	_ = f.Sync()
	_ = f.Close()

	if err := os.Rename(tmpPath, filePath); err == nil {
		// Register fully committed segment in authoritative DiskSegmentStore!
		if strings.HasPrefix(filename, "seg_") && strings.HasSuffix(filename, ".ts") {
			var seq uint64
			_, _ = fmt.Sscanf(filename, "seg_%d.ts", &seq)
			now := time.Now()
			segID := SegmentID{
				ServiceRef: sessionID,
				SessionID:  sessionID,
				Kind:       SegmentKindComplete,
				Sequence:   seq,
			}
			if s.registry != nil && s.registry.DiskStore() != nil {
				s.registry.DiskStore().CommitSegment(&DiskSegment{
					ID:            segID,
					ServiceRef:    sessionID,
					SessionID:     sessionID,
					Path:          filePath,
					Sequence:      seq,
					StartWallTime: now.Add(-2 * time.Second),
					EndWallTime:   now,
					DurationSec:   2.0,
					SizeBytes:     int64(len(data)),
					State:         SegmentActive,
				})
			}
		}
	} else {
		s.logger.Error().Err(err).Str("path", filePath).Msg("async dvr write rename failed")
	}
}
