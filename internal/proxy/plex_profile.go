// Package proxy provides Plex/iOS-optimized HLS streaming profiles.
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// PlexProfile represents a Plex/iOS-optimized streaming profile.
// This profile ensures Direct Play compatibility with Plex on iPhone/iPad by:
//   - Outputting HLS with proper Content-Types
//   - Enforcing H.264/AAC codecs
//   - Using short segments (2-4s) for fast startup
//   - Pre-buffering 1-2 segments before serving playlist
type PlexProfile struct {
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
	segmentSize int           // Segment duration in seconds (2-4)
	playlistLen int           // Number of segments to keep in playlist (3-6)
	ffmpegPath  string        // Path to ffmpeg binary
	ready       chan struct{} // Signals when first segments are ready
}

// PlexProfileConfig holds configuration for Plex/iOS profile.
type PlexProfileConfig struct {
	SegmentDuration int    // HLS segment duration in seconds (default: 2)
	PlaylistSize    int    // Number of segments in playlist (default: 3)
	FFmpegPath      string // Path to ffmpeg (default: /usr/bin/ffmpeg)
	ForceAAC        bool   // Force audio transcoding to AAC (default: true)
	AACBitrate      string // AAC bitrate (default: 192k)
	StartupSegments int    // Number of segments to pre-buffer before serving (default: 2)
}

// GetPlexProfileConfig loads Plex profile configuration from environment.
func GetPlexProfileConfig() PlexProfileConfig {
	cfg := PlexProfileConfig{
		SegmentDuration: 2,
		PlaylistSize:    3,
		FFmpegPath:      "/usr/bin/ffmpeg",
		ForceAAC:        true,
		AACBitrate:      "192k",
		StartupSegments: 2,
	}

	// Override from environment
	if v := os.Getenv("XG2G_PLEX_SEGMENT_DURATION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 && n <= 10 {
			cfg.SegmentDuration = n
		}
	}

	if v := os.Getenv("XG2G_PLEX_PLAYLIST_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 3 && n <= 10 {
			cfg.PlaylistSize = n
		}
	}

	if v := os.Getenv("XG2G_PLEX_FFMPEG_PATH"); v != "" {
		cfg.FFmpegPath = v
	}

	if v := os.Getenv("XG2G_PLEX_FORCE_AAC"); v != "" {
		cfg.ForceAAC = strings.ToLower(v) == "true"
	}

	if v := os.Getenv("XG2G_PLEX_AAC_BITRATE"); v != "" {
		cfg.AACBitrate = v
	}

	if v := os.Getenv("XG2G_PLEX_STARTUP_SEGMENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 5 {
			cfg.StartupSegments = n
		}
	}

	return cfg
}

// NewPlexProfile creates a new Plex/iOS-optimized HLS streamer.
func NewPlexProfile(serviceRef, targetURL, outputDir string, logger zerolog.Logger, config PlexProfileConfig) (*PlexProfile, error) {
	// Validate output directory
	streamID := sanitizeServiceRef(serviceRef)
	fullOutputDir, err := secureJoin(outputDir, streamID)
	if err != nil {
		return nil, fmt.Errorf("invalid stream path: %w", err)
	}

	if err := os.MkdirAll(fullOutputDir, 0750); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &PlexProfile{
		serviceRef:  serviceRef,
		targetURL:   targetURL,
		outputDir:   fullOutputDir,
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger.With().Str("service_ref", serviceRef).Str("profile", "plex").Logger(),
		lastAccess:  time.Now(),
		segmentSize: config.SegmentDuration,
		playlistLen: config.PlaylistSize,
		ffmpegPath:  config.FFmpegPath,
		ready:       make(chan struct{}),
	}, nil
}

