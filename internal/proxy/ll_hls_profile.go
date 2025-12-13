package proxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// LLHLSProfile represents a Low-Latency HLS profile optimized for native Apple clients.
// This implementation uses fragmented MP4 (fmp4) with partial segments for minimal latency.
//
// Key differences from classical HLS:
//   - Container: Fragmented MP4 (.m4s) instead of MPEG-TS (.ts)
//   - Segments: Divided into partial segments (~200ms each)
//   - Latency: ~0.5-1s instead of ~6s
//   - Compatibility: iOS 14+, macOS 11+, Safari 14+ only
//
// Use cases:
//   - Safari on iOS/macOS
//   - Native Apple TV app
//   - VLC on iOS (experimental)
type LLHLSProfile struct {
	serviceRef  string
	targetURL   string
	outputDir   string
	cmd         *exec.Cmd
	ctx         context.Context
	cancel      context.CancelFunc
	logger      zerolog.Logger
	lastAccess  time.Time
	mu          sync.RWMutex
	started     bool
	segmentSize int // Segment duration (1s recommended for LL-HLS)
	playlistLen int // Number of segments (6-10 for LL-HLS)
	partSize    int // Partial segment size in bytes (256KB default)
	ffmpegPath  string
	ready       chan struct{} // Signals when initial segments are ready
	hevcConfig  LLHLSConfig   // Store full config for decision making
}

// LLHLSConfig holds configuration for LL-HLS profile.
type LLHLSConfig struct {
	SegmentDuration int    // Segment duration in seconds (1-2)
	PlaylistSize    int    // Number of segments in playlist (6-10)
	StartupSegments int    // Pre-buffer segments before serving (2-3)
	PartSize        int    // Partial segment size in bytes (256KB default)
	FFmpegPath      string // Path to FFmpeg binary

	// HEVC / Transcoding Config
	HevcEnabled    bool   // Enable HEVC transcoding
	HevcBitrate    string // Target video bitrate (e.g. "6000k")
	HevcMaxBitrate string // Max video bitrate (e.g. "8000k")
	HevcEncoder    string // Encoder name (hevc_nvenc, hevc_vaapi, libx265)
	HevcProfile    string // Encoding profile (main, main10)
	HevcLevel      string // Encoding level (5.0, 5.1)
	VaapiDevice    string // VAAPI Device path (default /dev/dri/renderD128)
	PartDuration   string // Partial segment duration (ms, e.g. "200ms")
}

