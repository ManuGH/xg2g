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
	DefaultHLSCleanupInterval = 2 * time.Second

	// DefaultHLSPlaylistTimeout is how long we wait for the first playable segment/playlist
	DefaultHLSPlaylistTimeout = 60 * time.Second
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
	plexProfiles  map[string]*PlexProfile  // Plex/iOS-optimized profiles
	llhlsProfiles map[string]*LLHLSProfile // Low-Latency HLS profiles
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
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return nil, fmt.Errorf("create HLS output directory: %w", err)
	}

	m := &HLSManager{
		streams:       make(map[string]*HLSStreamer),
		plexProfiles:  make(map[string]*PlexProfile),
		llhlsProfiles: make(map[string]*LLHLSProfile),
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
	outputDir, err := secureJoin(m.outputBase, streamID)
	if err != nil {
		return nil, fmt.Errorf("invalid stream path: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0750); err != nil {
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

	if s.started {
		s.mu.Unlock()
		return nil
	}

	// Fresh context per session (avoid reusing canceled contexts)
	ctx, cancel := context.WithCancel(context.Background())

	// Inject stream_id into context logger for traceability
	streamID := fmt.Sprintf("hls-%s-%d", time.Now().Format("150405"), time.Now().UnixNano()%1000)
	logger := s.logger.With().Str("stream_id", streamID).Logger()
	ctx = logger.WithContext(ctx)

	// Mark stream as starting before long-running work to avoid lock contention
	s.ctx = ctx
	s.cancel = cancel
	s.logger = logger
	s.lastAccess = time.Now()
	s.started = true
	s.mu.Unlock()

	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			cancel()
			s.mu.Lock()
			s.started = false
			s.mu.Unlock()
		}
	}()

	playlistPath := filepath.Join(s.outputDir, "playlist.m3u8")
	segmentPattern := filepath.Join(s.outputDir, "segment_%03d.ts")

	// Ensure clean state by removing previous output
	// outputDir is already validated via secureJoin
	_ = os.RemoveAll(s.outputDir)
	if err := os.MkdirAll(s.outputDir, 0750); err != nil {
		return fmt.Errorf("re-create output directory: %w", err)
	}

	// ffmpeg command to convert MPEG-TS to HLS
	// -map 0:v -map 0:a: Only video and audio (exclude subtitles/other streams)
	// -c copy: No re-encoding
	// -f hls: HLS output format
	// -hls_time 2: 2-second segments (low latency)
	// -hls_list_size 6: Keep last 6 segments (12 seconds buffer)
	// -hls_flags: delete_segments (auto cleanup) + append_list (continuous stream)
	// If we are using the Web API, we must "Zap" and resolve the real stream URL manually.
	finalInputURL := s.targetURL
	webAPIURL := convertToWebAPI(s.targetURL, s.serviceRef)

	if webAPIURL != s.targetURL {
		logger.Info().Str("web_api_url", webAPIURL).Msg("attempting to resolve Web API stream (Zapping)")
		resolved, err := resolveWebAPI(webAPIURL)
		if err != nil {
			logger.Error().Err(err).Str("web_api_url", webAPIURL).Msg("failed to resolve Web API stream")
		} else {
			finalInputURL = resolved
			logger.Info().Str("resolved_url", finalInputURL).Msg("successfully resolved stream URL")
			// Give the tuner a moment to lock after zapping
			time.Sleep(1000 * time.Millisecond)
		}
	} else {
		logger.Info().Msg("using direct stream URL (no Web API detected)")
	}

	// Log stream start
	logger.Info().
		Str("event", "stream_start").
		Str("mode", "hls_generic").
		Str("input_url", sanitizeURL(s.targetURL)). // Assuming sanitizeURL is available in package (copied from transcoder)
		Msg("starting HLS session")

	startTime := time.Now()
	var exitReason string = "internal_error"
	var lastStats *FFmpegStats

	defer func() {
		event := logger.Info().
			Str("event", "stream_end").
			Str("exit_reason", exitReason).
			Dur("duration_ms", time.Since(startTime))

		if lastStats != nil {
			event.Float64("ffmpeg_last_speed", lastStats.Speed).
				Float64("ffmpeg_last_bitrate_kbps", lastStats.BitrateKBPS)
		}
		event.Msg("HLS session ended")
	}()

	logger.Info().Str("ffmpeg_input", finalInputURL).Msg("starting ffmpeg with input")

	args := []string{
		"-hide_banner",
	}
	args = append(args, logLevelArgs("info")...)
	args = append(args,
		"-err_detect", "ignore_err", // Ignore decoding errors
		"-ignore_unknown",                          // Ignore streams that fail probing
		"-fflags", "+genpts+igndts+discardcorrupt", // Regenerate PTS, ignore bad DTS, discard corrupt frames
		"-analyzeduration", "7000000", // 7s Analysis (safe for encrypted/slow-lock streams)
		"-probesize", "10000000", // 10MB: Safe for multi-program streams
		"-rw_timeout", "30000000", // 30s socket timeout
		"-reconnect", "1", "-reconnect_at_eof", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", // Robust HTTP input
		"-start_at_zero",                  // Reset timestamps to 0
		"-avoid_negative_ts", "make_zero", // Ensure no negative timestamps
		"-ss", "1.5", // SKIP GARBAGE: Drop first 1.5s of input (fixes Startbild/Green artifacts)
		"-thread_queue_size", "4096", // Increase thread queue for buffer
		"-i", finalInputURL,
		"-map", "0:v:0", // Explicitly map first video
		"-map", "0:a:0", // Explicitly map first audio
		"-af", "aresample=async=1", // Keep sync enabled
		"-c:v", "libx264", // TRANSCODE VIDEO via Software
		"-preset", "veryfast", // CPU Efficiency
		"-profile:v", "high", // High Profile (Better for HD)
		"-level", "4.1", // Standard HD Level
		"-pix_fmt", "yuv420p", // FORCE 4:2:0 for iOS compatibility
		"-crf", "18", // ULTRA QUALITY: Virtually Lossless
		"-vf", "yadif=0:-1:0", // Deinterlace
		"-g", "50", // Shorter GOP for faster keyframes (≈2s @25fps / 1s @50fps)
		"-keyint_min", "50", // Match GOP floor
		"-force_key_frames", "expr:gte(t,n_forced*2)", // Ensure keyframe every 2s for segment boundaries
		"-sc_threshold", "0", // Disable Scene Change detection (Consistent GOP)
		"-c:a", "aac", // Transcode audio to AAC
		"-profile:a", "aac_low", // Force LC-AAC (Max compatibility)
		"-ac", "2", // Stereo downmix
		"-ar", "48000", // Force 48kHz (iOS Friendly)
		"-b:a", "192k", // 192kbps audio bitrate
		"-bsf:v", "h264_mp4toannexb,dump_extra", // Extract and inject SPS/PPS headers into every keyframe
		"-f", "hls",
		"-hls_time", "2", // Faster first segment for quicker start
		"-hls_list_size", "12", // ~24s buffer
		"-hls_flags", "delete_segments+append_list+independent_segments",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	// #nosec G204 -- HLS transcoding: ffmpeg command with controlled arguments
	s.cmd = exec.CommandContext(s.ctx, "ffmpeg", args...)
	s.cmd.Stdout = nil

	// Capture ffmpeg stderr for debugging
	stderrPipe, err := s.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	// Monitor ffmpeg stderr in background
	// Monitor ffmpeg stderr in background with stats parsing
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)

	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		statsTicker := time.NewTicker(5 * time.Second)
		defer statsTicker.Stop()
		var statsSeq int

		for scanner.Scan() {
			line := scanner.Text()

			// Parse stats
			if stats := ParseFFmpegStats(line); stats != nil {
				lastStats = stats
				select {
				case <-statsTicker.C:
					statsSeq++
					logger.Info().
						Str("event", "ffmpeg_stats").
						Int("seq", statsSeq).
						Float64("speed", stats.Speed).
						Float64("bitrate_kbps", stats.BitrateKBPS).
						Msg("ffmpeg progress")
				default:
				}
			} else {
				if strings.Contains(strings.ToLower(line), "error") {
					logger.Warn().Str("stderr", line).Msg("ffmpeg warning")
				} else {
					logger.Debug().Str("stderr", line).Msg("hls ffmpeg stderr")
				}
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Debug().
				Err(err).
				Msg("ffmpeg stderr scanner error")
		}
	}()

	logger.Info().
		Str("service_ref", s.serviceRef).
		Str("target", s.targetURL).
		Str("output", playlistPath).
		Msg("starting HLS segmentation")

	if err := s.cmd.Start(); err != nil {
		// Cleanup output directory on start failure
		// outputDir is already validated via secureJoin during stream creation
		_ = os.RemoveAll(s.outputDir)
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// Monitor process in background
	// Monitor process in background
	go func() {
		defer stderrWg.Wait() // Wait for stderr logger

		if err := s.cmd.Wait(); err != nil {
			if s.ctx.Err() == nil {
				exitReason = "ffmpeg_exit"
				logger.Error().
					Err(err).
					Msg("HLS segmentation failed")
			} else {
				exitReason = "client_disconnect" // or stop called
			}
		} else {
			if s.ctx.Err() != nil {
				exitReason = "client_disconnect"
			} else {
				exitReason = "ffmpeg_exit"
			}
		}

		if exitReason == "internal_error" {
			exitReason = "success"
		}

		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
	}()

	// Wait for playlist to be created with timeout
	if err := s.waitForPlaylist(s.ctx); err != nil {
		// Cleanup on failure
		s.Stop()
		return fmt.Errorf("wait for playlist: %w", err)
	}

	cleanupOnError = false
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
	// outputDir is already validated via secureJoin during stream creation
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

// waitForPlaylist waits for the HLS playlist to be created.
func (s *HLSStreamer) waitForPlaylist(ctx context.Context) error {
	playlistPath := s.GetPlaylistPath()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Wait up to DefaultHLSPlaylistTimeout (slow/locked tuners can need >30s)
	timeout := time.After(DefaultHLSPlaylistTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for playlist creation")
		case <-ticker.C:
			// Keep stream alive while waiting (prevent idle cleanup)
			s.updateAccess()

			// check if process is still running
			s.mu.RLock()
			running := s.started
			s.mu.RUnlock()
			if !running {
				return fmt.Errorf("process exited before playlist creation")
			}

			// playlistPath is already validated via secureJoin (constructed from validated s.outputDir)
			if data, err := os.ReadFile(playlistPath); err == nil && len(data) > 0 { // #nosec G304
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					// Verify first referenced segment exists to avoid serving an empty playlist
					segmentPath, segErr := secureJoin(s.outputDir, line)
					if segErr == nil {
						if info, statErr := os.Stat(segmentPath); statErr == nil && info.Size() > 0 {
							return nil
						}
					}
				}
			}
		}
	}
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
	toCleanupPlex := make([]*PlexProfile, 0)

	// Cleanup generic HLS streams
	for ref, stream := range m.streams {
		if stream.isIdle(m.idleTimeout) {
			m.logger.Info().
				Str("service_ref", ref).
				Msg("removing idle HLS stream")
			toCleanup = append(toCleanup, stream)
			delete(m.streams, ref)
		}
	}

	// Cleanup Plex profiles
	for ref, profile := range m.plexProfiles {
		if profile.IsIdle(m.idleTimeout) {
			m.logger.Info().
				Str("service_ref", ref).
				Msg("removing idle Plex/iOS profile")
			toCleanupPlex = append(toCleanupPlex, profile)
			delete(m.plexProfiles, ref)
		}
	}

	// Cleanup LL-HLS profiles
	toCleanupLLHLS := make([]*LLHLSProfile, 0)
	for ref, profile := range m.llhlsProfiles {
		if profile.IsIdle(m.idleTimeout) {
			m.logger.Info().
				Str("service_ref", ref).
				Msg("removing idle LL-HLS profile")
			toCleanupLLHLS = append(toCleanupLLHLS, profile)
			delete(m.llhlsProfiles, ref)
		}
	}
	m.mu.Unlock()

	// Cleanup streams outside of manager lock to avoid blocking new requests
	for _, stream := range toCleanup {
		stream.Stop()
	}

	// Cleanup Plex profiles
	for _, profile := range toCleanupPlex {
		profile.Stop()
	}

	// Cleanup LL-HLS profiles
	for _, profile := range toCleanupLLHLS {
		profile.Stop()
	}
}

