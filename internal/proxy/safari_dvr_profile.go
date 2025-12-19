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

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/rs/zerolog"
)

// SafariDVRProfile represents an HLS profile optimized for Safari's native DVR/seekable behavior.
// This implementation uses MPEG-TS with a large sliding window (30-45 minutes) to enable
// proper scrubbing/seeking in Safari's native HLS stack.
//
// Key characteristics:
//   - Container: MPEG-TS (.ts) for maximum Safari compatibility
//   - Segment Duration: 6 seconds (conservative, reduces manifest reload frequency)
//   - DVR Window: 30-45 minutes (1800-2700 seconds)
//   - Sliding Window: Large hls_list_size to maintain seekable range
//
// Safari DVR Requirements:
//   - Safari calculates video.seekable range based strictly on playlist window
//   - Requires large EXT-X-MEDIA-SEQUENCE range for scrubber UI to appear
//   - PROGRAM-DATE-TIME enables absolute time mapping
//
// Use cases:
//   - Safari on iOS/macOS (native HLS stack)
//   - Live streams where DVR/rewind functionality is critical
//   - Scenarios where latency can be sacrificed for better UX
type SafariDVRProfile struct {
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
	config     streamprofile.SafariDVRConfig
	ready      chan struct{} // Signals when initial segments are ready
	ffmpegPath string
	startTime  time.Time
}

// Local readStableFile removed in favor of proxy.ReadStableFile wrapper
// which handles the sleep/debouncing more efficiently.

