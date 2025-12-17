// Package proxy provides HLS streaming support for the stream proxy.
package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

const (
	// DefaultHLSIdleTimeout is the default timeout for idle HLS streams before cleanup
	DefaultHLSIdleTimeout = 60 * time.Second

	// DefaultHLSCleanupInterval is the default interval for checking idle streams
	DefaultHLSCleanupInterval = 2 * time.Second

	// DefaultHLSPlaylistTimeout is how long we wait for the first playable segment/playlist
	DefaultHLSPlaylistTimeout = 60 * time.Second

	// DefaultHlsDVRSeconds determines how many seconds of DVR window we retain (30 minutes)
	DefaultHlsDVRSeconds = 1800
)

type HLSManagerConfig struct {
	// OutputDir controls where HLS sessions write playlists/segments.
	// Empty means use the legacy default (os.TempDir()).
	OutputDir string

	Generic streamprofile.GenericHLSConfig
	Safari  streamprofile.SafariDVRConfig
	LLHLS   streamprofile.LLHLSConfig

	FFmpegLogLevel string

	IdleTimeout      time.Duration
	CleanupInterval  time.Duration
	PlaylistTimeout  time.Duration
	EnableHLS        bool
	EnableSafariDVR  bool
	EnableLLHLSOptIn bool
}

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
	dvrWindow  int // seconds of DVR window to retain
	segmentDur int // seconds
	ffLogLevel string
}

// HLSManager manages multiple HLS streams.
type HLSManager struct {
	streams           map[string]*HLSStreamer
	llhlsProfiles     map[string]*LLHLSProfile     // Low-Latency HLS profiles (optional, via ?llhls=1)
	safariDVRProfiles map[string]*SafariDVRProfile // Safari DVR profiles (default for Apple clients)
	mu                sync.RWMutex
	logger            zerolog.Logger
	outputBase        string
	genericConfig     streamprofile.GenericHLSConfig
	safariConfig      streamprofile.SafariDVRConfig
	llhlsConfig       streamprofile.LLHLSConfig
	ffLogLevel        string
	startGroup        singleflight.Group
	cleanup           *time.Ticker
	stopChan          chan struct{}
	shutdownOnce      sync.Once
	idleTimeout       time.Duration // Configurable idle timeout for stream cleanup
	cleanupTicker     time.Duration // Configurable cleanup interval
	playlistTimeout   time.Duration
}