// Shutdown stops all streams and cleanup.
// Safe to call multiple times (idempotent).
func (m *HLSManager) Shutdown() {
	m.shutdownOnce.Do(func() {
		// Signal cleanup goroutine and stop ticker outside of lock
		close(m.stopChan)
		m.cleanup.Stop()

		// Collect streams and profiles under lock, stop them outside lock to avoid blocking
		m.mu.Lock()
		streams := make([]*HLSStreamer, 0, len(m.streams))
		for _, stream := range m.streams {
			streams = append(streams, stream)
		}
		profiles := make([]*PlexProfile, 0, len(m.plexProfiles))
		for _, profile := range m.plexProfiles {
			profiles = append(profiles, profile)
		}
		llhlsProfiles := make([]*LLHLSProfile, 0, len(m.llhlsProfiles))
		for _, profile := range m.llhlsProfiles {
			llhlsProfiles = append(llhlsProfiles, profile)
		}
		m.streams = make(map[string]*HLSStreamer)
		m.plexProfiles = make(map[string]*PlexProfile)
		m.llhlsProfiles = make(map[string]*LLHLSProfile)
		m.mu.Unlock()

		// Stop all streams outside of lock
		for _, stream := range streams {
			stream.Stop()
		}

		// Stop all Plex profiles outside of lock
		for _, profile := range profiles {
			profile.Stop()
		}

		// Stop all LL-HLS profiles outside of lock
		for _, profile := range llhlsProfiles {
			profile.Stop()
		}
	})
}