// Start starts the Plex/iOS HLS segmentation process.
// This process ensures:
//   - H.264 video with proper Annex-B headers via h264_mp4toannexb bitstream filter
//   - AAC-LC audio (transcoded from AC3/MP2 if necessary)
//   - Short segments (2-4s) for fast startup
//   - Pre-buffering of initial segments before signaling ready
//
// NOTE: This reuses the same H.264 repair technique from transcoder.RepairH264Stream()
// but outputs HLS instead of MPEG-TS.
func (p *PlexProfile) Start(forceAAC bool, aacBitrate string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}

	// Inject stream_id into context logger for traceability
	// This ensures all logs from this stream session share the same ID
	streamID := fmt.Sprintf("plex-%s-%d", time.Now().Format("150405"), time.Now().UnixNano()%1000)
	logger := zerolog.Ctx(p.ctx).With().Str("stream_id", streamID).Logger()
	// Update context with logger
	p.ctx = logger.WithContext(p.ctx)
	// Update struct logger as well for other methods
	p.logger = logger

	// Log stream start (Lifecycle Event)
	logger.Info().
		Str("event", "stream_start").
		Str("mode", "plex_hls").
		Str("input_url", sanitizeURL(p.targetURL)).
		Str("segment_duration", fmt.Sprintf("%ds", p.segmentSize)).
		Str("force_aac", fmt.Sprintf("%v", forceAAC)).
		Msg("starting plex profile session")

	startTime := time.Now()
	var exitReason string = "internal_error" // Default
	var lastStats *FFmpegStats

	defer func() {
		// Log stream end (Lifecycle Event)
		event := logger.Info().
			Str("event", "stream_end").
			Str("exit_reason", exitReason).
			Dur("duration_ms", time.Since(startTime))

		if lastStats != nil {
			event.Float64("ffmpeg_last_speed", lastStats.Speed).
				Float64("ffmpeg_last_bitrate_kbps", lastStats.BitrateKBPS)
		}
		event.Msg("plex profile session ended")
	}()

	playlistPath := filepath.Join(p.outputDir, "playlist.m3u8")
	segmentPattern := filepath.Join(p.outputDir, "segment_%03d.ts")

	// Build FFmpeg command for Plex/iOS HLS optimization
	// This extends the existing H.264 repair technique (h264_mp4toannexb) with HLS output
	// Based on transcoder.RepairH264Stream() but modified for segmented output
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

	args := []string{
		"-hide_banner",
	}
	args = append(args, logLevelArgs("info")...)
	args = append(args,
		"-fflags", "+genpts+igndts", // Regenerate timestamps (Enigma2 has broken DTS)
		"-i", finalInputURL,
		"-map", "0:v",
		"-c:v", "copy", // Copy video without re-encoding
		"-bsf:v", "h264_mp4toannexb", // CRITICAL: Add PPS/SPS headers for Plex (same as RepairH264Stream)
	)

	// Audio handling: AAC transcoding for iOS or copy for Plex server
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

	// Common timestamp/muxing options (same as RepairH264Stream)
	args = append(args,
		"-start_at_zero",
		"-avoid_negative_ts", "make_zero",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-mpegts_copyts", "1",
		"-mpegts_flags", "resend_headers+initial_discontinuity",
		"-pcr_period", "20",
		"-pat_period", "0.1",
		"-sdt_period", "0.5",
	)

	// HLS-specific output settings
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", p.segmentSize), // Segment duration (2-4s)
		"-hls_list_size", fmt.Sprintf("%d", p.playlistLen), // Playlist size (3-6 segments)
		"-hls_flags", "delete_segments+append_list+program_date_time", // Plex needs program_date_time
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	p.logger.Info().
		Str("target", p.targetURL).
		Str("output", playlistPath).
		Int("segment_duration", p.segmentSize).
		Int("playlist_size", p.playlistLen).
		Bool("force_aac", forceAAC).
		Msg("starting Plex/iOS HLS profile")

	// Validate and sanitize ffmpeg path
	ffmpegPath := filepath.Clean(p.ffmpegPath)
	if !filepath.IsAbs(ffmpegPath) {
		return fmt.Errorf("ffmpeg path must be absolute: %s", ffmpegPath)
	}

	// Create ffmpeg command
	// #nosec G204 -- ffmpegPath is sanitized above and args contain only predefined options
	p.cmd = exec.CommandContext(p.ctx, ffmpegPath, args...)
	p.cmd.Stdout = nil

	// Capture ffmpeg stderr for debugging
	stderrPipe, err := p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	// Monitor ffmpeg stderr in background
	// Monitor ffmpeg stderr in background with stats parsing
	// Use WaitGroup to ensure we capture all logs before exit
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
					logger.Debug().Str("stderr", line).Msg("plex profile ffmpeg stderr")
				}
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Debug().
				Err(err).
				Msg("ffmpeg stderr scanner error")
		}
	}()

	if err := p.cmd.Start(); err != nil {
		// Cleanup on start failure
		_ = os.RemoveAll(p.outputDir)
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	p.started = true

	// Monitor process in background
	go func() {
		defer stderrWg.Wait() // Wait for logger to finish

		if err := p.cmd.Wait(); err != nil {
			if p.ctx.Err() == nil {
				exitReason = "ffmpeg_exit"
				logger.Error().
					Err(err).
					Msg("Plex/iOS HLS segmentation failed")
			} else {
				exitReason = "client_disconnect"
			}
		} else {
			if p.ctx.Err() != nil {
				exitReason = "client_disconnect"
			} else {
				exitReason = "ffmpeg_exit" // Unexpected exit even if 0? HLS should run forever
			}
		}

		if exitReason == "internal_error" {
			exitReason = "success" // Should typically not happen for infinite HLS loop unless stopped
		}

		p.mu.Lock()
		p.started = false
		p.mu.Unlock()
	}()

	// Wait for initial segments to be ready (fast startup)
	config := GetPlexProfileConfig()
	if err := p.waitForSegments(p.ctx, config.StartupSegments); err != nil {
		p.Stop()
		return fmt.Errorf("wait for initial segments: %w", err)
	}

	// Signal ready
	close(p.ready)

	return nil
}

