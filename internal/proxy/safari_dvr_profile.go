// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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
	"time"

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
)

// SafariDVRProfile represents an HLS profile optimized for Safari's native DVR/seekable behavior.
// This implementation uses MPEG-TS with a large sliding window (30-45 minutes) to enable
// proper scrubbing/seeking in Safari's native HLS stack.
//
// Key characteristics:
//   - Container: fMP4/CMAF (.m4s) for best modern Safari compatibility and DVR
//   - Segment Duration: 2 seconds (per user requirement for Phase 3)
//   - DVR Window: 30-45 minutes (1800-2700 seconds)
//   - Sliding Window: Large hls_list_size to maintain seekable range
//
// Safari DVR Requirements:
//   - Safari calculates video.seekable range based strictly on playlist window
//   - Requires large EXT-X-MEDIA-SEQUENCE range for scrubber UI to appear
//   - PROGRAM-DATE-TIME enables absolute time mapping
//   - Init segment (init.mp4) required for fMP4
//
// Use cases:
//   - Safari on iOS/macOS (native HLS stack)
//   - Live streams where DVR/rewind functionality is critical
//   - Scenarios where latency can be sacrificed for better UX
type SafariDVRProfile struct {
	serviceRef   string
	targetURL    string
	outputDir    string
	cmd          *exec.Cmd
	ctx          context.Context
	cancel       context.CancelFunc
	logger       zerolog.Logger
	lastAccess   time.Time
	mu           sync.RWMutex
	started      bool
	stopping     bool
	config       streamprofile.SafariDVRConfig
	ready        chan struct{} // Signals when initial segments are ready
	ffmpegPath   string
	startTime    time.Time
	waitCh       chan error // Buffered channel for process exit result
	done         chan struct{}
	exitErr      error
	readyChecker ReadyChecker
	stderrBuf    *LineRing // Thread-safe ring buffer for stderr

	// Telemetry State
	programID       int
	inputSource     string        // "webapi" or "direct"
	startupDuration time.Duration // Time until first segment ready
}

// Local readStableFile removed in favor of proxy.ReadStableFile wrapper
// which handles the sleep/debouncing more efficiently.

// NewSafariDVRProfile creates a new Safari DVR profile.
func NewSafariDVRProfile(serviceRef, targetURL, baseDir string, logger zerolog.Logger, config streamprofile.SafariDVRConfig, checker ReadyChecker) (*SafariDVRProfile, error) {
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
		serviceRef:   serviceRef,
		targetURL:    targetURL,
		outputDir:    outputDir,
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger.With().Str("component", "safari_dvr_profile").Str("service_ref", serviceRef).Logger(),
		lastAccess:   time.Now(),
		config:       config,
		ready:        make(chan struct{}),
		ffmpegPath:   config.FFmpegPath,
		readyChecker: checker,
		stderrBuf:    NewLineRing(100), // Catch last 100 lines
	}, nil
}