// GetOrCreatePlexProfile gets an existing Plex profile or creates a new one.
func (m *HLSManager) GetOrCreatePlexProfile(serviceRef, targetURL string) (*PlexProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if profile already exists
	if profile, ok := m.plexProfiles[serviceRef]; ok {
		profile.UpdateAccess()
		return profile, nil
	}

	// Create new Plex profile
	config := GetPlexProfileConfig()
	profile, err := NewPlexProfile(serviceRef, targetURL, m.outputBase, m.logger, config)
	if err != nil {
		return nil, err
	}

	m.plexProfiles[serviceRef] = profile
	return profile, nil
}

// GetOrCreateLLHLSProfile gets an existing LL-HLS profile or creates a new one.
func (m *HLSManager) GetOrCreateLLHLSProfile(serviceRef, targetURL string) (*LLHLSProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if profile already exists
	if profile, ok := m.llhlsProfiles[serviceRef]; ok {
		profile.UpdateAccess()
		return profile, nil
	}

	// Create new LL-HLS profile
	config := GetLLHLSConfig()
	profile, err := NewLLHLSProfile(serviceRef, targetURL, m.outputBase, m.logger, config)
	if err != nil {
		return nil, err
	}

	m.llhlsProfiles[serviceRef] = profile
	return profile, nil
}