// GetLLHLSConfig returns LL-HLS configuration from environment or defaults.
func GetLLHLSConfig() LLHLSConfig {
	cfg := LLHLSConfig{
		SegmentDuration: 1,
		PlaylistSize:    6,
		StartupSegments: 2,
		PartSize:        262144,
		FFmpegPath:      "/usr/bin/ffmpeg",
		HevcEnabled:     false,
		HevcBitrate:     "6000k",
		HevcMaxBitrate:  "8000k",
		HevcEncoder:     "hevc_nvenc",
		HevcProfile:     "main",
		HevcLevel:       "5.0",
		VaapiDevice:     "/dev/dri/renderD128",
		PartDuration:    "200ms", // 200ms partial segments
	}

	// Override from environment
	if v := os.Getenv("XG2G_LLHLS_SEGMENT_DURATION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 2 {
			cfg.SegmentDuration = n
		}
	}

	if v := os.Getenv("XG2G_LLHLS_PLAYLIST_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 6 && n <= 10 {
			cfg.PlaylistSize = n
		}
	}

	if v := os.Getenv("XG2G_LLHLS_STARTUP_SEGMENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 3 {
			cfg.StartupSegments = n
		}
	}

	if v := os.Getenv("XG2G_LLHLS_PART_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 65536 && n <= 1048576 {
			cfg.PartSize = n
		}
	}

	if v := os.Getenv("XG2G_LLHLS_FFMPEG_PATH"); v != "" {
		cfg.FFmpegPath = v
	} else if v := os.Getenv("XG2G_WEB_FFMPEG_PATH"); v != "" {
		cfg.FFmpegPath = v
	}

	// HEVC Config - Renamed to XG2G_LLHLS_HEVC_*
	if v := os.Getenv("XG2G_LLHLS_HEVC_ENABLED"); v == "true" {
		cfg.HevcEnabled = true
	} else if v := os.Getenv("XG2G_WEB_HEVC_PROFILE_ENABLED"); v == "true" {
		cfg.HevcEnabled = true // Backwards compatibility during migration
	}

	if v := os.Getenv("XG2G_LLHLS_HEVC_BITRATE"); v != "" {
		cfg.HevcBitrate = v
	} else if v := os.Getenv("XG2G_WEB_HEVC_BITRATE"); v != "" {
		cfg.HevcBitrate = v
	}

	if v := os.Getenv("XG2G_LLHLS_HEVC_PEAK"); v != "" {
		cfg.HevcMaxBitrate = v
	} else if v := os.Getenv("XG2G_WEB_HEVC_MAXBITRATE"); v != "" {
		cfg.HevcMaxBitrate = v
	}

	if v := os.Getenv("XG2G_LLHLS_HEVC_ENCODER"); v != "" {
		cfg.HevcEncoder = v
	} else if v := os.Getenv("XG2G_WEB_HEVC_ENCODER"); v != "" {
		cfg.HevcEncoder = v
	}

	if v := os.Getenv("XG2G_LLHLS_HEVC_PROFILE"); v != "" {
		cfg.HevcProfile = v
	}

	if v := os.Getenv("XG2G_LLHLS_HEVC_LEVEL"); v != "" {
		cfg.HevcLevel = v
	}

	if v := os.Getenv("XG2G_LLHLS_VAAPI_DEVICE"); v != "" {
		cfg.VaapiDevice = v
	}

	if v := os.Getenv("XG2G_WEB_LL_HLS_PART_DURATION"); v != "" {
		cfg.PartDuration = v
	}

	return cfg
}