// Start starts the Safari DVR HLS segmentation process.
// This process ensures:
//   - fMP4/CMAF container for modern Safari compatibility
//   - Large sliding window (30-45 min) for DVR scrubbing
//   - EVENT playlist type with EXT-X-START hint
//   - PROGRAM-DATE-TIME for absolute time mapping
//   - 2s segment duration for responsive start
func (p *SafariDVRProfile) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopping {
		return fmt.Errorf("safari dvr profile is stopping")
	}
	if p.started {
		return nil
	}
	// New readiness signal per start; profiles can be restarted if ffmpeg exits.
	ready := make(chan struct{})
	p.ready = ready
	p.startTime = time.Now()
	// Initialize wait channel (buffered to prevent blocking)
	p.waitCh = make(chan error, 1)
	p.done = make(chan struct{})
	p.exitErr = nil

	// Refresh context (important for restartability)
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel

	playlistPath := filepath.Join(p.outputDir, "playlist.m3u8")
	sessionID := strconv.FormatInt(time.Now().UnixNano(), 36)
	// Use .m4s for fMP4 segments
	segmentPattern := filepath.Join(p.outputDir, fmt.Sprintf("segment_%s_%%05d.m4s", sessionID))
	initSegmentName := "init.mp4"

	// Ensure clean state by removing previous output
	_ = os.RemoveAll(p.outputDir)
	if err := os.MkdirAll(p.outputDir, 0750); err != nil {
		p.cancel()
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
		// Fix: Extract technical ServiceRef from targetURL for accurate readiness checking.
		// ReadyChecker compares against the receiver's reported ServiceRef (e.g. "1:0:19..."),
		// but p.serviceRef might be a slug (e.g. "orf1-hd...").
		technicalRef := p.serviceRef
		if u, err := url.Parse(p.targetURL); err == nil {
			q := u.Query()
			if val := q.Get("ref"); val != "" {
				technicalRef = val
			} else if val := q.Get("sRef"); val != "" {
				technicalRef = val
			} else {
				// Checks for direct stream URL path (e.g. /1:0:19...)
				pathRef := strings.TrimPrefix(u.Path, "/")
				if strings.Contains(pathRef, ":") {
					technicalRef = pathRef
				}
			}
		}

		p.logger.Info().Str("web_api_url", sanitizeURL(webAPIURL)).Str("tech_ref", technicalRef).Msg("attempting to resolve Web API stream (Zapping)")
		// Use centralized helper with robust readiness checks
		url, pid, err := ZapAndResolveStream(ctx, webAPIURL, technicalRef, p.readyChecker)
		if err != nil {
			p.logger.Error().Err(err).Str("web_api_url", sanitizeURL(webAPIURL)).Msg("failed to resolve Web API stream")
		} else {
			finalInputURL = url
			programID = pid
			p.logger.Info().Str("resolved_url", sanitizeURL(finalInputURL)).Int("program_id", programID).Msg("successfully resolved stream URL")
		}
	} else {
		p.logger.Info().Msg("using direct stream URL (no Web API detected)")
	}

	p.programID = programID
	if webAPIURL != p.targetURL {
		p.inputSource = "webapi"
	} else {
		p.inputSource = "direct"
	}

	metrics.StreamStartAttempts.WithLabelValues("safari_dvr", "init").Inc()

	// Tune FFmpeg Probe/Analysis parameters
	// If we successfully resolved a ProgramID via WebAPI, we can trust the stream structure more
	// and use significantly faster startup parameters.
	analyzeDuration := "7000000" // 7s (default safe)
	probeSize := "10000000"      // 10MB (default safe)

	if programID > 0 {
		analyzeDuration = "2000000" // 2s (fast start)
		probeSize = "2000000"       // 2MB (fast start)
		p.logger.Info().Msg("using optimized fast-start analysis parameters")
	}

	// Input options (robust for Enigma2 streams)
	args = append(args,
		"-err_detect", "ignore_err",
		"-ignore_unknown",
		"-fflags", "+genpts+igndts+discardcorrupt",
		"-analyzeduration", analyzeDuration,
		"-probesize", probeSize,
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

	// Video transcoding (Default: Copy for speed/quality, Transcode only if needed)
	// User Requirement: Video copy (Default), Audio AAC 160k (Default)
	args = append(args,
		"-c:v", "copy",
	)

	// Note: If input is not H.264/HEVC, copy might fail or be incompatible with fMP4.
	// But Enigma2 streams are usually H.264.
	// We might need a flag to force transcode if input is weird, but "Default Copy" was requested.

	// Audio handling (Always AAC for Safari)
	args = append(args,
		"-c:a", "aac",
		"-b:a", "160k",
		"-ac", "2",
		"-ar", "48000",
		"-af", "aresample=async=1",
	)

	// HLS output options (Safari DVR optimized - fMP4)
	args = append(args,
		"-f", "hls",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", initSegmentName,
		"-hls_time", "2", // 2s segments as requested
		"-hls_list_size", fmt.Sprintf("%d", hlsListSize),
		// Flags: independent_segments+append_list+omit_endlist (and temp_file for safety)
		"-hls_flags", "independent_segments+append_list+omit_endlist+temp_file+program_date_time",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	p.logger.Info().
		Str("target", sanitizeURL(p.targetURL)).
		Str("output", playlistPath).
		Int("segment_duration", 2).
		Int("dvr_window_seconds", p.config.DVRWindowSize).
		Int("hls_list_size", hlsListSize).
		Str("container", "fmp4").
		Msg("starting Safari DVR profile (fMP4, Copy Video, AAC Audio)")

	p.cmd = exec.CommandContext(p.ctx, p.ffmpegPath, args...) // #nosec G204
	p.cmd.Dir = p.outputDir

	// Ensure process group for consistent cleanup
	procgroup.Set(p.cmd)

	// Capture stderr for debugging
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		p.cancel()
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		p.cancel()
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	p.started = true

	// Monitor FFmpeg stderr
	go func() {
		// Use LineRing to capture tail, but also log debug lines.
		scanner := bufio.NewScanner(stderr)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			p.stderrBuf.Add(line) // Race-safe add

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
	go p.watchdogRoutine(ctx, playlistPath)

	// Wait for FFmpeg process to exit - SINGLE WAITER
	cmd := p.cmd
	waitCh := p.waitCh
	done := p.done
	startTime := p.startTime

	// Capture telemetry context locally
	tmProgramID := p.programID
	tmInputSrc := p.inputSource

	go func() {
		// Wait for command to finish
		waitErr := cmd.Wait()

		// Determine exit reason for metrics BEFORE unlocking/cleanup
		exitReason := "success"
		if waitErr != nil {
			// If context is canceled, it was likely deliberate Stop()
			if ctx.Err() != nil {
				exitReason = "profile_stopped"
			} else {
				// Otherwise it crashed
				exitReason = "ffmpeg_exit"
				p.logger.Error().Err(waitErr).Msg("ffmpeg process exited with error")
			}
		} else {
			// Clean exit
			if ctx.Err() != nil {
				exitReason = "profile_stopped"
			} else {
				p.logger.Info().Msg("ffmpeg process stopped naturally")
			}
		}

		// Emit Metric with reason code
		// exit status 183 is common
		exitCode := "unknown"
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = fmt.Sprintf("%d", exitErr.ExitCode())
			}
		} else {
			exitCode = "0"
		}

		// If exit code is non-zero (or 183), capture stderr tail from RingBuffer
		var stderrTail string
		if exitCode != "0" {
			stderrTail = p.stderrBuf.String()
		}

		metrics.IncFFmpegExit("safari_dvr", exitCode, exitReason)

		// Retrieve startup duration safely
		p.mu.RLock()
		startupDur := p.startupDuration
		p.mu.RUnlock()

		p.mu.Lock()
		p.exitErr = waitErr
		p.started = false
		p.mu.Unlock()

		close(done)

		// P2.5 Observability Metric
		metrics.ObserveStreamSession("hls_safari", exitReason, time.Since(startTime).Seconds())

		// PROPOSAL 1.C: Structured Session Log
		// event=hls.session_start (per user request name, though implies summary/end)
		p.logger.Info().
			Str("event", "hls.session_end").
			Str("profile", "safari_dvr").
			Str("input", tmInputSrc).
			Int("program_id", tmProgramID).
			Int64("segments_ready_ms", startupDur.Milliseconds()).              // Time until segments ready
			Int64("session_duration_ms", time.Since(startTime).Milliseconds()). // Total session duration
			Str("exit_reason", exitReason).
			Str("exit_code", exitCode).
			Str("stderr_tail", stderrTail).
			Msg("stream session ended")

		// Send result to wait channel and close it
		waitCh <- waitErr
		close(waitCh)
	}()

	// Wait for initial segments to be ready
	go p.waitForSegments(ctx, playlistPath, ready)

	return nil
}