// Stop stops the Plex/iOS HLS segmentation process.
func (p *PlexProfile) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return
	}

	p.logger.Info().Msg("stopping Plex/iOS HLS profile")
	p.cancel()

	// Clean up output directory
	if err := os.RemoveAll(p.outputDir); err != nil {
		p.logger.Warn().Err(err).Msg("failed to clean up HLS directory")
	}
}

// GetPlaylistPath returns the path to the HLS playlist file.
func (p *PlexProfile) GetPlaylistPath() string {
	return filepath.Join(p.outputDir, "playlist.m3u8")
}

// GetOutputDir returns the output directory for this stream.
func (p *PlexProfile) GetOutputDir() string {
	return p.outputDir
}

// UpdateAccess updates the last access time.
func (p *PlexProfile) UpdateAccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastAccess = time.Now()
}

// IsIdle checks if the stream has been idle for too long.
func (p *PlexProfile) IsIdle(timeout time.Duration) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.lastAccess) > timeout
}

// WaitReady waits for the stream to be ready (initial segments available).
func (p *PlexProfile) WaitReady(timeout time.Duration) error {
	select {
	case <-p.ready:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for stream to be ready")
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// waitForSegments waits for a specific number of segments to be created.
// This ensures fast startup by pre-buffering segments before serving playlist.
func (p *PlexProfile) waitForSegments(ctx context.Context, minSegments int) error {
	playlistPath := p.GetPlaylistPath()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Timeout: 30 seconds (Enigma2 tuning can take 2-5s, but allow more for slow receivers)
	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for %d segments", minSegments)
		case <-ticker.C:
			// Check if playlist exists and has enough segments
			if count, err := p.countSegments(playlistPath); err == nil && count >= minSegments {
				p.logger.Info().
					Int("segments", count).
					Int("min_required", minSegments).
					Msg("initial segments ready, stream can start")
				return nil
			}
		}
	}
}

