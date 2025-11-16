// Package proxy provides HLS streaming support for iOS devices.
package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	// DefaultHLSIdleTimeout is the default timeout for idle HLS streams before cleanup
	DefaultHLSIdleTimeout = 60 * time.Second

	// DefaultHLSCleanupInterval is the default interval for checking idle streams
	DefaultHLSCleanupInterval = 30 * time.Second
)

// HLSStreamer manages HLS segmentation for a single stream.
type HLSStreamer struct {
	serviceRef string
	targetURL  string
	outputDir  string
	cmd        *exec.Cmd
	ctx        context.Context
	cancel     context.CancelFunc
	logger     zerolog.Logger
	lastAccess time.Time
	mu         sync.RWMutex
	started    bool
}

// HLSManager manages multiple HLS streams.
type HLSManager struct {
	streams       map[string]*HLSStreamer
	mu            sync.RWMutex
	logger        zerolog.Logger
	outputBase    string
	cleanup       *time.Ticker
	stopChan      chan struct{}
	shutdownOnce  sync.Once
	idleTimeout   time.Duration // Configurable idle timeout for stream cleanup
	cleanupTicker time.Duration // Configurable cleanup interval
}

// NewHLSManager creates a new HLS stream manager.
func NewHLSManager(logger zerolog.Logger, outputDir string) (*HLSManager, error) {
	if outputDir == "" {
		outputDir = filepath.Join(os.TempDir(), "xg2g-hls")
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create HLS output directory: %w", err)
	}

	m := &HLSManager{
		streams:       make(map[string]*HLSStreamer),
		logger:        logger,
		outputBase:    outputDir,
		cleanup:       time.NewTicker(DefaultHLSCleanupInterval),
		stopChan:      make(chan struct{}),
		idleTimeout:   DefaultHLSIdleTimeout,
		cleanupTicker: DefaultHLSCleanupInterval,
	}

	// Start cleanup goroutine
	go m.cleanupRoutine()

	return m, nil
}

// GetOrCreateStream gets an existing stream or creates a new one.
func (m *HLSManager) GetOrCreateStream(serviceRef, targetURL string) (*HLSStreamer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if stream already exists
	if stream, ok := m.streams[serviceRef]; ok {
		stream.updateAccess()
		return stream, nil
	}

	// Create new stream
	stream, err := m.createStream(serviceRef, targetURL)
	if err != nil {
		return nil, err
	}

	m.streams[serviceRef] = stream
	return stream, nil
}

// createStream creates a new HLS streamer for a service reference.
func (m *HLSManager) createStream(serviceRef, targetURL string) (*HLSStreamer, error) {
	// Create output directory for this stream
	streamID := sanitizeServiceRef(serviceRef)
	outputDir := filepath.Join(m.outputBase, streamID)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create stream output directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream := &HLSStreamer{
		serviceRef: serviceRef,
		targetURL:  targetURL,
		outputDir:  outputDir,
		ctx:        ctx,
		cancel:     cancel,
		logger:     m.logger.With().Str("service_ref", serviceRef).Logger(),
		lastAccess: time.Now(),
	}

	return stream, nil
}

// Start starts the HLS segmentation process.
func (s *HLSStreamer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	playlistPath := filepath.Join(s.outputDir, "playlist.m3u8")
	segmentPattern := filepath.Join(s.outputDir, "segment_%03d.ts")

	// ffmpeg command to convert MPEG-TS to HLS
	// -c copy: No re-encoding (audio already AAC from Rust)
	// -f hls: HLS output format
	// -hls_time 2: 2-second segments (low latency)
	// -hls_list_size 6: Keep last 6 segments (12 seconds buffer)
	// -hls_flags: delete_segments (auto cleanup) + append_list (continuous stream)
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-i", s.targetURL,
		"-c", "copy",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "6",
		"-hls_flags", "delete_segments+append_list",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	}

	s.cmd = exec.CommandContext(s.ctx, "ffmpeg", args...)
	s.cmd.Stdout = nil

	// Capture ffmpeg stderr for debugging
	stderrPipe, err := s.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	// Monitor ffmpeg stderr in background
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		// Increase buffer for long ffmpeg lines
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			s.logger.Debug().
				Str("service_ref", s.serviceRef).
				Str("ffmpeg", scanner.Text()).
				Msg("hls ffmpeg stderr")
		}

		if err := scanner.Err(); err != nil {
			s.logger.Debug().
				Err(err).
				Str("service_ref", s.serviceRef).
				Msg("ffmpeg stderr scanner error")
		}
	}()

	s.logger.Info().
		Str("service_ref", s.serviceRef).
		Str("target", s.targetURL).
		Str("output", playlistPath).
		Msg("starting HLS segmentation")

	if err := s.cmd.Start(); err != nil {
		// Cleanup output directory on start failure
		_ = os.RemoveAll(s.outputDir)
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	s.started = true

	// Monitor process in background
	go func() {
		if err := s.cmd.Wait(); err != nil {
			if s.ctx.Err() == nil {
				s.logger.Error().
					Err(err).
					Str("service_ref", s.serviceRef).
					Msg("HLS segmentation failed")
			}
		}
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
	}()

	// Wait a bit for first segments to be created
	// TODO: Replace with waitForPlaylist(ctx) with timeout
	time.Sleep(500 * time.Millisecond)

	return nil
}