// watchdogRoutine monitors the playlist file for updates.
// If the playlist stops updating, it assumes ffmpeg has stalled and kills the process.
func (p *SafariDVRProfile) watchdogRoutine(ctx context.Context, playlistPath string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Give ffmpeg some time to start up
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	var lastModTime time.Time
	stallCount := 0
	maxStalls := 15 // 15 * 2s = 30s timeout

	for {
		select {
		case <-ctx.Done():
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

				// Use Stop() to cleanup properly instead of raw Process.Kill
				go p.Stop()
				return
			}
		}
	}
}

// waitForSegments waits for initial segments to be written before signaling ready.
func (p *SafariDVRProfile) waitForSegments(ctx context.Context, playlistPath string, ready chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	minSegments := p.config.StartupSegments

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			currentReady := p.ready
			started := p.started
			p.mu.RUnlock()
			if currentReady != ready || !started {
				return
			}

			data, err := ReadStableFile(ctx, playlistPath, 10*time.Millisecond, 250*time.Millisecond)
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

			// Also check for init.mp4
			initPath := filepath.Join(p.outputDir, "init.mp4")
			if _, err := os.Stat(initPath); err != nil {
				continue // Init segment not ready
			}

			if readyCount < minSegments {
				continue
			}

			p.logger.Info().
				Int("segments_ready", readyCount).
				Msg("Safari DVR profile ready")

			startupDur := time.Since(p.startTime)
			p.mu.Lock()
			p.startupDuration = startupDur
			p.mu.Unlock()

			// P2.5 Observability Metric
			metrics.ObserveHLSStartup("safari_dvr", startupDur.Seconds())
			close(ready)
			return
		}
	}
}