// NewSafariDVRProfile creates a new Safari DVR profile.
func NewSafariDVRProfile(serviceRef, targetURL, baseDir string, logger zerolog.Logger, config streamprofile.SafariDVRConfig) (*SafariDVRProfile, error) {
	// Create unique directory for this profile
	streamID := sanitizeServiceRef(serviceRef)
	outputDir, err := secureJoin(filepath.Join(baseDir, "safari-dvr"), streamID)
	if err != nil {
		return nil, fmt.Errorf("create output path: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &SafariDVRProfile{
		serviceRef: serviceRef,
		targetURL:  targetURL,
		outputDir:  outputDir,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger.With().Str("component", "safari_dvr_profile").Str("service_ref", serviceRef).Logger(),
		lastAccess: time.Now(),
		config:     config,
		ready:      make(chan struct{}),
		ffmpegPath: config.FFmpegPath,
	}, nil
}

// Start starts the Safari DVR HLS segmentation process.
// This process ensures:
//   - MPEG-TS container for maximum Safari compatibility
//   - Large sliding window (30-45 min) for DVR scrubbing
//   - EVENT playlist type with EXT-X-START hint
//   - PROGRAM-DATE-TIME for absolute time mapping
//   - Conservative segment size (6s) to reduce manifest reload frequency
func (p *SafariDVRProfile) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}
	// New readiness signal per start; profiles can be restarted if ffmpeg exits.
	ready := make(chan struct{})
	p.ready = ready
	p.startTime = time.Now()

	playlistPath := filepath.Join(p.outputDir, "playlist.m3u8")
	sessionID := strconv.FormatInt(time.Now().UnixNano(), 36)
	segmentPattern := filepath.Join(p.outputDir, fmt.Sprintf("segment_%s_%%05d.ts", sessionID))

	// Ensure clean state by removing previous output
	_ = os.RemoveAll(p.outputDir)
	if err := os.MkdirAll(p.outputDir, 0750); err != nil {
		return fmt.Errorf("re-create output directory: %w", err)
	}

	// Calculate hls_list_size based on DVR window
	// DVRWindowSize / SegmentDuration = number of segments to keep
	hlsListSize := p.config.DVRWindowSize / p.config.SegmentDuration
	if hlsListSize < 100 {
		hlsListSize = 100 // Minimum for meaningful DVR
	}

	// Build FFmpeg command for Safari DVR optimization
	args := []string{
		"-hide_banner",
	}
	args = append(args, logLevelArgs("info", "")...)

	// Resolve Web API if applicable
	finalInputURL := p.targetURL
	webAPIURL := convertToWebAPI(p.targetURL, p.serviceRef)
	var programID int
	if strings.Contains(p.targetURL, "/web/stream.m3u") || webAPIURL != p.targetURL {
		p.logger.Info().Str("web_api_url", webAPIURL).Msg("attempting to resolve Web API stream (Zapping)")
		resolved, err := resolveWebAPIStreamInfo(webAPIURL)
		if err != nil {
			p.logger.Error().Err(err).Str("web_api_url", webAPIURL).Msg("failed to resolve Web API stream")
		} else {
			finalInputURL = resolved.URL
			programID = resolved.ProgramID
			p.logger.Info().Str("resolved_url", finalInputURL).Int("program_id", programID).Msg("successfully resolved stream URL")
			time.Sleep(1000 * time.Millisecond) // Give tuner time to lock
		}
	} else {
		p.logger.Info().Msg("using direct stream URL (no Web API detected)")
	}

	// Input options (robust for Enigma2 streams)
	args = append(args,
		"-err_detect", "ignore_err",
		"-ignore_unknown",
		"-fflags", "+genpts+igndts+discardcorrupt",
		"-analyzeduration", "7000000", // 7s analysis
		"-probesize", "10000000", // 10MB probe
		"-rw_timeout", "30000000", // 30s socket timeout
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-start_at_zero",
		"-avoid_negative_ts", "make_zero",
		// Skip a small amount of potentially garbage data at stream start (improves Safari stability).
		"-ss", "1.5",
		"-thread_queue_size", "4096",
		"-i", finalInputURL,
	)

	// Map streams
	if programID > 0 {
		args = append(args,
			"-map", fmt.Sprintf("0:p:%d:v:0", programID), // First video in program
			"-map", fmt.Sprintf("0:p:%d:a:0?", programID), // First audio in program (optional)
		)
	} else {
		args = append(args,
			"-map", "0:v:0", // First video
			"-map", "0:a:0?", // First audio (optional)
		)
	}

	// Video transcoding (H.264 for Safari compatibility)
	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-profile:v", "high",
		"-level", "4.1",
		"-pix_fmt", "yuv420p",
		"-crf", "18", // High quality
		"-vf", "yadif=0:-1:0", // Deinterlace
		"-g", "150", // GOP = 6s * 25fps
		"-keyint_min", "150",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", p.config.SegmentDuration),
		"-sc_threshold", "0",
		"-bsf:v", "h264_mp4toannexb,dump_extra", // SPS/PPS headers
	)

	// Audio handling
	if p.config.ForceAAC {
		args = append(args,
			"-c:a", "aac",
			"-profile:a", "aac_low",
			"-ac", "2", // Stereo
			"-ar", "48000",
			"-b:a", p.config.AACBitrate,
			"-af", "aresample=async=1",
		)
	} else {
		args = append(args,
			"-c:a", "copy",
		)
	}

	// HLS output options (Safari DVR optimized)
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", p.config.SegmentDuration),
		"-hls_list_size", fmt.Sprintf("%d", hlsListSize),
		// Use a classic live sliding window; Safari derives seekable range from the visible window.
		// temp_file prevents clients from fetching half-written segments (common cause of periodic restarts).
		"-hls_flags", "delete_segments+program_date_time+independent_segments+temp_file",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	p.logger.Info().
		Str("target", p.targetURL).
		Str("output", playlistPath).
		Int("segment_duration", p.config.SegmentDuration).
		Int("dvr_window_seconds", p.config.DVRWindowSize).
		Int("hls_list_size", hlsListSize).
		Str("container", "mpegts").
		Msg("starting Safari DVR profile (large sliding window for scrubbing)")

	p.cmd = exec.CommandContext(p.ctx, p.ffmpegPath, args...) // #nosec G204
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
		scanner := bufio.NewScanner(stderr)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), "error") {
				p.logger.Warn().Str("stderr", line).Msg("ffmpeg warning")
			} else {
				p.logger.Debug().Str("stderr", line).Msg("safari dvr ffmpeg stderr")
			}
		}

		if err := scanner.Err(); err != nil {
			p.logger.Debug().Err(err).Msg("ffmpeg stderr scanner error")
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

		exitReason := "success"
		if err != nil {
			if p.ctx.Err() == nil {
				exitReason = "ffmpeg_exit"
				p.logger.Error().Err(err).Msg("ffmpeg process exited with error")
			} else {
				exitReason = "client_disconnect" // or stop called
				p.logger.Info().Msg("ffmpeg process stopped")
			}
		} else {
			if p.ctx.Err() != nil {
				exitReason = "client_disconnect"
			}
			p.logger.Info().Msg("ffmpeg process stopped")
		}

		// P2.5 Observability Metric
		metrics.ObserveStreamSession("hls_safari", exitReason, time.Since(p.startTime).Seconds())
	}()

	// Wait for initial segments to be ready
	go p.waitForSegments(playlistPath, ready)

	return nil
}