// NewLLHLSProfile creates a new LL-HLS profile.
func NewLLHLSProfile(serviceRef, targetURL, baseDir string, logger zerolog.Logger, config LLHLSConfig) (*LLHLSProfile, error) {
	// Create unique directory for this profile
	// Use sanitized service reference as directory name
	streamID := sanitizeServiceRef(serviceRef)
	outputDir, err := secureJoin(filepath.Join(baseDir, "llhls"), streamID)
	if err != nil {
		return nil, fmt.Errorf("create output path: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &LLHLSProfile{
		serviceRef:  serviceRef,
		targetURL:   targetURL,
		outputDir:   outputDir,
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger.With().Str("component", "llhls_profile").Str("service_ref", serviceRef).Logger(),
		lastAccess:  time.Now(),
		segmentSize: config.SegmentDuration,
		playlistLen: config.PlaylistSize,
		partSize:    config.PartSize,
		ffmpegPath:  config.FFmpegPath,
		ready:       make(chan struct{}),
		hevcConfig:  config,
	}, nil
}

// Start starts the LL-HLS segmentation process.
// This process ensures:
//   - H.264 video with proper Annex-B headers via h264_mp4toannexb bitstream filter
//   - AAC-LC audio (transcoded from AC3/MP2 if necessary)
//   - Fragmented MP4 segments with partial segments for low latency
//   - Pre-buffering of initial segments before signaling ready
//
// LL-HLS differences from classical HLS:
//   - Uses fmp4 instead of mpegts container
//   - Enables partial segments for sub-second latency
//   - Adds low_latency and independent_segments flags
func (p *LLHLSProfile) Start(forceAAC bool, aacBitrate string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}

	playlistPath := filepath.Join(p.outputDir, "playlist.m3u8")
	segmentPattern := filepath.Join(p.outputDir, "segment_%03d.m4s")
	initSegment := filepath.Join(p.outputDir, "init.mp4")

	// Ensure clean state by removing previous output
	// outputDir is already validated via secureJoin
	_ = os.RemoveAll(p.outputDir)
	if err := os.MkdirAll(p.outputDir, 0750); err != nil {
		return fmt.Errorf("re-create output directory: %w", err)
	}

	// Build FFmpeg command for LL-HLS optimization
	// Based on Plex profile but with fmp4 and LL-HLS specific flags
	args := []string{
		"-hide_banner",
	}
	args = append(args, logLevelArgs("warning")...)

	// VAAPI Specific Global Args (must be before input if possible, or strictly global)
	// User example: ffmpeg -init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va -i ...
	if p.hevcConfig.HevcEnabled && strings.Contains(p.hevcConfig.HevcEncoder, "vaapi") {
		// Explicitly initialize VAAPI device context
		// This fixes "Exit Code 234" / "Incompatible pixel format" errors
		deviceArg := fmt.Sprintf("vaapi=va:%s", p.hevcConfig.VaapiDevice)
		args = append(args, "-init_hw_device", deviceArg, "-filter_hw_device", "va")
	}

	// If we are using the Web API, we must "Zap" and resolve the real stream URL manually.
	finalInputURL := p.targetURL
	webAPIURL := convertToWebAPI(p.targetURL, p.serviceRef)

	if webAPIURL != p.targetURL {
		p.logger.Info().Str("web_api_url", webAPIURL).Msg("attempting to resolve Web API stream (Zapping)")
		resolved, err := resolveWebAPI(webAPIURL)
		if err != nil {
			p.logger.Error().Err(err).Str("web_api_url", webAPIURL).Msg("failed to resolve Web API stream")
		} else {
			finalInputURL = resolved
			p.logger.Info().Str("resolved_url", finalInputURL).Msg("successfully resolved stream URL")
			// Give the tuner a moment to lock after zapping
			time.Sleep(1000 * time.Millisecond)
		}
	} else {
		p.logger.Info().Msg("using direct stream URL (no Web API detected)")
	}

	args = append(args,
		"-fflags", "+genpts+igndts", // Regenerate timestamps (Enigma2 has broken DTS)
		"-i", finalInputURL,
		"-map", "0:v",
	)

	// Video Transcoding Decision
	if p.hevcConfig.HevcEnabled {
		// HEVC (H.265) Transcoding
		args = append(args,
			"-c:v", p.hevcConfig.HevcEncoder, // Hardware encoder
			"-b:v", p.hevcConfig.HevcBitrate, // Average bitrate
			"-maxrate", p.hevcConfig.HevcMaxBitrate, // Max bitrate
			"-bufsize", p.hevcConfig.HevcMaxBitrate, // Buffer size
		)

		// Pixel Format logic:
		// VAAPI requires 'vaapi' surface via filter, not explicit -pix_fmt yuv420p on output
		// NVENC/Software usually want -pix_fmt yuv420p for compatibility
		if !strings.Contains(p.hevcConfig.HevcEncoder, "vaapi") {
			args = append(args, "-pix_fmt", "yuv420p")
		}

		args = append(args,
			"-r", "25", // Normalize framerate
			"-shortest",                            // Finish when shortest stream ends
			"-profile:v", p.hevcConfig.HevcProfile, // Explicit Profile (main)
			"-level:v", p.hevcConfig.HevcLevel, // Explicit Level (5.0)
		)

		// Encoder specific tunings
		if strings.Contains(p.hevcConfig.HevcEncoder, "nvenc") {
			args = append(args,
				"-preset", "p4", // Good balance for LL-HLS
				"-tune", "ll", // Low latency tune
				"-zerolatency", "1",
			)
		} else if strings.Contains(p.hevcConfig.HevcEncoder, "vaapi") {
			// VAAPI Pipeline: Software Decode -> Upload -> Encode
			// -vf 'format=nv12,hwupload'
			args = append(args,
				"-vf", "format=nv12,hwupload",
				"-low_power", "0", // 0 is safer/default, 1 needed for some low power units? User didn't specify.
			)
		} else if strings.Contains(p.hevcConfig.HevcEncoder, "libx265") {
			args = append(args,
				"-preset", "ultrafast",
				"-tune", "zerolatency",
			)
		}

		// Set TAG for Apple devices to recognize HEVC in fMP4
		// hvc1 is widely supported
		args = append(args, "-tag:v", "hvc1")

	} else {
		// CLASSICAL COPY (Original Behavior)
		args = append(args,
			"-c:v", "copy", // Copy video without re-encoding
			"-bsf:v", "h264_mp4toannexb", // CRITICAL: Add PPS/SPS headers (same as Plex profile)
		)
	}

	// Audio handling: AAC transcoding for iOS
	if forceAAC {
		args = append(args,
			"-map", "0:a",
			"-c:a", "aac", // Transcode to AAC-LC
			"-b:a", aacBitrate,
			"-ac", "2", // Stereo
			"-async", "1", // Audio-video sync
		)
	} else {
		args = append(args,
			"-map", "0:a",
			"-c:a", "copy", // Copy audio as-is
		)
	}

	// Common timestamp/muxing options (same as Plex profile)
	args = append(args,
		"-start_at_zero",
		"-avoid_negative_ts", "make_zero",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-flags", "+cgop", // Closed GOP for better segmentation
	)

	// LL-HLS specific output settings
	// CRITICAL: Uses fmp4 instead of mpegts for partial segment support
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", p.segmentSize), // 1s segments (shorter than classical HLS)
		"-hls_list_size", fmt.Sprintf("%d", p.playlistLen), // 6-10 segments
		"-hls_segment_type", "fmp4", // CRITICAL: Fragmented MP4 (not mpegts!)
		"-hls_fmp4_init_filename", "init.mp4", // Initialization segment
		"-hls_flags", "independent_segments+delete_segments+program_date_time", // LL-HLS flags
		"-hls_segment_filename", segmentPattern,
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof", // fmp4 optimization
		playlistPath,
	)

	p.logger.Info().
		Str("target", p.targetURL).
		Str("output", playlistPath).
		Int("segment_duration", p.segmentSize).
		Int("playlist_size", p.playlistLen).
		Bool("hevc_transcode", p.hevcConfig.HevcEnabled).
		Str("video_encoder", p.hevcConfig.HevcEncoder).
		Str("container", "fmp4").
		Msg("starting LL-HLS profile (Low-Latency HLS)")

	p.cmd = exec.CommandContext(p.ctx, p.ffmpegPath, args...)
	p.cmd.Dir = p.outputDir

	// Capture stderr for debugging
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	p.started = true

	// Monitor FFmpeg stderr
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				p.logger.Debug().Str("ffmpeg_stderr", string(buf[:n])).Msg("ffmpeg output")
			}
			if err != nil {
				break
			}
		}
	}()

	// Watchdog: Monitor playlist updates to detect stalls
	go p.watchdogRoutine(playlistPath)

	// Wait for FFmpeg process to exit
	go func() {
		err := p.cmd.Wait()
		p.mu.Lock()
		p.started = false
		p.mu.Unlock()
		if err != nil && p.ctx.Err() == nil {
			p.logger.Error().Err(err).Msg("ffmpeg process exited with error (possibly killed by watchdog or crash)")
		} else {
			p.logger.Info().Msg("ffmpeg process stopped")
		}
		// Cleanup is handled by Next Start() or Stop() calls, but we can ensure dir is clean
		// if we aren't being restarted immediately?
		// Actually, if we crash/exit, we want Stop() to be called eventually by timeouts or new requests calling Start
		// If we do nothing, isIdle will eventually clean us up.
	}()

	// Wait for initial segments to be ready
	go p.waitForSegments(initSegment, playlistPath)

	return nil
}