// ServeHLS handles HLS playlist and segment requests.
// Routes to appropriate HLS profile based on User-Agent:
//   - Plex clients → Plex/iOS profile (classical HLS, mpegts)
//   - Native Apple clients → LL-HLS profile (fmp4, low latency)
//   - Others → Generic HLS
func (m *HLSManager) ServeHLS(w http.ResponseWriter, r *http.Request, serviceRef, targetURL string) error {
	userAgent := r.Header.Get("User-Agent")

	// Priority 1: Plex clients (classical HLS for maximum compatibility)
	if ShouldUsePlexProfile(userAgent) {
		return m.servePlexHLS(w, r, serviceRef, targetURL)
	}

	// Priority 2: Native Apple clients (LL-HLS for low latency)
	if IsNativeAppleClient(userAgent) {
		return m.serveLLHLS(w, r, serviceRef, targetURL)
	}

	// Fallback: Generic HLS for all other clients
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

// servePlexHLS serves HLS using the Plex/iOS-optimized profile.
func (m *HLSManager) servePlexHLS(w http.ResponseWriter, r *http.Request, serviceRef, targetURL string) error {
	// Get or create Plex profile
	profile, err := m.GetOrCreatePlexProfile(serviceRef, targetURL)
	if err != nil {
		return fmt.Errorf("get plex profile: %w", err)
	}

	// Start profile if not already started
	if err := profile.Start(true, "192k"); err != nil {
		return fmt.Errorf("start plex profile: %w", err)
	}

	// Wait for stream to be ready (initial segments available)
	if err := profile.WaitReady(30 * time.Second); err != nil {
		return fmt.Errorf("plex profile not ready: %w", err)
	}

	// Determine what to serve
	path := r.URL.Path

	if strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, "/hls") {
		// Serve Plex-optimized playlist
		return profile.ServePlaylist(w)
	} else if strings.Contains(path, "segment_") && strings.HasSuffix(path, ".ts") {
		// Serve segment
		segmentName := filepath.Base(path)
		return profile.ServeSegment(w, segmentName)
	}

	// Default: serve playlist
	return profile.ServePlaylist(w)
}