// watchdogRoutine monitors the playlist file for updates.
// If the playlist stops updating, it assumes ffmpeg has stalled and kills the process.
func (p *SafariDVRProfile) watchdogRoutine(playlistPath string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Give ffmpeg some time to start up
	time.Sleep(10 * time.Second)

	var lastModTime time.Time
	stallCount := 0
	maxStalls := 15 // 15 * 2s = 30s timeout

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
				stallCount++
			} else {
				if !info.ModTime().After(lastModTime) {
					stallCount++
				} else {
					lastModTime = info.ModTime()
					stallCount = 0
				}
			}

			if stallCount >= maxStalls {
				p.logger.Error().
					Int("stall_seconds", stallCount*2).
					Msg("watchdog: stream stalled (no playlist updates), killing ffmpeg")

				if p.cmd != nil && p.cmd.Process != nil {
					_ = p.cmd.Process.Kill()
				}
				return
			}
		}
	}
}

// waitForSegments waits for initial segments to be written before signaling ready.
func (p *SafariDVRProfile) waitForSegments(playlistPath string, ready chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	minSegments := p.config.StartupSegments

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			currentReady := p.ready
			started := p.started
			p.mu.RUnlock()
			if currentReady != ready || !started {
				return
			}

			data, err := ReadStableFile(p.ctx, playlistPath, 10*time.Millisecond, 250*time.Millisecond)
			if err != nil || len(data) == 0 {
				continue
			}

			lines := strings.Split(string(data), "\n")
			readyCount := 0
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				segmentPath, segErr := secureJoin(p.outputDir, line)
				if segErr != nil {
					continue
				}
				info, statErr := os.Stat(segmentPath)
				if statErr != nil || info.Size() == 0 {
					break
				}
				readyCount++
				if readyCount >= minSegments {
					break
				}
			}

			if readyCount < minSegments {
				continue
			}

			p.logger.Info().
				Int("segments_ready", readyCount).
				Msg("Safari DVR profile ready")

			// P2.5 Observability Metric
			metrics.ObserveHLSStartup("safari", time.Since(p.startTime).Seconds())
			close(ready)
			return
		}
	}
}

// WaitReady waits for the profile to be ready with a timeout.
func (p *SafariDVRProfile) WaitReady(timeout time.Duration) error {
	p.mu.RLock()
	ready := p.ready
	p.mu.RUnlock()
	if ready == nil {
		return fmt.Errorf("safari dvr profile not initialized")
	}
	select {
	case <-ready:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for Safari DVR profile to be ready")
	}
}

// Stop stops the Safari DVR profile and cleans up resources.
func (p *SafariDVRProfile) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return
	}

	p.logger.Info().Msg("stopping Safari DVR profile")
	p.cancel()

	// Wait for process to exit (with timeout)
	done := make(chan struct{})
	go func() {
		if p.cmd != nil && p.cmd.Process != nil {
			// Wait for the command to finish
			_ = p.cmd.Wait()

		}
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info().Msg("Safari DVR profile stopped gracefully")
	case <-time.After(5 * time.Second):
		p.logger.Warn().Msg("force killing Safari DVR profile")
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()

		}
	}

	p.started = false

	// Cleanup output directory
	if err := os.RemoveAll(p.outputDir); err != nil {
		p.logger.Error().Err(err).Msg("failed to cleanup Safari DVR output directory")
	}
}

// UpdateAccess updates the last access time.
func (p *SafariDVRProfile) UpdateAccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastAccess = time.Now()
}