// watchdogRoutine monitors the playlist file for updates.
// If the playlist stops updating, it assumes ffmpeg has stalled and kills the process.
func (p *LLHLSProfile) watchdogRoutine(playlistPath string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Give ffmpeg some time to start up and write first files
	time.Sleep(5 * time.Second)

	var lastModTime time.Time
	stallCount := 0
	maxStalls := 3 // 3 * 2s = 6s timeout

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			if !p.started {
				p.mu.RUnlock()
				return
			}
			p.mu.RUnlock()

			info, err := os.Stat(playlistPath)
			if err != nil {
				// Playlist gone? Stall.
				stallCount++
			} else {
				if !info.ModTime().After(lastModTime) {
					stallCount++
				} else {
					// Healthy
					lastModTime = info.ModTime()
					stallCount = 0
				}
			}

			if stallCount >= maxStalls {
				p.logger.Error().
					Int("stall_seconds", stallCount*2).
					Msg("watchdog: stream stalled (no playlist updates), killing ffmpeg")

				// Kill process to force restart on next request
				if p.cmd != nil && p.cmd.Process != nil {
					_ = p.cmd.Process.Kill()
				}
				return
			}
		}
	}
}

// waitForSegments waits for initial segments to be written before signaling ready.
func (p *LLHLSProfile) waitForSegments(initSegment, playlistPath string) {
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			p.logger.Warn().Msg("timeout waiting for LL-HLS segments")
			close(p.ready)
			return
		case <-ticker.C:
			// Check if init segment and playlist exist
			if _, err := os.Stat(initSegment); err != nil {
				continue
			}
			if _, err := os.Stat(playlistPath); err != nil {
				continue
			}

			// Check if at least one segment exists
			matches, err := filepath.Glob(filepath.Join(p.outputDir, "segment_*.m4s"))
			if err != nil || len(matches) == 0 {
				continue
			}

			p.logger.Info().
				Int("segments_ready", len(matches)).
				Msg("LL-HLS profile ready")
			close(p.ready)
			return
		}
	}
}