// Stop stops the HLS segmentation process.
func (s *HLSStreamer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	s.logger.Info().Msg("stopping HLS segmentation")
	s.cancel()

	// Clean up output directory
	if err := os.RemoveAll(s.outputDir); err != nil {
		s.logger.Warn().Err(err).Msg("failed to clean up HLS directory")
	}
}

// GetPlaylistPath returns the path to the HLS playlist file.
func (s *HLSStreamer) GetPlaylistPath() string {
	return filepath.Join(s.outputDir, "playlist.m3u8")
}

// GetOutputDir returns the output directory for this stream.
func (s *HLSStreamer) GetOutputDir() string {
	return s.outputDir
}

// updateAccess updates the last access time.
func (s *HLSStreamer) updateAccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAccess = time.Now()
}

// isIdle checks if the stream has been idle for too long.
func (s *HLSStreamer) isIdle(timeout time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.lastAccess) > timeout
}

// cleanupRoutine periodically cleans up idle streams.
func (m *HLSManager) cleanupRoutine() {
	for {
		select {
		case <-m.cleanup.C:
			m.cleanupIdleStreams()
		case <-m.stopChan:
			return
		}
	}
}

// cleanupIdleStreams removes streams that haven't been accessed recently.
func (m *HLSManager) cleanupIdleStreams() {
	// Collect idle streams under lock, cleanup outside lock to avoid blocking
	m.mu.Lock()
	toCleanup := make([]*HLSStreamer, 0)

	for ref, stream := range m.streams {
		if stream.isIdle(m.idleTimeout) {
			m.logger.Info().
				Str("service_ref", ref).
				Msg("removing idle HLS stream")
			toCleanup = append(toCleanup, stream)
			delete(m.streams, ref)
		}
	}
	m.mu.Unlock()

	// Cleanup streams outside of manager lock to avoid blocking new requests
	for _, stream := range toCleanup {
		stream.Stop()
	}
}

// Shutdown stops all streams and cleanup.
// Safe to call multiple times (idempotent).
func (m *HLSManager) Shutdown() {
	m.shutdownOnce.Do(func() {
		// Signal cleanup goroutine and stop ticker outside of lock
		close(m.stopChan)
		m.cleanup.Stop()

		// Collect streams under lock, stop them outside lock to avoid blocking
		m.mu.Lock()
		streams := make([]*HLSStreamer, 0, len(m.streams))
		for _, stream := range m.streams {
			streams = append(streams, stream)
		}
		m.streams = make(map[string]*HLSStreamer)
		m.mu.Unlock()

		// Stop all streams outside of lock
		for _, stream := range streams {
			stream.Stop()
		}
	})
}

// ServeHLS handles HLS playlist and segment requests.
func (m *HLSManager) ServeHLS(w http.ResponseWriter, r *http.Request, serviceRef, targetURL string) error {
	// Get or create stream
	stream, err := m.GetOrCreateStream(serviceRef, targetURL)
	if err != nil {
		return fmt.Errorf("get stream: %w", err)
	}

	// Start segmentation if not already started
	if err := stream.Start(); err != nil {
		return fmt.Errorf("start stream: %w", err)
	}

	// Determine what to serve (playlist or segment)
	path := r.URL.Path

	if strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, "/hls") {
		// Serve playlist
		return m.servePlaylist(w, stream)
	} else if strings.Contains(path, "segment_") && strings.HasSuffix(path, ".ts") {
		// Serve segment
		segmentName := filepath.Base(path)
		return m.serveSegment(w, stream, segmentName)
	}

	// Default: serve playlist
	return m.servePlaylist(w, stream)
}

// servePlaylist serves the HLS playlist file.
func (m *HLSManager) servePlaylist(w http.ResponseWriter, stream *HLSStreamer) error {
	playlistPath := stream.GetPlaylistPath()

	// Wait for playlist to exist
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(playlistPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Read playlist
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write playlist: %w", err)
	}

	return nil
}

// serveSegment serves an HLS segment file.
func (m *HLSManager) serveSegment(w http.ResponseWriter, stream *HLSStreamer, segmentName string) error {
	segmentPath := filepath.Join(stream.GetOutputDir(), segmentName)

	// Wait for segment to exist
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(segmentPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Open segment file
	file, err := os.Open(segmentPath)
	if err != nil {
		return fmt.Errorf("open segment: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.logger.Warn().Err(err).Msg("failed to close segment file")
		}
	}()

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "max-age=10")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		return fmt.Errorf("write segment: %w", err)
	}

	return nil
}

// sanitizeServiceRef converts a service reference to a safe directory name.
func sanitizeServiceRef(ref string) string {
	// Remove colons and replace with underscores
	safe := strings.ReplaceAll(ref, ":", "_")
	// Remove any other problematic characters
	safe = strings.ReplaceAll(safe, "/", "_")
	return safe
}