// NewHLSManager creates a new HLS stream manager.
func NewHLSManager(logger zerolog.Logger, cfg HLSManagerConfig) (*HLSManager, error) {
	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join(os.TempDir(), "xg2g-hls")
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return nil, fmt.Errorf("create HLS output directory: %w", err)
	}

	generic := cfg.Generic
	if generic.SegmentDuration <= 0 {
		generic = streamprofile.DefaultGenericHLSConfig()
	}
	safari := cfg.Safari
	if safari.SegmentDuration <= 0 || safari.DVRWindowSize <= 0 {
		safari = streamprofile.DefaultSafariDVRConfig()
	}
	llhls := cfg.LLHLS
	if llhls.SegmentDuration <= 0 {
		llhls = streamprofile.DefaultLLHLSConfig()
	}

	idleTimeout := cfg.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultHLSIdleTimeout
	}
	cleanupInterval := cfg.CleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultHLSCleanupInterval
	}
	playlistTimeout := cfg.PlaylistTimeout
	if playlistTimeout <= 0 {
		playlistTimeout = DefaultHLSPlaylistTimeout
	}

	m := &HLSManager{
		streams:           make(map[string]*HLSStreamer),
		llhlsProfiles:     make(map[string]*LLHLSProfile),
		safariDVRProfiles: make(map[string]*SafariDVRProfile),
		logger:            logger,
		outputBase:        outputDir,
		genericConfig:     generic,
		safariConfig:      safari,
		llhlsConfig:       llhls,
		ffLogLevel:        strings.TrimSpace(cfg.FFmpegLogLevel),
		cleanup:           time.NewTicker(cleanupInterval),
		stopChan:          make(chan struct{}),
		idleTimeout:       idleTimeout,
		cleanupTicker:     cleanupInterval,
		playlistTimeout:   playlistTimeout,
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
		dvrWindow:  m.genericConfig.DVRWindowSize,
		segmentDur: m.genericConfig.SegmentDuration,
		ffLogLevel: m.ffLogLevel,
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
	sessionID := strconv.FormatInt(time.Now().UnixNano(), 36)
	segmentPattern := filepath.Join(s.outputDir, fmt.Sprintf("segment_%s_%%05d.ts", sessionID))
	startTagPath := filepath.Join(s.outputDir, ".start_tag")

	// Ensure clean state by removing previous output
	// outputDir is already validated via secureJoin
	_ = os.RemoveAll(s.outputDir)
	if err := os.MkdirAll(s.outputDir, 0750); err != nil {
		return fmt.Errorf("re-create output directory: %w", err)
	}

	// ffmpeg command to convert MPEG-TS to HLS
	// -map 0:v -map 0:a: Only video and audio (exclude subtitles/other streams)
	// -f hls: HLS output format
	// -hls_time 2: 2-second segments (low latency)
	// -hls_list_size N: Keep last N segments (DVR window)
	// -hls_flags: delete_segments (bounded disk) + temp_file (atomic segments)
	// If we are using the Web API, we must "Zap" and resolve the real stream URL manually.
	finalInputURL := s.targetURL
	webAPIURL := convertToWebAPI(s.targetURL, s.serviceRef)
	var programID int

	// If target is already a WebIF stream or we converted to WebIF, resolve once to get the real TS URL.
	if strings.Contains(s.targetURL, "/web/stream.m3u") || webAPIURL != s.targetURL {
		logger.Info().Str("web_api_url", webAPIURL).Msg("attempting to resolve Web API stream (Zapping)")
		resolved, err := resolveWebAPIStreamInfo(webAPIURL)
		if err != nil {
			logger.Error().Err(err).Str("web_api_url", webAPIURL).Msg("failed to resolve Web API stream")
		} else {
			finalInputURL = resolved.URL
			programID = resolved.ProgramID
			logger.Info().Str("resolved_url", finalInputURL).Int("program_id", programID).Msg("successfully resolved stream URL")
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
	var exitReason = "internal_error"
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

	dvrSeconds := s.dvrWindow
	if dvrSeconds <= 0 {
		dvrSeconds = DefaultHlsDVRSeconds
	}
	segmentDuration := s.segmentDur
	if segmentDuration <= 0 {
		segmentDuration = 2
	}
	dvrListSize := dvrSeconds / segmentDuration
	if dvrListSize < 12 {
		dvrListSize = 12
	}

	logger.Info().
		Str("ffmpeg_input", finalInputURL).
		Int("dvr_seconds", dvrSeconds).
		Int("hls_list_size", dvrListSize).
		Msg("starting ffmpeg with input")

	// Write EXT-X-START hint for DVR to instruct clients to start 30 minutes behind live edge
	startOffset := dvrSeconds
	startTag := fmt.Sprintf("#EXT-X-START:TIME-OFFSET=-%d,PRECISE=YES\n", startOffset)
	if err := os.WriteFile(startTagPath, []byte(startTag), 0600); err != nil {
		logger.Warn().Err(err).Msg("failed to write EXT-X-START tag")
	}

	args := []string{
		"-hide_banner",
	}
	args = append(args, logLevelArgs("info", s.ffLogLevel)...)
	args = append(args,
		"-err_detect", "ignore_err", // Ignore decoding errors
		"-ignore_unknown",                          // Ignore streams that fail probing
		"-fflags", "+genpts+igndts+discardcorrupt", // Regenerate PTS, ignore bad DTS, discard corrupt frames
		"-analyzeduration", "7000000", // 7s Analysis (safe for encrypted/slow-lock streams)
		"-probesize", "10000000", // 10MB: Safe for multi-program streams
		"-rw_timeout", "30000000", // 30s socket timeout
	)

	// Add HTTP-specific robustness flags only for network streams
	// Security / Stability: Strict scheme check to avoid applying these flags to file paths
	if u, err := url.Parse(finalInputURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		args = append(args,
			"-reconnect", "1", "-reconnect_at_eof", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", // Robust HTTP input
		)
	}

	args = append(args,
		"-start_at_zero",                  // Reset timestamps to 0
		"-avoid_negative_ts", "make_zero", // Ensure no negative timestamps
		"-ss", "1.5", // SKIP GARBAGE: Drop first 1.5s of input (fixes Startbild/Green artifacts)
		"-thread_queue_size", "4096", // Increase thread queue for buffer
		"-i", finalInputURL,
	)

	// Explicit stream mapping (program-aware when WebIF provides program hint)
	if programID > 0 {
		args = append(args,
			"-map", fmt.Sprintf("0:p:%d:v:0", programID),
			"-map", fmt.Sprintf("0:p:%d:a:0?", programID),
		)
	} else {
		args = append(args,
			"-map", "0:v:0", // Explicitly map first video
			"-map", "0:a:0?", // Explicitly map first audio (optional)
		)
	}

	args = append(args,
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
		"-hls_time", fmt.Sprintf("%d", segmentDuration), // 2s segments
		"-hls_list_size", fmt.Sprintf("%d", dvrListSize), // DVR window (~30min default)
		"-hls_flags", "delete_segments+program_date_time+independent_segments+temp_file",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	// #nosec G204 -- HLS transcoding: ffmpeg command with controlled arguments
	s.cmd = exec.CommandContext(s.ctx, "ffmpeg", args...) // #nosec G204
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

	// Force kill process group to ensure no zombies
	if s.cmd != nil && s.cmd.Process != nil {
		pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			// Fallback if PGID lookup fails (process might be dead already)
			_ = s.cmd.Process.Kill()
		}
	}

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

	// Use fsnotify based watcher instead of polling loop
	// We wait for the playlist file to be created and have non-zero size
	if err := WaitForFile(ctx, s.logger, playlistPath, DefaultHLSPlaylistTimeout); err != nil {
		return err
	}

	// Double check that the first segment is also valid (legacy check deemed useful)
	// We do a quick check here, but rely on WaitForFile for the heavy lifting
	// #nosec G304
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Found a segment line, check if it exists
		segmentPath, _ := secureJoin(s.outputDir, line)
		// We can reuse WaitForFile for the segment too, with a short timeout
		// since it should be there if referenced in playlist
		if err := WaitForFile(ctx, s.logger, segmentPath, 2*time.Second); err != nil {
			s.logger.Warn().Str("segment", line).Msg("referenced segment not immediately available")
		}
		return nil
	}

	return nil
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

	// Cleanup Safari DVR profiles
	toCleanupSafariDVR := make([]*SafariDVRProfile, 0)
	for ref, profile := range m.safariDVRProfiles {
		if profile.IsIdle(m.idleTimeout) {
			m.logger.Info().
				Str("service_ref", ref).
				Msg("removing idle Safari DVR profile")
			toCleanupSafariDVR = append(toCleanupSafariDVR, profile)
			delete(m.safariDVRProfiles, ref)
		}
	}
	m.mu.Unlock()

	// Cleanup streams outside of manager lock to avoid blocking new requests
	for _, stream := range toCleanup {
		stream.Stop()
	}

	// Cleanup LL-HLS profiles
	for _, profile := range toCleanupLLHLS {
		profile.Stop()
	}

	// Cleanup Safari DVR profiles
	for _, profile := range toCleanupSafariDVR {
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
		llhlsProfiles := make([]*LLHLSProfile, 0, len(m.llhlsProfiles))
		for _, profile := range m.llhlsProfiles {
			llhlsProfiles = append(llhlsProfiles, profile)
		}
		safariDVRProfiles := make([]*SafariDVRProfile, 0, len(m.safariDVRProfiles))
		for _, profile := range m.safariDVRProfiles {
			safariDVRProfiles = append(safariDVRProfiles, profile)
		}
		m.streams = make(map[string]*HLSStreamer)
		m.llhlsProfiles = make(map[string]*LLHLSProfile)
		m.safariDVRProfiles = make(map[string]*SafariDVRProfile)
		m.mu.Unlock()

		// Stop all streams outside of lock
		for _, stream := range streams {
			stream.Stop()
		}

		// Stop all LL-HLS profiles outside of lock
		for _, profile := range llhlsProfiles {
			profile.Stop()
		}

		// Stop all Safari DVR profiles outside of lock
		for _, profile := range safariDVRProfiles {
			profile.Stop()
		}
	})
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
	profile, err := NewLLHLSProfile(serviceRef, targetURL, m.outputBase, m.logger, m.llhlsConfig)
	if err != nil {
		return nil, err
	}

	m.llhlsProfiles[serviceRef] = profile
	return profile, nil
}