// serveLLHLS serves HLS using the Low-Latency HLS profile.
func (m *HLSManager) serveLLHLS(w http.ResponseWriter, r *http.Request, serviceRef, targetURL string) error {
	// Get or create LL-HLS profile
	profile, err := m.GetOrCreateLLHLSProfile(serviceRef, targetURL)
	if err != nil {
		return fmt.Errorf("get llhls profile: %w", err)
	}

	// Start profile if not already started
	if err := profile.Start(true, "192k"); err != nil {
		return fmt.Errorf("start llhls profile: %w", err)
	}

	// Wait for stream to be ready (initial segments available)
	if err := profile.WaitReady(30 * time.Second); err != nil {
		return fmt.Errorf("llhls profile not ready: %w", err)
	}

	// Determine what to serve
	path := r.URL.Path

	if strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, "/hls") {
		// Serve LL-HLS playlist
		return profile.ServePlaylist(w)
	} else if strings.HasSuffix(path, ".m4s") || strings.HasSuffix(path, "init.mp4") {
		// Serve fmp4 segment or init segment
		segmentName := filepath.Base(path)
		return profile.ServeSegment(w, segmentName)
	}

	// Default: serve playlist
	return profile.ServePlaylist(w)
}

// ServeSegmentFromAnyStream finds and serves a segment from any active stream.
// This handles the case where Safari requests segments with relative paths like "/segment_043.ts"
// without the /hls/ prefix.
func (m *HLSManager) ServeSegmentFromAnyStream(w http.ResponseWriter, segmentName string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try each active stream to find the segment
	for _, stream := range m.streams {
		// Validate segment path to prevent directory traversal
		segmentPath, err := secureJoin(stream.GetOutputDir(), segmentName)
		if err != nil {
			continue // Skip invalid paths
		}
		if _, err := os.Stat(segmentPath); err == nil {
			// Found the segment, serve it
			stream.updateAccess()
			return m.serveSegment(w, stream, segmentName)
		}
	}

	return fmt.Errorf("segment %s not found in any active stream", segmentName)
}

// servePlaylist serves the HLS playlist file.
func (m *HLSManager) servePlaylist(w http.ResponseWriter, stream *HLSStreamer) error {
	playlistPath := stream.GetPlaylistPath()

	// Wait for playlist to exist (up to 30 seconds for initial segment creation)
	for i := 0; i < 300; i++ {
		// playlistPath is already validated via secureJoin (constructed from validated outputDir)
		if _, err := os.Stat(playlistPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Read playlist
	// playlistPath is constructed from validated outputDir (via secureJoin during stream creation)
	// playlistPath is constructed from validated outputDir (via secureJoin during stream creation)
	data, err := os.ReadFile(playlistPath) // #nosec G304
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write playlist: %w", err)
	}

	return nil
}

// serveSegment serves an HLS segment file.
func (m *HLSManager) serveSegment(w http.ResponseWriter, stream *HLSStreamer, segmentName string) error {
	// Validate segment path to prevent directory traversal
	segmentPath, err := secureJoin(stream.GetOutputDir(), segmentName)
	if err != nil {
		return fmt.Errorf("invalid segment path: %w", err)
	}

	// Wait for segment to exist (up to 10 seconds)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(segmentPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Open segment file
	// segmentPath is validated via secureJoin above
	// segmentPath is validated via secureJoin above
	file, err := os.Open(segmentPath) // #nosec G304
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

// secureJoin safely joins a root directory with a user-provided path component.
// It prevents path traversal attacks by ensuring the result stays within root.
func secureJoin(root, userPath string) (string, error) {
	// Clean the path
	cleaned := filepath.Clean(userPath)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", userPath)
	}

	// Reject paths starting with ..
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path traversal not allowed: %q", userPath)
	}

	// Join with root
	full := filepath.Join(root, cleaned)

	// Ensure result is within root (defense in depth)
	rootClean := filepath.Clean(root) + string(filepath.Separator)
	fullClean := filepath.Clean(full) + string(filepath.Separator)

	if !strings.HasPrefix(fullClean, rootClean) {
		return "", fmt.Errorf("path escapes root directory: %q", userPath)
	}

	return full, nil
}