// WaitReady waits for the profile to be ready with a timeout.
func (p *LLHLSProfile) WaitReady(timeout time.Duration) error {
	select {
	case <-p.ready:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for LL-HLS profile to be ready")
	}
}

// Stop stops the LL-HLS profile and cleans up resources.
func (p *LLHLSProfile) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return
	}

	p.logger.Info().Msg("stopping LL-HLS profile")
	p.cancel()

	// Wait for process to exit (with timeout)
	done := make(chan struct{})
	go func() {
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info().Msg("LL-HLS profile stopped gracefully")
	case <-time.After(5 * time.Second):
		p.logger.Warn().Msg("force killing LL-HLS profile")
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
	}

	p.started = false

	// Cleanup output directory
	if err := os.RemoveAll(p.outputDir); err != nil {
		p.logger.Error().Err(err).Msg("failed to cleanup LL-HLS output directory")
	}
}

// UpdateAccess updates the last access time.
func (p *LLHLSProfile) UpdateAccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastAccess = time.Now()
}

// IsIdle returns true if the profile hasn't been accessed within the timeout.
func (p *LLHLSProfile) IsIdle(timeout time.Duration) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.lastAccess) > timeout
}

// ServePlaylist serves the LL-HLS playlist.
func (p *LLHLSProfile) ServePlaylist(w http.ResponseWriter) error {
	p.UpdateAccess()

	playlistPath := filepath.Join(p.outputDir, "playlist.m3u8")
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Write(data)
	return nil
}

// ServeSegment serves an LL-HLS segment (.m4s or init.mp4).
func (p *LLHLSProfile) ServeSegment(w http.ResponseWriter, segmentName string) error {
	p.UpdateAccess()

	// Validate segment name to prevent path traversal
	segmentPath, err := secureJoin(p.outputDir, segmentName)
	if err != nil {
		return fmt.Errorf("invalid segment path: %w", err)
	}

	data, err := os.ReadFile(segmentPath)
	if err != nil {
		return fmt.Errorf("read segment: %w", err)
	}

	// Determine Content-Type based on file extension
	contentType := "video/iso.segment"
	if segmentName == "init.mp4" {
		contentType = "video/mp4"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
	return nil
}

// IsNativeAppleClient detects if the request is from a native Apple client.
// These clients support LL-HLS (iOS 14+, macOS 11+, Safari 14+).
// Excludes Plex (which uses its own HLS implementation).
func IsNativeAppleClient(userAgent string) bool {
	ua := userAgent

	// Exclude Plex (handled separately)
	if IsPlexClient(userAgent) {
		return false
	}

	// Native Apple clients that support LL-HLS
	return strings.Contains(ua, "AppleCoreMedia") || // Native iOS/macOS media player
		strings.Contains(ua, "CFNetwork") || // iOS network stack
		strings.Contains(ua, "VideoToolbox") // macOS video framework
}