// GetOrCreateSafariDVRProfile gets an existing Safari DVR profile or creates a new one.
func (m *HLSManager) GetOrCreateSafariDVRProfile(serviceRef, targetURL string) (*SafariDVRProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if profile already exists
	if profile, ok := m.safariDVRProfiles[serviceRef]; ok {
		profile.UpdateAccess()
		return profile, nil
	}

	// Create new Safari DVR profile
	profile, err := NewSafariDVRProfile(serviceRef, targetURL, m.outputBase, m.logger, m.safariConfig)
	if err != nil {
		return nil, err
	}

	m.safariDVRProfiles[serviceRef] = profile
	return profile, nil
}

// ServeHLS handles HLS playlist and segment requests.
// Routes to appropriate HLS profile based on User-Agent and query parameters:
//   - ?llhls=1 → LL-HLS profile (low latency, opt-in)
//   - Safari/Apple clients → Safari DVR profile (large sliding window for scrubbing)
//   - Others → Generic HLS
func (m *HLSManager) ServeHLS(w http.ResponseWriter, r *http.Request, serviceRef, targetURL string) error {
	userAgent := r.Header.Get("User-Agent")

	// Priority 1: Explicit LL-HLS request via query parameter
	if r.URL.Query().Get("llhls") == "1" {
		return m.serveLLHLS(w, r, serviceRef, targetURL)
	}

	// Priority 2: Safari/Apple clients → Safari DVR profile
	if IsSafariClient(userAgent) || IsNativeAppleClient(userAgent) {
		return m.serveSafariDVR(w, r, serviceRef, targetURL)
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

// PreflightHLS ensures the selected HLS profile is started and ready before a client
// attaches the playlist to a <video> element. This avoids first-play failures on
// Safari when the initial manifest/segments take longer to become available.
func (m *HLSManager) PreflightHLS(ctx context.Context, r *http.Request, serviceRef, targetURL string) error {
	userAgent := r.Header.Get("User-Agent")

	var key string
	switch {
	case r.URL.Query().Get("llhls") == "1":
		key = "llhls:" + serviceRef
	case IsSafariClient(userAgent) || IsNativeAppleClient(userAgent):
		key = "safari:" + serviceRef
	default:
		key = "generic:" + serviceRef
	}

	_, err, _ := m.startGroup.Do(key, func() (any, error) {
		switch {
		case r.URL.Query().Get("llhls") == "1":
			profile, err := m.GetOrCreateLLHLSProfile(serviceRef, targetURL)
			if err != nil {
				return nil, err
			}
			aacBitrate := strings.TrimSpace(m.safariConfig.AACBitrate)
			if aacBitrate == "" {
				aacBitrate = "192k"
			}
			if err := profile.Start(true, aacBitrate); err != nil {
				return nil, err
			}
			return nil, profile.WaitReady(m.playlistTimeout)

		case IsSafariClient(userAgent) || IsNativeAppleClient(userAgent):
			profile, err := m.GetOrCreateSafariDVRProfile(serviceRef, targetURL)
			if err != nil {
				return nil, err
			}
			if err := profile.Start(); err != nil {
				return nil, err
			}
			return nil, profile.WaitReady(m.playlistTimeout)

		default:
			stream, err := m.GetOrCreateStream(serviceRef, targetURL)
			if err != nil {
				return nil, err
			}

			// Start() already blocks until the first playlist+segment is available.
			// Run it in a goroutine so we can respect ctx cancellation.
			done := make(chan error, 1)
			go func() { done <- stream.Start() }()
			select {
			case err := <-done:
				return nil, err
			case <-ctx.Done():
				stream.Stop()
				return nil, ctx.Err()
			}
		}
	})

	return err
}

// serveSafariDVR serves HLS using the Safari DVR profile (large sliding window).
func (m *HLSManager) serveSafariDVR(w http.ResponseWriter, r *http.Request, serviceRef, targetURL string) error {
	// Get or create Safari DVR profile
	profile, err := m.GetOrCreateSafariDVRProfile(serviceRef, targetURL)
	if err != nil {
		return fmt.Errorf("get safari dvr profile: %w", err)
	}

	// Start profile if not already started
	if err := profile.Start(); err != nil {
		return fmt.Errorf("start safari dvr profile: %w", err)
	}

	// Wait for stream to be ready (initial segments available)
	if err := profile.WaitReady(m.playlistTimeout); err != nil {
		return fmt.Errorf("safari dvr profile not ready: %w", err)
	}

	// Determine what to serve
	path := r.URL.Path

	if strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, "/hls") {
		// Serve Safari DVR playlist
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
	if err := profile.WaitReady(m.playlistTimeout); err != nil {
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

	// Also check Safari DVR profiles (Safari can sometimes request segments without the /hls/{ref}/ prefix)
	for _, profile := range m.safariDVRProfiles {
		segmentPath, err := secureJoin(profile.outputDir, segmentName)
		if err != nil {
			continue
		}
		if _, err := os.Stat(segmentPath); err == nil {
			profile.UpdateAccess()
			return profile.ServeSegment(w, segmentName)
		}
	}

	return fmt.Errorf("segment %s not found in any active stream", segmentName)
}

// servePlaylist serves the HLS playlist file.
func (m *HLSManager) servePlaylist(w http.ResponseWriter, stream *HLSStreamer) error {
	// Keep the stream alive while clients are actively fetching the manifest.
	stream.updateAccess()

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
	data, err := os.ReadFile(playlistPath) // #nosec G304
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	// Check for start tag (DVR hint)
	startTagPath := filepath.Join(stream.GetOutputDir(), ".start_tag")
	// #nosec G304
	if startTagData, err := os.ReadFile(startTagPath); err == nil && len(startTagData) > 0 {
		// Inject start tag after #EXTM3U header
		content := string(data)
		if strings.HasPrefix(content, "#EXTM3U") {
			if idx := strings.Index(content, "\n"); idx != -1 {
				// Insert tag after first line (which is usually #EXTM3U)
				content = content[:idx+1] + string(startTagData) + content[idx+1:]
				data = []byte(content)
			}
		}
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
	// Keep the stream alive while clients are actively fetching segments.
	stream.updateAccess()

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

// Legacy helpers were moved to internal/core/pathutil; see pathutil.go for wrappers.