// WaitReady waits for the profile to be ready with a timeout.
func (p *SafariDVRProfile) WaitReady(timeout time.Duration) error {
	p.mu.RLock()
	ready := p.ready
	done := p.done
	p.mu.RUnlock()
	if ready == nil {
		return fmt.Errorf("safari dvr profile not initialized")
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ready:
		return nil
	case <-done:
		p.mu.RLock()
		exitErr := p.exitErr
		p.mu.RUnlock()
		if exitErr == nil {
			return fmt.Errorf("safari dvr profile stopped before ready")
		}
		return fmt.Errorf("safari dvr ffmpeg exited before ready: %w", exitErr)
	case <-timer.C:
		return fmt.Errorf("timeout waiting for Safari DVR profile to be ready")
	}
}

// Stop stops the Safari DVR profile and cleans up resources.
func (p *SafariDVRProfile) Stop() {
	p.mu.Lock()
	if p.stopping {
		p.mu.Unlock()
		return
	}
	p.stopping = true

	started := p.started
	cmd := p.cmd
	waitCh := p.waitCh
	cancel := p.cancel
	outputDir := p.outputDir
	p.mu.Unlock()

	p.logger.Info().Msg("stopping Safari DVR profile")

	// Ensure context is cancelled
	if cancel != nil {
		cancel()
	}

	// Robust termination using process groups
	// This blocks until proper exit or kill + grace period
	if started && cmd != nil && waitCh != nil {
		if err := procgroup.Terminate(cmd, waitCh, 2*time.Second); err != nil {
			p.logger.Warn().Err(err).Msg("process group termination reported error")
		}
	}

	// Cleanup output directory
	if err := os.RemoveAll(outputDir); err != nil {
		p.logger.Error().Err(err).Msg("failed to cleanup Safari DVR output directory")
	}

	p.mu.Lock()
	p.started = false
	p.cmd = nil
	p.waitCh = nil
	p.stopping = false
	p.mu.Unlock()

	p.logger.Info().Msg("Safari DVR profile stopped gracefully")
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

// ServeSegment serves a Safari DVR HLS segment (.m4s or .mp4).
func (p *SafariDVRProfile) ServeSegment(ctx context.Context, w http.ResponseWriter, segmentName string) error {
	p.UpdateAccess()

	// Validate segment name to prevent path traversal
	segmentPath, err := secureJoin(p.outputDir, segmentName)
	if err != nil {
		return fmt.Errorf("invalid segment path: %w", err)
	}

	// Wait for segment to exist (up to 10 seconds) with Context awareness
	timer := time.NewTimer(0)
	defer timer.Stop()

	// 10s timeout derived from previous loop (100 * 100ms)
	timeout := time.After(10 * time.Second)

	found := false
	for !found {
		select {
		case <-ctx.Done():
			return ctx.Err() // Client request cancel
		case <-p.ctx.Done():
			return p.ctx.Err() // Server requested stop
		case <-timeout:
			return fmt.Errorf("timeout waiting for segment")
		case <-timer.C:
			if _, err := os.Stat(segmentPath); err == nil {
				found = true
			} else {
				timer.Reset(100 * time.Millisecond)
			}
		}
	}

	// Open segment file
	file, err := os.Open(segmentPath) // #nosec G304 - validated via secureJoin
	if err != nil {
		return fmt.Errorf("open segment: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	// Set content type based on extension
	if strings.HasSuffix(segmentName, ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
	} else {
		w.Header().Set("Content-Type", "video/iso.segment") // m4s
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Segments never change
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		return fmt.Errorf("write segment: %w", err)
	}

	return nil
}

// User-Agent helpers moved to internal/core/useragent; see useragent.go for wrappers.

// LineRing is a thread-safe ring buffer for storing the last N lines of text.
type LineRing struct {
	lines []string
	max   int
	mu    sync.RWMutex
}

func NewLineRing(max int) *LineRing {
	return &LineRing{
		lines: make([]string, 0, max),
		max:   max,
	}
}

func (r *LineRing) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) >= r.max {
		// Shift
		// Note: This is O(n), but max is small (100) and this is per-line write on stderr.
		// Performance impact is negligible for this use case.
		r.lines = r.lines[1:]
	}
	r.lines = append(r.lines, line)
}

func (r *LineRing) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return strings.Join(r.lines, "\n")
}