// IsIdle returns true if the profile hasn't been accessed within the timeout.
func (p *SafariDVRProfile) IsIdle(timeout time.Duration) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.lastAccess) > timeout
}

// ServePlaylist serves the Safari DVR HLS playlist.
// The playlist is enhanced with an EVENT hint to encourage Safari to expose scrubbing controls.
func (p *SafariDVRProfile) ServePlaylist(w http.ResponseWriter) error {
	p.UpdateAccess()

	playlistPath := filepath.Join(p.outputDir, "playlist.m3u8")
	// 10ms stability window is usually enough for local atomic writes
	data, err := ReadStableFile(p.ctx, playlistPath, 10*time.Millisecond, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	// Safari can be conservative about exposing a DVR scrubber while the playlist is still live.
	// Empirically, always hint EVENT semantics and start a bit behind the live edge (EXT-X-START)
	// improves stability (less "segment-boundary" stalling) and encourages a seekable UI once the
	// window has accumulated enough history.
	content := string(data)
	if strings.HasPrefix(content, "#EXTM3U") {
		lines := strings.Split(content, "\n")

		hasStart := false
		hasPlaylistType := false
		for _, line := range lines {
			if strings.HasPrefix(line, "#EXT-X-START:") {
				hasStart = true
			}
			if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
				hasPlaylistType = true
			}
			if hasStart && hasPlaylistType {
				break
			}
		}

		offsetSegments := p.config.StartupSegments
		if offsetSegments < 2 {
			offsetSegments = 2
		}
		offsetSeconds := offsetSegments * p.config.SegmentDuration
		if offsetSeconds < 1 {
			offsetSeconds = 1
		}

		startTag := fmt.Sprintf("#EXT-X-START:TIME-OFFSET=-%d,PRECISE=YES", offsetSeconds)
		playlistTypeTag := "#EXT-X-PLAYLIST-TYPE:EVENT"

		out := make([]string, 0, len(lines)+2)
		insertedStart := false
		insertedPlaylistType := false

		for i, line := range lines {
			if i == 0 && strings.HasPrefix(line, "#EXTM3U") {
				out = append(out, line)
				if !hasStart {
					out = append(out, startTag)
					insertedStart = true
				}
				continue
			}

			out = append(out, line)

			if !hasPlaylistType && !insertedPlaylistType && strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
				out = append(out, playlistTypeTag)
				insertedPlaylistType = true
			}
		}

		// If the playlist has no MEDIA-SEQUENCE yet (very early start), insert the EVENT hint near the top.
		if !hasPlaylistType && !insertedPlaylistType && len(out) > 0 {
			insertAt := 1
			if insertedStart {
				insertAt = 2
			}
			if insertAt > len(out) {
				insertAt = len(out)
			}
			out2 := make([]string, 0, len(out)+1)
			out2 = append(out2, out[:insertAt]...)
			out2 = append(out2, playlistTypeTag)
			out2 = append(out2, out[insertAt:]...)
			out = out2
		}

		data = []byte(strings.Join(out, "\n"))
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	// Safari builds its DVR timeline from what it has seen/retained; allowing a short-lived
	// cache of the manifest (aligned to segment duration) helps the native stack accumulate
	// a larger seekable window without over-polling.
	maxAge := p.config.SegmentDuration
	if maxAge < 1 {
		maxAge = 1
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d, must-revalidate", maxAge))
	w.Header().Del("Pragma")
	w.Header().Set("Expires", time.Now().Add(time.Duration(maxAge)*time.Second).UTC().Format(http.TimeFormat))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write playlist: %w", err)
	}

	return nil
}

// ServeSegment serves a Safari DVR HLS segment (.ts).
func (p *SafariDVRProfile) ServeSegment(w http.ResponseWriter, segmentName string) error {
	p.UpdateAccess()

	// Validate segment name to prevent path traversal
	segmentPath, err := secureJoin(p.outputDir, segmentName)
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
	file, err := os.Open(segmentPath) // #nosec G304 - validated via secureJoin
	if err != nil {
		return fmt.Errorf("open segment: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Segments never change
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		return fmt.Errorf("write segment: %w", err)
	}

	return nil
}

// User-Agent helpers moved to internal/core/useragent; see useragent.go for wrappers.