// countSegments counts the number of segments listed in the playlist.
func (p *PlexProfile) countSegments(playlistPath string) (int, error) {
	// Check if playlist exists
	if _, err := os.Stat(playlistPath); err != nil {
		return 0, err
	}

	// Read playlist
	// playlistPath is constructed from validated outputDir (via secureJoin)
	data, err := os.ReadFile(playlistPath) // #nosec G304
	if err != nil {
		return 0, err
	}

	// Count .ts entries in playlist
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), ".ts") {
			count++
		}
	}

	return count, nil
}

// ServePlaylist serves the HLS playlist with correct Content-Type for Plex.
func (p *PlexProfile) ServePlaylist(w http.ResponseWriter) error {
	playlistPath := p.GetPlaylistPath()

	// Read playlist
	// playlistPath is constructed from validated outputDir (via secureJoin)
	data, err := os.ReadFile(playlistPath) // #nosec G304
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	// Set Plex-compatible headers
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write playlist: %w", err)
	}

	return nil
}

// ServeSegment serves an HLS segment with correct Content-Type.
func (p *PlexProfile) ServeSegment(w http.ResponseWriter, segmentName string) error {
	// Validate segment path
	segmentPath, err := secureJoin(p.GetOutputDir(), segmentName)
	if err != nil {
		return fmt.Errorf("invalid segment path: %w", err)
	}

	// Wait for segment to exist (with timeout)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(segmentPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Open segment file
	// segmentPath is validated via secureJoin above
	file, err := os.Open(segmentPath) // #nosec G304
	if err != nil {
		return fmt.Errorf("open segment: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			p.logger.Warn().Err(err).Msg("failed to close segment file")
		}
	}()

	// Get file size for Content-Length
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat segment: %w", err)
	}

	// Set headers for MPEG-TS segment
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Cache-Control", "max-age=10")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		return fmt.Errorf("write segment: %w", err)
	}

	return nil
}

// IsPlexClient detects if the request is from a Plex client.
// Returns true for both Plex Media Server (backend) and Plex apps (iOS, Android, etc.)
func IsPlexClient(userAgent string) bool {
	ua := strings.ToLower(userAgent)
	return strings.Contains(ua, "plex") ||
		strings.Contains(ua, "plexmediaserver")
}

// IsPlexDirectStream detects if this is a Plex DIRECT stream request.
// Plex sends different requests:
//   - Initial probe: "Plex Media Server/..." (should get original stream)
//   - Direct stream: Contains "DirectStream" or "directPlay" (optimized HLS)
//   - Transcoding: Contains "transcode" (let Plex handle it)
func IsPlexDirectStream(userAgent string, requestPath string) bool {
	ua := strings.ToLower(userAgent)
	path := strings.ToLower(requestPath)

	// If Plex is explicitly transcoding, don't interfere
	if strings.Contains(path, "transcode") || strings.Contains(ua, "transcode") {
		return false
	}

	// Direct Play/Direct Stream requests should use optimized profile
	if strings.Contains(ua, "directstream") || strings.Contains(ua, "directplay") {
		return true
	}

	// Default: Plex is probing or wants original stream
	// Let Plex decide if it needs to transcode based on network conditions
	return false
}

// IsIOSPlexClient detects if the request is from Plex on iOS/iPadOS.
func IsIOSPlexClient(userAgent string) bool {
	ua := strings.ToLower(userAgent)
	return IsPlexClient(userAgent) && (strings.Contains(ua, "iphone") ||
		strings.Contains(ua, "ipad") ||
		strings.Contains(ua, "ios"))
}

// ShouldUsePlexProfile determines if a request should use the Plex/iOS profile.
// Returns true if:
//   - User-Agent indicates Plex client (any platform)
//   - Plex profile is explicitly enabled via environment
func ShouldUsePlexProfile(userAgent string) bool {
	// Check if Plex profile is enabled
	if enabled := os.Getenv("XG2G_PLEX_PROFILE_ENABLED"); enabled != "" {
		if strings.ToLower(enabled) != "true" {
			return false
		}
	} else {
		// Default: enabled for Plex clients
		return IsPlexClient(userAgent)
	}

	return IsPlexClient(userAgent)
}
