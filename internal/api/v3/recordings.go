// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ManuGH/xg2g/internal/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/platform/net"

	ffmpegexec "github.com/ManuGH/xg2g/internal/pipeline/exec/ffmpeg"
	"github.com/ManuGH/xg2g/internal/platform/fs"
	"github.com/ManuGH/xg2g/internal/recordings"
)

// FFmpegProgress represents parsed metrics from ffmpeg -progress
type FFmpegProgress struct {
	OutTimeUs int64
	TotalSize int64
	Speed     string
	Fps       float64
	UpdatedAt time.Time
}

// hasAdvanced returns true if p represents forward progress compared to prev.
// Advances in either OutTimeUs or TotalSize are considered progress.
func (p FFmpegProgress) HasAdvanced(prev FFmpegProgress) bool {
	if p.OutTimeUs > prev.OutTimeUs {
		return true
	}
	if p.TotalSize > prev.TotalSize {
		return true
	}
	return false
}

// parseFFmpegProgress reads key=value lines from r and sends updates to ch.
// It effectively debounces updates, emitting accumulated state when proper keys are seen.
func ParseFFmpegProgress(r io.Reader, ch chan<- FFmpegProgress) {
	scanner := bufio.NewScanner(r)
	var current FFmpegProgress
	current.UpdatedAt = time.Now()

	// FFmpeg writes key=value pairs. "progress=continue" or "progress=end" usually flush a block.
	// But simply encountering "out_time_us" is often a good trigger or we can accumulate.
	// Best pattern: Accumulate until "progress=" line.

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

		switch key {
		case "out_time_us":
			if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.OutTimeUs = v
			}
		case "total_size":
			if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.TotalSize = v
			}
		case "speed":
			current.Speed = val
		case "fps":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				current.Fps = v
			}
		case "progress":
			// End of a report block
			if val == "continue" || val == "end" {
				current.UpdatedAt = time.Now()
				// Send a copy
				select {
				case ch <- current:
				default:
					// Drop update if channel is full to prevent stalling ffmpeg
				}
			}
		}
	}
}

// Types are now generated in server_gen.go

var (
	errRecordingInvalid  = errors.New("invalid recording ref")
	errRecordingNotReady = errors.New("recording not ready")
	errRecordingNotFound = errors.New("recording not found")
	errTooManyBuilds     = errors.New("too many concurrent builds")
)

const (
	recordingIDMinLen          = 16
	recordingIDMaxLen          = 1024 // Reduced from 2048 to limit potential payload size
	recordingRetryAfterSeconds = 2
	recordingBuildTimeout      = 2 * time.Hour
	recordingBuildStaleAfter   = 2 * time.Hour
	recordingBuildFailBackoff  = 30 * time.Second
)

// recordingBuildState removed Phase B

// P8.2: Type for Dependency Injection

// Typed Errors for Hardening
var (
	ErrProbeFailed       = errors.New("probe failed")
	ErrSourceUnavailable = errors.New("source unavailable")
	ErrFFmpegFatal       = errors.New("ffmpeg fatal error")
	ErrFFmpegStalled     = errors.New("ffmpeg stalled")
)

func ClassifyFFmpegError(stderr string, segmentsWritten int) error {
	// P8.2: Robust Classifier
	stderr = strings.ToLower(stderr)
	if stderr == "" && segmentsWritten == 0 {
		return ErrFFmpegFatal
	}

	// 1. Late Failure -> Runtime Issue (Not Probe)
	if segmentsWritten > 0 {
		return ErrFFmpegFatal
	}

	// 2. Auth / Missing Source -> Non-Retryable
	if strings.Contains(stderr, "401 unauthorized") ||
		strings.Contains(stderr, "403 forbidden") ||
		strings.Contains(stderr, "404 not found") ||
		strings.Contains(stderr, "connection refused") ||
		strings.Contains(stderr, "no route to host") {
		return ErrSourceUnavailable
	}

	// 3. Probe / init failure patterns -> Retryable
	probePatterns := []string{
		"could not find codec parameters",
		"no streams",
		"invalid data found",
		"error while decoding stream", // Generic decode error often fixed by transcode
	}
	for _, p := range probePatterns {
		if strings.Contains(stderr, p) {
			return ErrProbeFailed
		}
	}
	// Default to fatal if we can't classify
	return ErrFFmpegFatal
}

// CheckSourceAvailability performs a preflight check
// CheckSourceAvailability validates if a source URL is reachable and playable.
func CheckSourceAvailability(ctx context.Context, sourceURL string) error {
	// Only check HTTP(s) sources
	if !strings.HasPrefix(sourceURL, "http") {
		return nil
	}

	u, err := url.Parse(sourceURL)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Extract Auth if present
	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
		// Clear from URL to avoid leaking in logs if we printed it (we don't here, but good practice)
		u.User = nil
	}
	cleanURL := u.String()

	req, err := http.NewRequestWithContext(ctx, "HEAD", cleanURL, nil)
	if err != nil {
		return err
	}

	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}

	// Use a default client with timeout
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("preflight failed: %w", err)
	}

	// Robustness: Handle 405 Method Not Allowed by retrying with GET
	if resp.StatusCode == http.StatusMethodNotAllowed {
		// Drain and close HEAD response before retry
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		resp.Body.Close()

		// Create new GET request (avoid reusing potential state)
		retryReq, rErr := http.NewRequestWithContext(ctx, "GET", cleanURL, nil)
		if rErr != nil {
			return fmt.Errorf("preflight retry setup failed: %w", rErr)
		}
		if username != "" || password != "" {
			retryReq.SetBasicAuth(username, password)
		}

		// Request minimal content (align Range with drain limit)
		retryReq.Header.Set("Range", "bytes=0-4095")

		resp, err = client.Do(retryReq)
		if err != nil {
			return fmt.Errorf("preflight retry failed: %w", err)
		}
	}

	// Drain and close final response (connection reuse courtesy)
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)

	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
		return fmt.Errorf("%w: HTTP %d", ErrSourceUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("source error: HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetRecordingsRecordingIdStatus handles GET /api/v3/recordings/{recordingId}/status
func (s *Server) GetRecordingsRecordingIdStatus(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		s.writeRecordingPlaybackError(w, r, "", errRecordingInvalid)
		return
	}

	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	cacheDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	// 1. Check Active Build (Phase B: vodManager)
	if run := s.vodManager.Get(cacheDir); run != nil {
		resp := RecordingBuildStatus{
			State: RecordingBuildStatusStateRUNNING,
		}
		if run.Err != nil {
			resp.State = RecordingBuildStatusStateFAILED
			msg := run.Err.Error()
			resp.Error = &msg
		} else {
			// Check done channel if finished success?
			select {
			case <-run.Done:
				if run.Err == nil {
					resp.State = RecordingBuildStatusStateREADY
				}
			default:
				// Running
			}
		}

		progressiveReady := false
		if _, ok := RecordingLivePlaylistReady(cacheDir); ok {
			progressiveReady = true
		}
		resp.ProgressiveReady = &progressiveReady

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// 2. Check Completed
	state := RecordingBuildStatusStateIDLE
	progressiveReady := false
	if RecordingPlaylistReady(cacheDir) {
		// Validated final playlist
		state = RecordingBuildStatusStateREADY
	} else if path, ok := RecordingLivePlaylistReady(cacheDir); ok {
		// Valid progressive playlist
		_ = path
		state = RecordingBuildStatusStateRUNNING
		progressiveReady = true
	}

	resp := RecordingBuildStatus{
		State:            state,
		ProgressiveReady: &progressiveReady,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetRecordings handles GET /api/v3/recordings
// Query: ?root=<id>&path=<rel_path>
func (s *Server) GetRecordings(w http.ResponseWriter, r *http.Request, params GetRecordingsParams) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	// 1. Prepare Roots (Map manual params to query logic if needed, but generated code passes params struct)
	// We need to use params instead of parsing r.URL manually if we want to be clean,
	// but for now let's just use the method signature and existing logic if possible.
	// Wait, param signature changed!
	// Old: func (s *Server) GetRecordingsHandler(w http.ResponseWriter, r *http.Request)
	// New: func (s *Server) GetRecordings(w http.ResponseWriter, r *http.Request, params GetRecordingsParams)

	// I need to adapt the body to use 'params' or just ignore them and keep parsing URL if the URL is strictly equal.
	// But generated code calls with params.

	// Adapting body to use existing URL parsing or params.
	// Params.Root and Params.Path are pointers.

	// 1. Prepare Roots
	// 1. Prepare Roots (Configured + Discovered)
	// Helper to normalize root IDs consistently (lowercase, spaces to underscores)
	normalizeRootID := func(id string) string {
		return strings.ToLower(strings.ReplaceAll(id, " ", "_"))
	}

	// Collect all roots with normalized IDs and collision handling
	rootsRaw := make(map[string]string) // normalized ID -> path

	// Start with configured roots (normalize their IDs)
	if len(cfg.RecordingRoots) > 0 {
		for k, v := range cfg.RecordingRoots {
			normalizedID := normalizeRootID(k)
			// Collision handling for configured roots
			baseID := normalizedID
			counter := 2
			for {
				if _, exists := rootsRaw[normalizedID]; !exists {
					break
				}
				normalizedID = fmt.Sprintf("%s-%d", baseID, counter)
				counter++
			}
			rootsRaw[normalizedID] = v
		}
	}
	// Note: If no roots configured, discovery will populate.
	// If both are empty eventually, a default is added later.

	// Dynamic Discovery: Fetch locations from OpenWebIF
	client := s.newOpenWebIFClient(cfg, snap)
	if locs, err := client.GetLocations(r.Context()); err == nil {
		for _, loc := range locs {
			// Generate an ID for the root. Use the name if available, else sanitized path base.
			id := loc.Name
			if id == "" {
				id = path.Base(loc.Path)
			}
			normalizedID := normalizeRootID(id)

			// Collision handling: Suffix with -2, -3 etc if ID exists
			baseID := normalizedID
			counter := 2
			for {
				if _, exists := rootsRaw[normalizedID]; !exists {
					break
				}
				normalizedID = fmt.Sprintf("%s-%d", baseID, counter)
				counter++
			}

			rootsRaw[normalizedID] = loc.Path
		}
	} else {
		log.L().Warn().Err(err).Msg("failed to discover recording locations")
	}

	// Final check: if still empty, assume standard HDD
	if len(rootsRaw) == 0 {
		rootsRaw["hdd"] = "/media/hdd/movie"
	}

	// Use rootsRaw as the canonical roots map
	roots := rootsRaw

	rootList := make([]RecordingRoot, 0, len(roots))
	for id, pathStr := range roots {
		// Local vars for pointers
		i := id
		n := path.Base(pathStr)
		rootList = append(rootList, RecordingRoot{Id: &i, Name: &n})
	}
	// Sort for stability
	sort.Slice(rootList, func(i, j int) bool {
		// Dereference safely (though we just created them not nil)
		id1 := ""
		if rootList[i].Id != nil {
			id1 = *rootList[i].Id
		}
		id2 := ""
		if rootList[j].Id != nil {
			id2 = *rootList[j].Id
		}
		return id1 < id2
	})

	// 2. Parse Query
	var qRootID, qPath string
	if params.Root != nil {
		qRootID = *params.Root
	}
	if params.Path != nil {
		qPath = *params.Path
	}

	// If no root specified, return roots list (using first default for display)
	if qRootID == "" {
		if _, ok := roots["hdd"]; ok {
			qRootID = "hdd"
		} else if len(rootList) > 0 {
			if rootList[0].Id != nil {
				qRootID = *rootList[0].Id
			}
		} else {
			http.Error(w, "No recording roots configured", http.StatusInternalServerError)
			return
		}
	}

	// 3. Resolve & Validate Path
	// Security: Strict confinement using SanitizeRecordingRelPath
	rootAbs, ok := roots[qRootID]
	if !ok {
		http.Error(w, "Invalid root ID", http.StatusBadRequest)
		return
	}

	// 3. Resolve & Validate Path
	// ConfineRelPath uses local FS checks which fail for remote receiver paths.
	// We switch to string-based validation only using our POSIX helper.
	// Note: We assume qPath params (from net/url) are already URL-decoded,
	// so "a%2eb" comes in as "a.b". SanitizeRecordingRelPath handles the string form.
	cleanRel, blocked := SanitizeRecordingRelPath(qPath)
	if blocked {
		log.L().Warn().Str("path", qPath).Msg("path traversal attempt detected")
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Consistency: Use sanitized path for response structure and breadcrumbs
	qPath = cleanRel
	if qPath == "." {
		qPath = "" // Normalize root to empty for display/breadcrumbs
	}

	// Construct the target path to send to receiver
	// Note: rootAbs is the remote path on the receiver (e.g. /media/hdd/movie)
	// MUST use path.Join (POSIX) for receiver, not filepath.Join
	cleanTarget := path.Join(rootAbs, cleanRel)

	// 4. Fetch from Receiver
	// client is already initialized above for discovery
	list, err := client.GetRecordings(r.Context(), cleanTarget)
	if err != nil {
		log.L().Error().Err(err).Str("path", cleanTarget).Msg("failed to fetch recordings")
		http.Error(w, "Failed to fetch recordings from receiver", http.StatusBadGateway)
		return
	}

	// ... continues ...

	// 5. Build Response
	// Helper for pointers
	strPtr := func(s string) *string { return &s }
	int64Ptr := func(i int64) *int64 { return &i }

	recordingsList := make([]RecordingItem, 0, len(list.Movies))
	for _, m := range list.Movies {
		beginUnixSeconds := int64(m.Begin)
		desc := m.Description
		if m.ExtendedDescription != "" {
			if desc != "" {
				desc += "\n\n"
			}
			desc += m.ExtendedDescription
		}

		durationSeconds, durationKnown := ParseRecordingDurationSeconds(m.Length)

		// Fallback for missing length on local files (NAS)
		if (m.Length == "" || m.Length == "0") && s.recordingPathMapper != nil {
			// Extract filesystem path
			receiverPath := ExtractPathFromServiceRef(m.ServiceRef)
			if strings.HasPrefix(receiverPath, "/") {
				if localPath, ok := s.recordingPathMapper.ResolveLocal(receiverPath); ok {
					if info, err := os.Stat(localPath); err == nil {
						// Try robust probe first (Stufe 2)
						dur, pErr := ProbeDuration(r.Context(), localPath)
						if pErr == nil && dur > 0 {
							m.Length = fmt.Sprintf("%d min", int(dur.Minutes()))
							durationSeconds = int64(dur.Seconds())
							durationKnown = durationSeconds > 0
							// Operator-facing log for "VOD-mode eligibility"
							log.L().Info().
								Str("recording_id", m.ServiceRef).
								Int("duration_sec", int(dur.Seconds())).
								Str("duration_source", "ffprobe").
								Bool("mapped_local", true).
								Msg("recording duration resolved")
						} else {
							// Fallback to size heuristic (Stufe 3)
							log.L().Warn().Err(pErr).Str("file", localPath).Msg("probe failed, using heuristic")
							// Estimate duration: 8 Mbps (~1 MB/s)
							minutes := info.Size() / (60 * 1024 * 1024)
							if minutes > 0 {
								m.Length = fmt.Sprintf("%d min", minutes)
								durationSeconds = int64(minutes * 60)
								durationKnown = durationSeconds > 0
								log.L().Debug().Str("file", localPath).Int64("size", info.Size()).Msgf("estimated heuristic duration: %s", m.Length)
							}
						}
					}
				}
			}
		}

		recordingID := EncodeRecordingID(m.ServiceRef)
		var durationPtr *int64
		if durationKnown && durationSeconds > 0 {
			durationPtr = int64Ptr(durationSeconds)
		}

		recordingsList = append(recordingsList, RecordingItem{
			ServiceRef:       strPtr(m.ServiceRef),
			RecordingId:      strPtr(recordingID),
			Title:            strPtr(m.Title),
			Description:      strPtr(desc),
			BeginUnixSeconds: int64Ptr(beginUnixSeconds),
			DurationSeconds:  durationPtr,
			Length:           strPtr(m.Length),
			Filename:         strPtr(m.Filename),
		})
	}

	// Enrich with Resume Data
	if principal := auth.PrincipalFromContext(r.Context()); principal != nil {
		s.attachResumeSummaries(r.Context(), principal.ID, recordingsList)
	}

	directoriesList := make([]DirectoryItem, 0, len(list.Bookmarks))
	// Optimization: hoist root trim
	root := strings.TrimSuffix(rootAbs, "/")
	// Process Directories (Bookmarks)
	for _, b := range list.Bookmarks {
		// Bookmarks are absolute paths on the receiver.
		// Safe containment check: Must be proper subdirectory

		// Exact match check (optional, usually skipped as it's the current dir)
		if b.Path == rootAbs {
			continue
		}

		if !strings.HasPrefix(b.Path, root+"/") {
			continue
		}

		rel := strings.TrimPrefix(b.Path, root+"/")

		// Double check we haven't produced something absolute or odd
		if rel == "" || strings.HasPrefix(rel, "/") {
			continue
		}

		directoriesList = append(directoriesList, DirectoryItem{
			Name: strPtr(b.Name),
			Path: strPtr(rel),
		})
	}

	breadcrumbsList := make([]Breadcrumb, 0)
	if qPath != "" && qPath != "." {
		// Use slash for splitting as we mandate POSIX paths for receiver
		parts := strings.Split(qPath, "/")
		built := ""
		for _, p := range parts {
			if p == "" {
				continue
			}
			built = path.Join(built, p)
			breadcrumbsList = append(breadcrumbsList, Breadcrumb{
				Name: strPtr(p),
				Path: strPtr(built),
			})
		}
	}

	// Fix RootList to generated type
	genRoots := append([]RecordingRoot(nil), rootList...)

	response := RecordingResponse{
		Roots:       &genRoots,
		CurrentRoot: strPtr(qRootID),
		CurrentPath: strPtr(qPath),
		Recordings:  &recordingsList,
		Directories: &directoriesList,
		Breadcrumbs: &breadcrumbsList,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.L().Error().Err(err).Msg("failed to encode response")
	}
}

// GetRecordingPlaybackInfo determines the best playback strategy
func (s *Server) attachResumeSummaries(ctx context.Context, principalID string, items []RecordingItem) {
	if s.resumeStore == nil {
		return
	}

	logger := log.WithComponentFromContext(ctx, "resume")

	for i := range items {
		// Dereference pointer safely
		if items[i].RecordingId == nil {
			continue
		}
		rid := *items[i].RecordingId
		if rid == "" {
			continue
		}

		// Ensure basic validity of ID to avoid accidental serviceRef usage
		if !ValidRecordingID(rid) {
			continue
		}

		st, err := s.resumeStore.Get(ctx, principalID, rid)
		if err != nil {
			// Do not fail list rendering on resume issues; log and continue.
			logger.Debug().
				Err(err).
				Str("principal", principalID).
				Str("recording", rid).
				Msg("resume get failed (list enrichment)")
			continue
		}
		if st == nil {
			continue
		}

		// Marshal minimal summary.
		updatedAt := st.UpdatedAt.UTC()
		sum := &ResumeSummary{
			PosSeconds:      &st.PosSeconds,
			DurationSeconds: &st.DurationSeconds,
			Finished:        &st.Finished,
		}
		if !st.UpdatedAt.IsZero() {
			sum.UpdatedAt = &updatedAt
		}

		items[i].Resume = sum
	}
}

func (s *Server) GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	// ENTRY LOG (Temporary)
	log.L().Info().Str("recording_id", recordingId).Str("ua", r.UserAgent()).Msg("GetRecordingPlaybackInfo request received")

	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		s.writeRecordingPlaybackError(w, r, "", errRecordingInvalid)
		return
	}
	if err := ValidateRecordingRef(serviceRef); err != nil {
		s.writeRecordingPlaybackError(w, r, "", err)
		return
	}

	// 1. Resolve Path
	// We need to find if we have local access to the file to serve it directly.
	// If only accessible via receiver HTTP, we must fallback to HLS (unless we proxy-remux, which is heavy).

	// Try resolve local
	var localPath string
	receiverPath := ExtractPathFromServiceRef(serviceRef)
	if s.recordingPathMapper != nil {
		if p, ok := s.recordingPathMapper.ResolveLocal(receiverPath); ok {
			localPath = p
		}
	}

	mode := PlaybackInfoMode("hls") // Default
	url := fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", recordingId)
	reason := "remote_source"

	// 2. Logic: If Local & Finished -> Direct MP4
	// "Finished" check: File exists and is stable (not actively growing)
	if localPath != "" {
		if _, err := os.Stat(localPath); err == nil {
			// Stability check: Use IsStable() to detect actively recording files
			// If file modtime is within RecordingStableWindow, it's still growing
			// → fallback to HLS progressive playback
			isActive := !recordings.IsStable(localPath, s.cfg.RecordingStableWindow)

			if !isActive {
				// VOD Mode: File is stable, use progressive MP4
				mode = PlaybackInfoMode("direct_mp4")
				url = fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", recordingId)
				// Inject auth token for direct playback (headers not supported by video tag)
				if token := extractToken(r); token != "" {
					url += "?token=" + token
				}
				reason = "local_file_available"
			} else {
				reason = "file_growing"
			}
		}
	}

	var durationSec float32
	if localPath != "" {
		if d, err := ProbeDuration(r.Context(), localPath); err == nil {
			durationSec = float32(d.Seconds())
		}
	}

	resp := PlaybackInfo{
		Mode:            mode,
		Url:             url,
		Reason:          &reason,
		DurationSeconds: &durationSec,
	}

	// DIAGNOSTIC LOG (Temporary)
	log.L().Info().
		Str("recording_id", recordingId).
		Str("resolved_path", localPath).
		Str("mode", string(mode)).
		Str("reason", reason).
		Msg("GetRecordingPlaybackInfo decision")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// StreamRecordingDirect handles the direct playback (remux to MP4)
// GET /recordings/{recordingId}/stream.mp4
// StreamRecordingDirect handles the direct playback (remux to MP4)
// GET /recordings/{recordingId}/stream.mp4
func (s *Server) StreamRecordingDirect(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// 1. Resolve Local Path
	var localPath string
	receiverPath := ExtractPathFromServiceRef(serviceRef)
	if s.recordingPathMapper != nil {
		if p, ok := s.recordingPathMapper.ResolveLocal(receiverPath); ok {
			localPath = p
		}
	}

	if localPath == "" {
		http.Error(w, "Direct playback unavailable (remote source)", http.StatusNotFound)
		return
	}

	// 2. Compute Cache Path (Standardized VOD Cache)
	hash := sha1.Sum([]byte(serviceRef))
	cacheName := fmt.Sprintf("%x.mp4", hash)
	cacheDir := filepath.Join(s.cfg.DataDir, "vod-cache")
	cachePath := filepath.Join(cacheDir, cacheName)

	// 3. Unify Build Orchestration
	// Delegate to central asset manager which handles:
	// - Validation
	// - Cache checking (and versioning via VODCacheVersion)
	// - Concurrency control (Semaphore)
	// - Build scheduling (recordingRun map)
	_, err := s.ensureRecordingPlaybackAssets(r.Context(), serviceRef)
	if err == nil {
		// Success! Cache is ready.
		// Open file explicitly to ensure we can close it (ServeContent does not close)
		f, err := os.Open(cachePath)
		if err != nil {
			log.L().Error().Err(err).Str("path", cachePath).Msg("failed to open cached vod file")
			http.Error(w, "Stream Error", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		if info, err := f.Stat(); err == nil {
			http.ServeContent(w, r, "stream.mp4", info.ModTime(), f)
			return
		}
	} else if !errors.Is(err, errRecordingNotReady) {
		// Genuine Error
		log.L().Error().Err(err).Str("recording", recordingId).Msg("vod preparation failed")
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	// 4. Build In Progress (errRecordingNotReady)
	// Check central state for ETA calculation
	// Phase B: SOA VOD Manager
	var isRunning bool

	if run := s.vodManager.Get(cachePath); run != nil {
		select {
		case <-run.Done:
			// Run verified complete
			if run.Err != nil {
				s.writeRecordingPlaybackError(w, r, serviceRef, run.Err)
				return
			}
			// Success - serve it
			http.ServeFile(w, r, cachePath)
			return
		default:
			isRunning = true
		}
	}

	// If build is running (or just scheduled), return 503 with ETA
	if isRunning {
		// Dynamic ETA Calculation
		eta := 30
		if info, err := os.Stat(localPath); err == nil {
			// Estimate based on 40MB/s processing speed
			sizeMB := float64(info.Size()) / (1024 * 1024)
			eta = int(sizeMB/40.0) + 10
		}

		resp := map[string]interface{}{
			"code":        "PREPARING",
			"message":     fmt.Sprintf("Preparing video... Estimated wait: ~%ds", eta),
			"eta_seconds": eta,
			"retry_after": 5,
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// If not in map but ensure returned NotReady, it might be in queue or just failed.
	// Fallback generic wait.
	w.Header().Set("Retry-After", "5")
	http.Error(w, "Starting VOD Preparation", http.StatusServiceUnavailable)
}

// ExtractPathFromServiceRef extracts the filesystem path from an Enigma2 service reference.
// Enigma2 service references have the format: "1:0:0:0:0:0:0:0:0:0:/path/to/file.ts"
// Returns the path part (everything after the last colon) if it starts with "/",
// otherwise returns the original string unchanged (defensive).
func ExtractPathFromServiceRef(serviceRef string) string {
	// Find the last colon
	lastColon := strings.LastIndex(serviceRef, ":")
	if lastColon == -1 {
		// No colon found, return as-is
		return serviceRef
	}

	// Extract everything after the last colon
	pathPart := serviceRef[lastColon+1:]

	// Only return if it looks like an absolute path
	if strings.HasPrefix(pathPart, "/") {
		return pathPart
	}

	// Otherwise return original (defensive - might be a different format)
	return serviceRef
}

func (s *Server) resolveRecordingPlaybackSource(ctx context.Context, serviceRef string) (string, string, string, error) {
	// Phase 1: Strict Validation Check (before any normalization)
	if err := ValidateRecordingRef(serviceRef); err != nil {
		return "", "", "", err
	}
	// Logic continues with trimmed ref
	serviceRef = strings.TrimSpace(serviceRef)

	receiverPath := ExtractPathFromServiceRef(serviceRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return "", "", "", errRecordingInvalid
	}

	_, invalid := SanitizeRecordingRelPath(strings.TrimLeft(receiverPath, "/"))
	if invalid {
		return "", "", "", errRecordingInvalid
	}

	s.mu.RLock()
	host := s.cfg.OWIBase
	streamPort := s.cfg.StreamPort
	stableWindow := s.cfg.RecordingStableWindow
	policy := strings.ToLower(strings.TrimSpace(s.cfg.RecordingPlaybackPolicy))
	pathMapper := s.recordingPathMapper
	username := s.cfg.OWIUsername
	password := s.cfg.OWIPassword
	s.mu.RUnlock()

	allowLocal := policy != "receiver_only"
	allowReceiver := policy != "local_only"

	if allowLocal && pathMapper != nil {
		if localPath, ok := pathMapper.ResolveLocal(receiverPath); ok {
			parts, err := RecordingParts(localPath)
			if err == nil && len(parts) > 0 {
				if recordings.IsStable(parts[len(parts)-1], stableWindow) {
					var durationSeconds string
					if dur, pErr := ProbeDuration(ctx, localPath); pErr == nil && dur > 0 {
						durationSeconds = fmt.Sprintf("%.3f", dur.Seconds())
					} else if pErr != nil {
						log.L().Warn().Err(pErr).Str("source", localPath).Msg("failed to probe recording duration")
					}
					return "local", localPath, durationSeconds, nil
				}
				if !allowReceiver {
					return "", "", "", errRecordingNotReady
				}
				log.L().Debug().
					Str("local_path", localPath).
					Dur("stable_window", stableWindow).
					Msg("file unstable, falling back to receiver")
			} else if err != nil && !allowReceiver {
				return "", "", "", ErrRecordingNotFound
			}
		} else if !allowReceiver {
			return "", "", "", ErrRecordingNotFound
		}
	}

	if !allowReceiver {
		return "", "", "", ErrRecordingNotFound
	}

	h, _, err := net.NormalizeAuthority(host, "http")
	if err != nil {
		return "", "", "", fmt.Errorf("invalid OWI base: %w", err)
	}
	host = h

	// Enigma2 requires the ServiceRef to be literal (colons and all).
	// We use a custom escaping helper to ensure RawPath is valid UTF-8 percent-encoded
	// while strictly preserving colons and slashes.
	rawPath := EscapeServiceRefPath("/" + serviceRef)

	u := url.URL{
		Scheme:  "http",
		Host:    fmt.Sprintf("%s:%d", host, streamPort),
		Path:    "/" + serviceRef, // Decoded path.
		RawPath: rawPath,          // Properly encoded path (valid for Go net/url).
	}

	// Inject credentials if configured
	if username != "" && password != "" {
		u.User = url.UserPassword(username, password)
	}

	streamURL := u.String()

	return "receiver", streamURL, "", nil
}

// GetRecordingHLSPlaylist handles GET /api/v3/recordings/{recordingId}/playlist.m3u8.
// Recording playback is asset-based (VOD) and does not use v3 sessions.
func (s *Server) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		s.writeRecordingPlaybackError(w, r, "", errRecordingInvalid)
		return
	}

	playlistPath, err := s.ensureRecordingVODPlaylist(r.Context(), serviceRef)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	info, err := os.Stat(playlistPath)
	if err != nil || info.IsDir() {
		s.writeRecordingPlaybackError(w, r, serviceRef, errRecordingNotReady)
		return
	}
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, errRecordingNotReady)
		return
	}
	// Determine Playlist Type
	// Default: VOD
	playlistType := "VOD"
	isLivePlaylist := filepath.Base(playlistPath) == "index.live.m3u8"

	if isLivePlaylist {
		// Consistently check stability to determine if it's truly LIVE or just a finished file using the live playlist
		cacheDir := filepath.Dir(playlistPath)
		// If file is growing (active), use EVENT/LIVE mode.
		// If stable (finished), force VOD mode to ensure ENDLIST is added.
		if !recordings.IsStable(cacheDir, s.cfg.RecordingStableWindow) {
			playlistType = "EVENT"
		}
	}

	playlist := RewritePlaylistType(string(data), playlistType)
	http.ServeContent(w, r, "playlist.m3u8", info.ModTime(), bytes.NewReader([]byte(playlist)))
}

// GetRecordingHLSPlaylistHead handles HEAD /api/v3/recordings/{recordingId}/playlist.m3u8.
// Safari uses HEAD to check Content-Length. Delegates to GET handler (http.ServeContent handles HEAD).
func (s *Server) GetRecordingHLSPlaylistHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.GetRecordingHLSPlaylist(w, r, recordingId)
}

// GetRecordingHLSTimeshift handles GET /api/v3/recordings/{recordingId}/timeshift.m3u8.
// Recording playback is asset-based (timeshift) and does not use v3 sessions.
func (s *Server) GetRecordingHLSTimeshift(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		s.writeRecordingPlaybackError(w, r, "", errRecordingInvalid)
		return
	}

	playlistPath, err := s.ensureRecordingTimeshiftPlaylist(r.Context(), serviceRef)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	info, err := os.Stat(playlistPath)
	if err != nil || info.IsDir() {
		s.writeRecordingPlaybackError(w, r, serviceRef, errRecordingNotReady)
		return
	}
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, errRecordingNotReady)
		return
	}
	playlist := RewritePlaylistType(string(data), "EVENT")
	http.ServeContent(w, r, "timeshift.m3u8", info.ModTime(), bytes.NewReader([]byte(playlist)))
}

// GetRecordingHLSTimeshiftHead handles HEAD /api/v3/recordings/{recordingId}/timeshift.m3u8.
// Safari uses HEAD to check Content-Length. Delegates to GET handler (http.ServeContent handles HEAD).
func (s *Server) GetRecordingHLSTimeshiftHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.GetRecordingHLSTimeshift(w, r, recordingId)
}

// GetRecordingHLSCustomSegment serves the generated HLS segments.
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		s.writeRecordingPlaybackError(w, r, "", errRecordingInvalid)
		return
	}

	segment = filepath.Base(segment)
	if segment == "." || segment == ".." || strings.Contains(segment, "\\") {
		http.Error(w, "invalid segment name", http.StatusBadRequest)
		return
	}
	if !IsAllowedVideoSegment(segment) {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return
	}

	if _, err := s.ensureRecordingPlaybackAssets(r.Context(), serviceRef); err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	cacheDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		log.L().Error().Err(err).Msg("recording cache dir unavailable")
		http.Error(w, "recording storage unavailable", http.StatusServiceUnavailable)
		return
	}

	filePath, err := fs.ConfineRelPath(cacheDir, segment)
	if err != nil {
		http.Error(w, "segment not found", http.StatusNotFound)
		return
	}

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		http.Error(w, "segment not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "segment unavailable", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "segment not found", http.StatusNotFound)
		return
	}

	if segment == "init.mp4" {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Encoding", "identity") // Disable compression
	} else if strings.HasSuffix(segment, ".m4s") || strings.HasSuffix(segment, ".cmfv") {
		// Safari REQUIRES video/mp4 for all fMP4 content (not video/iso.segment)
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Encoding", "identity") // Disable compression
	} else {
		w.Header().Set("Content-Type", "video/MP2T")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Encoding", "identity") // Disable compression
	}

	f, err := os.Open(filePath) // #nosec G304 -- filePath is confined to recording cache dir.
	if err != nil {
		http.Error(w, "segment unavailable", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	http.ServeContent(w, r, segment, info.ModTime(), f)
}

// GetRecordingHLSCustomSegmentHead handles HEAD /api/v3/recordings/{recordingId}/{segment}.
// Safari uses HEAD to check Content-Length. Delegates to GET handler (http.ServeContent handles HEAD).
func (s *Server) GetRecordingHLSCustomSegmentHead(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	s.GetRecordingHLSCustomSegment(w, r, recordingId, segment)
}

func (s *Server) ensureRecordingPlaybackAssets(ctx context.Context, serviceRef string) (string, error) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	if err := ValidateRecordingRef(serviceRef); err != nil {
		return "", err
	}

	cacheDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}
	// 1. Check Final
	if RecordingPlaylistReady(cacheDir) {
		// P8: LRU Touch
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return filepath.Join(cacheDir, "index.m3u8"), nil
	}

	// 2. Check Live (Progressive VOD - Phase 6)
	if path, ok := RecordingLivePlaylistReady(cacheDir); ok {
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return path, nil
	}

	sourceType, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
	if err != nil {
		return "", err
	}

	// 3. Delegate to VOD Manager (Phase B)
	// Key: cacheDir (Directory Path for HLS)
	// Work: runRecordingBuild (HLS logic)

	// Probe params from config? Used to be passed in legacy
	probeSize := "50M" // Default (was s.cfg...) - actually let's access s.cfg safely
	analyzeDur := "100M"
	s.mu.RLock()
	// Access config under lock? Or is it safe?
	if s.cfg.VODProbeSize != "" {
		probeSize = s.cfg.VODProbeSize
	}
	if s.cfg.VODAnalyzeDuration != "" {
		analyzeDur = s.cfg.VODAnalyzeDuration
	}
	s.mu.RUnlock()

	_, isNew := s.vodManager.Ensure(ctx, cacheDir, func(ctx context.Context) error {
		// HLS Build Logic
		// Logic encapsulated in runRecordingBuild
		// Note: Legacy scheduleRecordingBuild did pre-checks and semaphores.
		// Manager handles exactly-once.
		// Does runRecordingBuild clean up cacheDir on start? Yes.
		return s.runRecordingBuild(ctx, cacheDir, sourceType, source, false, probeSize, analyzeDur)
	})

	if isNew {
		return "", errRecordingNotReady
	}
	// Existing run - check result?
	// If exists and running loops -> not ready.
	// We return NotReady regardless for async.
	return "", errRecordingNotReady
}

func (s *Server) ensureRecordingVODPlaylist(ctx context.Context, serviceRef string) (string, error) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	probeSize := "50M"
	analyzeDur := "100M"
	if s.cfg.VODProbeSize != "" {
		probeSize = s.cfg.VODProbeSize
	}
	if s.cfg.VODAnalyzeDuration != "" {
		analyzeDur = s.cfg.VODAnalyzeDuration
	}
	s.mu.RUnlock()

	if err := ValidateRecordingRef(serviceRef); err != nil {
		return "", err
	}

	cacheDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}

	if RecordingPlaylistReady(cacheDir) {
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return filepath.Join(cacheDir, "index.m3u8"), nil
	}

	// Fix: Support Progressive VOD (Phase 6)
	if path, ok := RecordingLivePlaylistReady(cacheDir); ok {
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return path, nil
	}

	sourceType, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
	if err != nil {
		return "", err
	}

	_, isNew := s.vodManager.Ensure(ctx, cacheDir, func(ctx context.Context) error {
		return s.runRecordingBuild(ctx, cacheDir, sourceType, source, false, probeSize, analyzeDur)
	})

	if isNew {
		return "", errRecordingNotReady
	}
	// Fallthrough to errRecordingNotReady if logic continues (original code returned err)
	// Original: if err != nil return err.
	// New: Ensure returns status. Logic matches ensureRecordingPlaybackAssets.

	return "", errRecordingNotReady
}

func (s *Server) ensureRecordingTimeshiftPlaylist(ctx context.Context, serviceRef string) (string, error) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	probeSize := "50M"
	analyzeDur := "100M"
	if s.cfg.VODProbeSize != "" {
		probeSize = s.cfg.VODProbeSize
	}
	if s.cfg.VODAnalyzeDuration != "" {
		analyzeDur = s.cfg.VODAnalyzeDuration
	}
	s.mu.RUnlock()

	if err := ValidateRecordingRef(serviceRef); err != nil {
		return "", err
	}

	cacheDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}

	if path, ok := RecordingLivePlaylistReady(cacheDir); ok {
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return path, nil
	}

	if RecordingPlaylistReady(cacheDir) {
		return "", errRecordingNotReady
	}

	sourceType, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
	if err != nil {
		return "", err
	}

	_, isNew := s.vodManager.Ensure(ctx, cacheDir, func(ctx context.Context) error {
		return s.runRecordingBuild(ctx, cacheDir, sourceType, source, false, probeSize, analyzeDur)
	})

	if isNew {
		return "", errRecordingNotReady
	}

	return "", errRecordingNotReady
}

// RecordingLivePlaylistReady checks if a valid progressive playlist exists.
// Criteria: index.live.m3u8 exists AND references at least one existing segment file.
func RecordingLivePlaylistReady(cacheDir string) (string, bool) {
	livePath := filepath.Join(cacheDir, "index.live.m3u8")

	// 1. Check Playlist Existence
	info, err := os.Stat(livePath)
	if err != nil || info.IsDir() {
		return "", false
	}

	// 2. Parse Playlist for valid segment reference
	data, err := os.ReadFile(livePath)
	if err != nil {
		return "", false
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// VOD Recording uses TS-HLS only (no fMP4), so we only check for .ts segments
	hasSegment := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		// Found a URI line (segment)
		// Security: confine segment path to cache dir
		// Validate segment name BEFORE path confinement/resolution to prevent bypass
		if !IsAllowedVideoSegment(l) {
			continue
		}

		safeSeg, err := fs.ConfineRelPath(cacheDir, l)
		if err != nil {
			continue
		}
		// Double check file extension on resolved path (Canonical check)
		if !IsAllowedVideoSegment(safeSeg) {
			continue
		}

		if _, err := os.Stat(safeSeg); err == nil {
			hasSegment = true
			break
		}
	}

	if hasSegment {
		return livePath, true
	}
	return "", false
}

func RewritePlaylistType(content, playlistType string) string {
	if playlistType == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	newLines := make([]string, 0, len(lines)+1)
	inserted := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
			continue
		}
		// Sanitize: Remove EXT-X-DISCONTINUITY (Safari Fix)
		if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY") {
			continue
		}
		newLines = append(newLines, line)
		if line == "#EXTM3U" && !inserted {
			newLines = append(newLines, "#EXT-X-PLAYLIST-TYPE:"+playlistType)
			inserted = true
		}
	}
	if !inserted {
		newLines = append([]string{"#EXT-X-PLAYLIST-TYPE:" + playlistType}, newLines...)
	}

	// Fix: VOD Playlists MUST have EXT-X-ENDLIST to be treated as finite/seekable by Safari
	if playlistType == "VOD" {
		hasEndlist := false
		for _, line := range lines {
			if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
				hasEndlist = true
				break
			}
		}
		if !hasEndlist {
			newLines = append(newLines, "#EXT-X-ENDLIST")
		}
	}

	return strings.Join(newLines, "\n")
}

func (s *Server) buildRecordingPlaylist(ctx context.Context, cacheDir, sourceType, source string) error {
	s.mu.RLock()
	probeSize := s.cfg.VODProbeSize
	analyzeDur := s.cfg.VODAnalyzeDuration
	s.mu.RUnlock()

	// Attempt 1: Configured (Fast) Probe -> Forced Transcode (Safari Fix)
	// We force transcode=true here to ensure clean timestamp/IDR alignment.
	// Copy mode causes ~1.2s A/V offset reject by Safari (MediaError 4).
	// Guardrails: Concurrency restricted by semaphore in scheduleRecordingBuild.
	if err := s.prepareRecordingCacheDir(cacheDir); err != nil {
		return err
	}

	err := s.runRecordingBuild(ctx, cacheDir, sourceType, source, false, probeSize, analyzeDur)
	if err == nil {
		if RecordingPlaylistReady(cacheDir) {
			return nil
		}
		// Fallthrough only if not ready and no error (incomplete copy?)
		log.L().Warn().Str("source", source).Msg("recording build incomplete, trying transcode fallback")
	} else {
		// HARDENED RETRY LOGIC
		if errors.Is(err, ErrProbeFailed) {
			log.L().Warn().Err(err).Str("source", source).Msg("fast probe failed (retryable), switching to robust params")
			// Proceed to Attempt 2
		} else {
			// Fail Fast for Auth, 404, or IO errors
			return err
		}
	}

	// Attempt 2: Robust Probe + Transcode (Fallback)
	// Note: We also switch to transcode=true here as fallback, assuming copy might have failed due to codecs.
	// But Phase 6 logic was: Try Copy -> if nil but not ready -> Try Transcode.
	// The Adaptive Probe logic adds another dimension.

	// Simplified Strategy:
	// 1. Try Copy with Fast Probe.
	// 2. If fails, Try Transcode with Robust Probe.

	if err := s.prepareRecordingCacheDir(cacheDir); err != nil {
		return err
	}

	// Robust Params
	robustProbe := "200M"
	robustAnalyze := "200M"

	if err := s.runRecordingBuild(ctx, cacheDir, sourceType, source, true, robustProbe, robustAnalyze); err != nil {
		return err
	}
	if !RecordingPlaylistReady(cacheDir) {
		return fmt.Errorf("recording playlist incomplete after transcode")
	}
	return nil
}

func (s *Server) prepareRecordingCacheDir(cacheDir string) error {
	if err := os.RemoveAll(cacheDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	// #nosec G301 -- cache dir serves playback assets.
	return os.MkdirAll(cacheDir, 0755)
}

// VODCacheVersion identifies the current generation of VOD transcoding logic.
// Increment this when changing ffmpeg flags (e.g. +cgop) to invalidate old caches.
const VODCacheVersion = 8

func (s *Server) runRecordingBuild(ctx context.Context, cacheDir, sourceType, source string, transcode bool, probeSize, analyzeDur string) error {
	// 0. Preflight Check (Fail Fast)
	pf := s.preflightCheck
	if pf == nil {
		// Fallback to default if not set
		pf = CheckSourceAvailability
	}
	if err := pf(ctx, source); err != nil {
		return err
	}

	// Paths
	livePlaylist := filepath.Join(cacheDir, "index.live.m3u8")
	finalPlaylist := filepath.Join(cacheDir, "index.m3u8")
	tmpFinalPlaylist := filepath.Join(cacheDir, "index.final.tmp")

	// Clean slate for this run
	_ = os.Remove(livePlaylist)
	_ = os.Remove(tmpFinalPlaylist)
	// We do NOT remove finalPlaylist here to support idempotency if another process finished it,
	// checking it is done in buildRecordingPlaylist level or readiness.
	// But since this is a *new* build attempt (previous failed/stale), we should probably ensure we don't conflict.
	// However, atomic rename overwrites.

	input := source
	useConcat := false
	if sourceType == "local" {
		parts, err := RecordingParts(source)
		if err != nil {
			return err
		}
		if len(parts) != 1 || parts[0] != source {
			concatFile := filepath.Join(cacheDir, "concat.txt")
			if err := WriteConcatList(concatFile, parts); err != nil {
				return err
			}
			input = concatFile
			useConcat = true
		}
	}
	// AV1 + fMP4 Strategy (User Request)
	// Switch to .m4s and AV1 codec.
	segmentPattern := ffmpegexec.SegmentPattern(cacheDir, ".m4s")
	args := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-ignore_unknown", // Ignore unknown stream types (data, etc.)
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",
		"-probesize", probeSize,
		"-analyzeduration", analyzeDur,
		"-avoid_negative_ts", "make_zero", // Essential for fMP4
		// Hardware Init for VAAPI
		"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
		"-filter_hw_device", "va",
	}
	if useConcat {
		args = append(args, "-f", "concat", "-safe", "0")
	}
	args = append(args,
		"-i", input,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-sn", // Drop subtitles
		"-dn", // Drop data streams
	)
	// 1. Reset to 0 (PTS-STARTPTS).
	// 2. Advance Audio by 2.0s (PTS-2.0/TB).
	audioArgs := []string{
		"-filter:a", "asetpts=PTS-STARTPTS,asetpts=PTS-2.000/TB,aresample=async=1:min_comp=0.01:comp_duration=1.0,aformat=channel_layouts=stereo",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ar", "48000",
		"-profile:a", "aac_low",
	}

	if transcode {
		args = append(args,
			// Video Transcode (AV1 VAAPI + Reset):
			// format=nv12 -> hwupload -> deinterlace -> reset timestamp
			"-filter:v", "format=nv12,hwupload,deinterlace_vaapi,setpts=PTS-STARTPTS",
			"-c:v", "av1_vaapi",
			"-qp", "35", // Quality Control
		)
		args = append(args, audioArgs...)
	} else {
		// Fallback for copy
		args = append(args, "-c:v", "copy")
		args = append(args, audioArgs...)
	}

	// Progressive VOD Flags (Phase 6)
	hlsFlags := "append_list+temp_file" // Crucial for progressive update without overwriting invalid list
	if transcode {
		hlsFlags = "independent_segments+append_list+temp_file"
	}

	// Dynamic segment pattern Override REMOVED (Set initially to .m4s)

	args = append(args,
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-f", "hls",
		"-hls_segment_type", "fmp4", // Explicit fMP4 type
		"-hls_time", "6",
		"-hls_list_size", "0",
		// NO -hls_playlist_type vod during build -> allows progressive updates
		"-hls_flags", hlsFlags,
	)

	if transcode {
		args = append(args, "-hls_segment_type", "fmp4")
	}

	args = append(args,
		"-hls_segment_filename", segmentPattern,
		livePlaylist, // Write to index.live.m3u8
	)

	s.mu.RLock()
	ffmpegBin := s.cfg.FFmpeg.Bin
	s.mu.RUnlock()
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	// P10: Progress-Based Monitoring
	// We use pipe:1 for machine-readable progress
	args = append(args, "-progress", "pipe:1")

	cmd := exec.CommandContext(ctx, ffmpegBin, args...) // #nosec G204

	// Capture stderr for classification
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// Capture stdout for progress parser
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Define attemptMode for metrics
	attemptMode := "fast"
	if probeSize == "200M" {
		attemptMode = "robust"
	}

	// P8.2: Process Enforcement (Set proc after start)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cmd start: %w", err)
	}

	// Start Parser
	progressCh := make(chan FFmpegProgress, 100)
	go func() {
		defer close(progressCh)
		ParseFFmpegProgress(stdoutPipe, progressCh)
	}()

	// Update state with process pointer
	// (Process state tracking removed Phase B)

	// P8.2: Clear proc on exit
	// (Process clear removed Phase B)

	// Supervisor Loop
	done := make(chan error, 1)
	start := time.Now()

	go func() {
		done <- cmd.Wait()
	}()

	// Progress state for watchdog
	var lastProgress FFmpegProgress
	lastProgressAt := time.Now()
	setupLatencyObserved := false
	metricsTicker := time.NewTicker(5 * time.Second) // Observability ticker
	defer metricsTicker.Stop()

	// Stall Timeout (User Requirement: Short timeout once reliable)
	// We use a safe default of 90s for now as requested range 30-90s
	stallTimeout := 90 * time.Second
	// Allow grace period for startup
	startupGrace := 20 * time.Second

	// Generate Build ID
	buildID := fmt.Sprintf("bld-%d", time.Now().UnixNano())

	log.L().Info().
		Str("pipeline", "vod_build").
		Bool("transcode", transcode).
		Str("video_input", input).
		Str("build_id", buildID).
		Str("stall_timeout", stallTimeout.String()).
		Msg("starting ffmpeg with progress monitoring")

	for {
		select {
		case err := <-done:
			// Process exited
			dur := time.Since(start).Seconds()

			if err != nil {
				// P8.1: Classifier Wiring
				// Compute segments written for context (still useful for triage)
				segCount := 0
				if count, _, e := GetSegmentStats(cacheDir); e == nil {
					segCount = count
				}

				stderr := stderrBuf.String()
				stderrLen := len(stderr)
				if stderrLen > 8192 {
					stderr = stderr[:8192] + "...(truncated)"
				}

				cls := ClassifyFFmpegError(stderrBuf.String(), segCount)

				log.L().Error().
					Str("stderr", stderr).
					Int("segments_written", segCount).
					Float64("duration_s", dur).
					Str("cache_dir", cacheDir).
					Int64("last_out_time", lastProgress.OutTimeUs).
					Err(err).
					Msg("ffmpeg exited with error")

				metrics.ObserveVODBuildDuration("failed", dur)

				// Report Failure to Breaker (P10)
				// s.recordingMu.Lock()
				// s.recordingMu.Unlock()

				return fmt.Errorf("%w: ffmpeg failed: %v", cls, err)
			}
			// Success: Finalize
			metrics.ObserveVODBuildDuration("success", dur)
			log.L().Info().
				Str("cache_dir", cacheDir).
				Float64("duration_s", dur).
				Msg("ffmpeg build finished successfully")

			// Report Success (P10)
			// (Breaker success removed Phase B)

			return s.finalizeRecordingPlaylist(cacheDir, livePlaylist, finalPlaylist)

		case <-ctx.Done():
			// Context canceled
			dur := time.Since(start).Seconds()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				metrics.ObserveVODBuildDuration("stale", dur)
			} else {
				metrics.ObserveVODBuildDuration("canceled", dur)
			}

			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return ctx.Err()

		case p, ok := <-progressCh:
			if !ok {
				// Channel closed, wait for done
				continue
			}

			// Check for forward progress
			if p.HasAdvanced(lastProgress) {
				lastProgress = p
				lastProgressAt = time.Now()

				// Update internal state for UI
				// (Progress state logic removed Phase B - TODO: Restore via manager)
			} else {
				// Received update but no advancement (paused/stuck/buffering)
				// We do NOT update lastProgressAt here, so timeout countdown continues.
			}

		case <-metricsTicker.C:
			// 1. Check Liveness / Stall
			sinceLast := time.Since(lastProgressAt)
			// Apply grace period at start
			if time.Since(start) < startupGrace {
				// do nothing
			} else if sinceLast > stallTimeout {
				// STALL DETECTED
				dur := time.Since(start).Seconds()
				metrics.ObserveVODBuildDuration("stale", dur)

				// Enrich log with details requested
				log.L().Error().
					Str("cache_dir", cacheDir).
					Dur("stall_duration", sinceLast).
					Dur("timeout", stallTimeout).
					Int64("last_out_time_us", lastProgress.OutTimeUs).
					Int64("last_total_size", lastProgress.TotalSize).
					Str("last_speed", lastProgress.Speed).
					Msg("VOD Supervisor: Killing stalled ffmpeg process (no progress updates)")

				// Report Stall Failure
				// s.recordingMu.Lock()
				// s.recordingMu.Unlock()

				_ = cmd.Process.Kill()
				return fmt.Errorf("%w: no progress for %v", ErrFFmpegStalled, stallTimeout)
			}

			// 2. Observability (Pulse)
			// Check file system stats for Readiness only (not liveness)
			segCount, _, _ := GetSegmentStats(cacheDir)

			// Setup Latency Metric (once)
			// We define "live ready" when playlist exists and we have segments.
			// This is distinct from ffmpeg liveness.
			if _, ok := RecordingLivePlaylistReady(cacheDir); ok && !setupLatencyObserved {
				dur := time.Since(start).Seconds()
				metrics.ObserveVODSetupLatency("live_ready", attemptMode, dur)
				setupLatencyObserved = true
			}

			log.L().Info().
				Int64("out_time_us", lastProgress.OutTimeUs).
				Str("speed", lastProgress.Speed).
				Int("seg_count", segCount).
				Dur("since_progress", sinceLast).
				Msg("VOD Supervisor Tick")
		}
	}
}

// Helper to get verify progress stats
func GetSegmentStats(dir string) (int, time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, time.Time{}, err
	}
	count := 0
	var maxMtime time.Time
	for _, e := range entries {
		name := e.Name()
		// Use canonical segment validation to ensure consistent policy
		if IsAllowedVideoSegment(name) {
			count++
			if info, err := e.Info(); err == nil {
				if info.ModTime().After(maxMtime) {
					maxMtime = info.ModTime()
				}
			}
		}
	}
	return count, maxMtime, nil
}

// Finalize: Atomic read-modify-write-rename
func (s *Server) finalizeRecordingPlaylist(cacheDir, livePath, finalPath string) error {
	// 1. Check if source exists
	data, err := os.ReadFile(livePath)
	if err != nil {
		return fmt.Errorf("read live playlist: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	var newLines []string
	hasVod := false

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "#EXT-X-PLAYLIST-TYPE") {
			continue // remove existing type if any
		}
		if l == "#EXT-X-ENDLIST" {
			continue // skip, we append at end
		}
		newLines = append(newLines, l)
		if l == "#EXTM3U" && !hasVod {
			newLines = append(newLines, "#EXT-X-PLAYLIST-TYPE:VOD")
			hasVod = true
		}
	}
	// Append Endlist
	newLines = append(newLines, "#EXT-X-ENDLIST")

	// Write to temp final
	tmpFinal := filepath.Join(cacheDir, "index.final.tmp")
	if err := os.WriteFile(tmpFinal, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return fmt.Errorf("write final tmp: %w", err)
	}

	// Atomic Rename
	if err := os.Rename(tmpFinal, finalPath); err != nil {
		return fmt.Errorf("rename final: %w", err)
	}

	// Cleanup live
	_ = os.Remove(livePath)

	log.L().Info().Str("path", finalPath).Msg("Recording build finalized")
	return nil
}

func RecordingCacheDir(hlsRoot, serviceRef string) (string, error) {
	if strings.TrimSpace(hlsRoot) == "" {
		return "", fmt.Errorf("hls root not configured")
	}
	return filepath.Join(hlsRoot, "recordings", RecordingCacheKey(serviceRef)), nil
}

func RecordingCacheKey(serviceRef string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(serviceRef)))
	return hex.EncodeToString(sum[:])
}

func ValidateRecordingRef(serviceRef string) error {
	// Security: Ensure input is valid UTF-8 before processing
	if !utf8.ValidString(serviceRef) {
		return errRecordingInvalid
	}

	// Security: Reject control chars, \ and ?#
	// Checked BEFORE TrimSpace to reject hidden formatting/control chars.
	// We specifically allow spaces (0x20) as they are common in filenames.
	for _, r := range serviceRef {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' {
			return errRecordingInvalid
		}
	}

	trimmedRef := strings.TrimSpace(serviceRef)
	if trimmedRef == "" {
		return errRecordingInvalid
	}

	receiverPath := ExtractPathFromServiceRef(trimmedRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return errRecordingInvalid
	}
	cleanRef := strings.TrimLeft(receiverPath, "/")
	cleanRef = path.Clean("/" + cleanRef)
	cleanRef = strings.TrimPrefix(cleanRef, "/")
	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		return errRecordingInvalid
	}
	// Strict check: Reject any ".." usage even if it effectively stays inside root
	// Check for traversal in the raw strings
	if strings.Contains(receiverPath, "/../") || strings.HasSuffix(receiverPath, "/..") {
		return errRecordingInvalid
	}

	// Check for traversal in decoded path (catch %2e%2e)
	if decoded, err := url.PathUnescape(receiverPath); err == nil {
		if decoded != receiverPath {
			if strings.Contains(decoded, "/../") || strings.HasSuffix(decoded, "/..") {
				return errRecordingInvalid
			}
		}
	}

	return nil
}

func RecordingPlaylistReady(cacheDir string) bool {
	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	info, err := os.Stat(playlistPath)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		return false
	}
	playlist := string(data)
	if !strings.Contains(playlist, "#EXTM3U") {
		return false
	}
	if !strings.Contains(playlist, "#EXT-X-PLAYLIST-TYPE:VOD") {
		return false
	}
	if !strings.Contains(playlist, "#EXT-X-ENDLIST") {
		return false
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if IsAllowedVideoSegment(entry.Name()) {
			return true
		}
	}
	return false
}

// RecordingParts returns the sequence of files for a multi-part recording.
func RecordingParts(basePath string) ([]string, error) {
	basePath = filepath.Clean(basePath)
	baseInfo, err := os.Stat(basePath)
	baseExists := err == nil && baseInfo.Mode().IsRegular()

	dir := filepath.Dir(basePath)
	baseName := filepath.Base(basePath)
	baseExt := filepath.Ext(baseName)
	baseStem := strings.TrimSuffix(baseName, baseExt)
	entries, readErr := os.ReadDir(dir)
	if readErr != nil && !baseExists {
		if os.IsNotExist(readErr) {
			return nil, ErrRecordingNotFound
		}
		return nil, readErr
	}

	type part struct {
		index int
		path  string
	}
	partsByIndex := make(map[int]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if baseExt != "" && strings.HasPrefix(name, baseStem+"_") && strings.HasSuffix(name, baseExt) {
			suffix := strings.TrimSuffix(strings.TrimPrefix(name, baseStem+"_"), baseExt)
			if AllDigits(suffix) {
				if idx, err := strconv.Atoi(suffix); err == nil {
					if _, exists := partsByIndex[idx]; !exists {
						partsByIndex[idx] = filepath.Join(dir, name)
					}
				}
			}
			continue
		}

		if strings.HasPrefix(name, baseName+".") {
			suffix := strings.TrimPrefix(name, baseName+".")
			if AllDigits(suffix) {
				if idx, err := strconv.Atoi(suffix); err == nil {
					if _, exists := partsByIndex[idx]; !exists {
						partsByIndex[idx] = filepath.Join(dir, name)
					}
				}
			}
		}
	}

	parts := make([]part, 0, len(partsByIndex))
	for idx, p := range partsByIndex {
		parts = append(parts, part{index: idx, path: p})
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].index < parts[j].index
	})

	if baseExists || len(parts) > 0 {
		out := make([]string, 0, len(parts)+1)
		if baseExists {
			out = append(out, basePath)
		}
		for _, p := range parts {
			out = append(out, p.path)
		}
		return out, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return nil, ErrRecordingNotFound
}

func AllDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func WriteConcatList(path string, parts []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	for _, part := range parts {
		line := "file " + ConcatEscape(part) + "\n"
		if _, err := io.WriteString(f, line); err != nil {
			return err
		}
	}
	return f.Sync()
}

func ConcatEscape(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\\' || r == '\'' || r == ' ' || r == '#' || r == '\t' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func InsertArgsBefore(args []string, needle string, insert []string) []string {
	for i, arg := range args {
		if arg == needle {
			out := make([]string, 0, len(args)+len(insert))
			out = append(out, args[:i]...)
			out = append(out, insert...)
			out = append(out, args[i:]...)
			return out
		}
	}
	return append(insert, args...)
}

func (s *Server) writeRecordingPlaybackError(w http.ResponseWriter, r *http.Request, serviceRef string, err error) {
	state := "IDLE"
	switch {
	case errors.Is(err, errRecordingInvalid):
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid recording id")
		return
	case errors.Is(err, ErrRecordingNotFound):
		RespondError(w, r, http.StatusNotFound, ErrRecordingNotFound, nil)
		return
	case errors.Is(err, errTooManyBuilds):
		// 429 Too Many Requests
		w.Header().Set("Retry-After", "30")
		RespondError(w, r, http.StatusTooManyRequests, ErrConcurrentBuildsExceeded, map[string]any{
			"state":          "REJECTED",
			"max_concurrent": s.cfg.VODMaxConcurrent,
		})
		return
	case errors.Is(err, errRecordingNotReady):
		w.Header().Set("Retry-After", strconv.Itoa(recordingRetryAfterSeconds))
		state = "PENDING"
		// If serviceRef is valid, try to check status
		if serviceRef != "" {
			s.mu.RLock()
			hlsRoot := s.cfg.HLS.Root
			s.mu.RUnlock()
			if cacheDir, cacheErr := RecordingCacheDir(hlsRoot, serviceRef); cacheErr == nil {
				// Phase B: Use vodManager
				if run := s.vodManager.Get(cacheDir); run != nil {
					state = "RUNNING"
					select {
					case <-run.Done:
						if run.Err != nil {
							state = "FAILED"
						} else {
							state = "READY"
						}
					default:
					}
				}
			}
		}
		RespondError(w, r, http.StatusServiceUnavailable, ErrRecordingNotReady, map[string]any{
			"state":             state,
			"retryAfterSeconds": recordingRetryAfterSeconds,
		})
		return
	}
	// Default
	log.L().Error().Err(err).Msg("recording playback error")
	RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "internal error")
}

// CleanupStaleArtifacts removes .tmp files older than 1 hour and all .lock files on startup.
func (s *Server) CleanupStaleArtifacts() {
	s.mu.RLock()
	dataDir := s.cfg.DataDir
	s.mu.RUnlock()

	vodDir := filepath.Join(dataDir, "vod-cache")
	files, err := os.ReadDir(vodDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.L().Warn().Err(err).Msg("failed to read vod-cache for startup cleanup")
		}
		return
	}

	count := 0
	for _, f := range files {
		name := f.Name()
		path := filepath.Join(vodDir, name)

		// 1. Lock Cleanup (Always clean locks on startup as we are not running jobs)
		if strings.HasSuffix(name, ".lock") {
			if err := os.Remove(path); err == nil {
				count++
			}
			continue
		}

		// 2. Tmp File Cleanup (> 1h)
		if strings.HasSuffix(name, ".tmp") {
			info, err := f.Info()
			if err == nil && time.Since(info.ModTime()) > 1*time.Hour {
				if err := os.Remove(path); err == nil {
					count++
				}
			}
		}
	}
	if count > 0 {
		log.L().Info().Int("count", count).Msg("startup: cleaned stale vod artifacts")
	}
}

// StartRecordingCacheEvicter runs a background worker to clean up old recording caches.
// Phase 8: Resource Management (LRU Eviction)
func (s *Server) StartRecordingCacheEvicter(ctx context.Context) {
	// P10: Startup Cleanup (One-off)
	s.CleanupStaleArtifacts()

	ttl := s.cfg.VODCacheTTL

	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.L().Info().Dur("ttl", ttl).Msg("starting recording cache evicter")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Run stale build cleanup periodically (to kill hung processes even if no new requests come in)
			// Phase B: VOD Manager handles its own cleanup via context/cancel.
			// Legacy cleanupRecordingBuilds removed.

			// Run file eviction
			s.evictRecordingCaches(ttl)
		}
	}
}

func (s *Server) evictRecordingCaches(ttl time.Duration) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	dataDir := s.cfg.DataDir
	s.mu.RUnlock()

	// Item struct for sorting
	type cacheItem struct {
		path    string
		isDir   bool
		modTime time.Time
		size    int64
	}
	var items []cacheItem

	// 1. Scan HLS Recordings (Directories)
	recordingsDir := filepath.Join(hlsRoot, "recordings")
	if entries, err := os.ReadDir(recordingsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				if info, err := e.Info(); err == nil {
					items = append(items, cacheItem{
						path:    filepath.Join(recordingsDir, e.Name()),
						isDir:   true,
						modTime: info.ModTime(),
						size:    0, // Calculate if needed, but for now treat directories as chunks
					})
				}
			}
		}
	}

	// 2. Scan MP4 VOD Cache (Files)
	mp4Dir := filepath.Join(dataDir, "vod-cache")
	if entries, err := os.ReadDir(mp4Dir); err == nil {
		for _, e := range entries {
			// Skip locks and temp files
			if strings.HasSuffix(e.Name(), ".lock") || strings.HasSuffix(e.Name(), ".tmp") {
				continue
			}
			if !e.IsDir() {
				if info, err := e.Info(); err == nil {
					items = append(items, cacheItem{
						path:    filepath.Join(mp4Dir, e.Name()),
						isDir:   false,
						modTime: info.ModTime(),
						size:    info.Size(),
					})
				}
			}
		}
	}

	// Sort by Age (Oldest first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.Before(items[j].modTime)
	})

	now := time.Now()
	evictedCount := 0

	// Safety: Active Builds Check
	// Safety: Active Builds Check
	isActive := func(p string) bool {
		// Phase B: Use vodManager
		if run := s.vodManager.Get(p); run != nil {
			// Check if done?
			select {
			case <-run.Done:
				return false // Finished, can be evicted (if old)
			default:
				return true // Running, do not evict
			}
		}

		// Legacy Lock check? (Removed Phase A)
		if strings.HasSuffix(p, ".mp4") {
			if _, err := os.Stat(p + ".lock"); err == nil {
				return true
			}
		}
		return false
	}

	// 3. TTL Eviction
	remainingItems := make([]cacheItem, 0, len(items))
	for _, item := range items {
		if isActive(item.path) {
			remainingItems = append(remainingItems, item)
			continue
		}

		age := now.Sub(item.modTime)
		if age > ttl {
			log.L().Info().Str("path", item.path).Dur("age", age).Msg("evicting stale cache item")
			if err := os.RemoveAll(item.path); err == nil {
				evictedCount++
				metrics.IncVODCacheEvicted()
			}
		} else {
			remainingItems = append(remainingItems, item)
		}
	}
	items = remainingItems

	// 4. Disk Pressure Eviction
	// Threshold: 5GB or 10%? Let's say 5GB for now as absolute safety.
	const MinFreeSpace = 5 * 1024 * 1024 * 1024 // 5GB

	var stat syscall.Statfs_t
	// Check space of DataDir
	if err := syscall.Statfs(dataDir, &stat); err == nil {
		// Available blocks * size per block
		freeSpace := int64(stat.Bavail) * int64(stat.Bsize)

		if freeSpace < MinFreeSpace {
			log.L().Warn().Int64("free_bytes", freeSpace).Msg("disk pressure detected, triggering aggressive eviction")

			// Evict oldest remaining non-active items until we free enough space?
			// Let's loop until we free up to Target (e.g. MinFree + 10GB buffer)
			// For simplicity: Try to free 10% of threshold (500MB) or just enough to get back to green?
			// Let's ensure we free at least 1GB if under pressure.
			targetFree := int64(1 * 1024 * 1024 * 1024)

			bytesFreed := int64(0)
			for _, item := range items {
				if bytesFreed >= targetFree {
					break
				}
				if isActive(item.path) {
					continue
				}

				if err := os.RemoveAll(item.path); err == nil {
					log.L().Info().Str("path", item.path).Msg("evicting cache item due to disk pressure")
					evictedCount++
					bytesFreed += item.size // Approximate for dirs
					if item.size == 0 && item.isDir {
						bytesFreed += 100 * 1024 * 1024 // Estimate 100MB for HLS dir?
					}
					metrics.IncVODCacheEvicted()
				}
			}
		}
	}

	if evictedCount > 0 {
		log.L().Info().Int("count", evictedCount).Msg("cache eviction complete")
	}
}

func EncodeRecordingID(serviceRef string) string {
	if strings.TrimSpace(serviceRef) == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(serviceRef))
}

func ValidRecordingID(id string) bool {
	if len(id) < recordingIDMinLen || len(id) > recordingIDMaxLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// DecodeRecordingID helper (factored out)
func (s *Server) DecodeRecordingID(id string) string {
	id = strings.TrimSpace(id)
	if !ValidRecordingID(id) {
		return ""
	}
	if decodedBytes, err := base64.RawURLEncoding.DecodeString(id); err == nil {
		if len(decodedBytes) == 0 {
			return ""
		}
		if !utf8.Valid(decodedBytes) {
			return ""
		}
		decoded := string(decodedBytes)
		if strings.TrimSpace(decoded) == "" {
			return ""
		}
		if strings.ContainsRune(decoded, '\x00') {
			return ""
		}
		// Strictly validate the decoded reference immediately
		if err := ValidateRecordingRef(decoded); err != nil {
			return ""
		}
		return decoded
	}
	return ""
}

func ParseRecordingDurationSeconds(length string) (int64, bool) {
	length = strings.TrimSpace(length)
	if length == "" || length == "0" {
		return 0, false
	}

	if strings.Contains(length, ":") {
		parts := strings.Split(length, ":")
		if len(parts) == 3 {
			hours, err1 := strconv.Atoi(parts[0])
			minutes, err2 := strconv.Atoi(parts[1])
			seconds, err3 := strconv.Atoi(parts[2])
			if err1 != nil || err2 != nil || err3 != nil {
				return 0, false
			}
			total := (hours * 3600) + (minutes * 60) + seconds
			if total <= 0 {
				return 0, false
			}
			return int64(total), true
		}
		if len(parts) == 2 {
			minutes, err1 := strconv.Atoi(parts[0])
			seconds, err2 := strconv.Atoi(parts[2])
			if err1 != nil || err2 != nil {
				return 0, false
			}
			total := (minutes * 60) + seconds
			if total <= 0 {
				return 0, false
			}
			return int64(total), true
		}
		return 0, false
	}

	fields := strings.Fields(length)
	if len(fields) == 0 {
		return 0, false
	}
	minStr := strings.TrimSpace(fields[0])
	minStr = strings.TrimSuffix(minStr, "min")
	minStr = strings.TrimSuffix(minStr, "mins")
	minStr = strings.TrimSuffix(minStr, "m")
	minutes, err := strconv.Atoi(minStr)
	if err != nil || minutes <= 0 {
		return 0, false
	}
	return int64(minutes * 60), true
}

// DeleteRecording handles DELETE /api/v3/recordings/{recordingId}
// Deletes the recording via OpenWebIF on the receiver.
func (s *Server) DeleteRecording(w http.ResponseWriter, r *http.Request, recordingId string) {
	if strings.TrimSpace(recordingId) == "" {
		http.Error(w, "Missing recording ID", http.StatusBadRequest)
		return
	}

	// Decode ID
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" || ValidateRecordingRef(serviceRef) != nil {
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	// Use OpenWebIF Client to delete the recording on the receiver
	// This works for HDD and NAS locations without needing local mounts.
	client := s.newOpenWebIFClient(cfg, snap)

	log.L().Info().Str("sref", serviceRef).Msg("requesting recording deletion via OpenWebIF")

	if err := client.DeleteMovie(r.Context(), serviceRef); err != nil {
		log.L().Error().Err(err).Str("sref", serviceRef).Msg("failed to delete recording")
		// Map generic error to 500. We could try to parse "not found" but OWI usually returns generic "false"
		http.Error(w, fmt.Sprintf("Failed to delete recording: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SanitizeRecordingRelPath implementation for POSIX paths
// Returns the cleaned relative path and whether it was blocked (traversal detected).
func SanitizeRecordingRelPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	// Security: Reject control chars, \, ?, #, and unicode Cf
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' {
			return "", true
		}
	}

	// Treat as relative: strip leading slashes
	p = strings.TrimLeft(p, "/")

	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", true
	}
	if clean == "." {
		return "", false // Root
	}

	return clean, false
}

// IsAllowedVideoSegment provides a single canonical check for segment serving.
// VOD Recording uses TS-HLS only for maximum compatibility.
// STRICT: Only allow files starting with "seg_" and ending with .ts extension.
func IsAllowedVideoSegment(path string) bool {
	name := filepath.Base(path)
	// Allow init.mp4 for fMP4
	if name == "init.mp4" {
		return true
	}
	// Enforce prefix to prevent arbitrary file exposure
	if !strings.HasPrefix(name, "seg_") {
		return false
	}

	ext := strings.ToLower(filepath.Ext(name))
	// VOD Recording outputs TS or fMP4 segments
	return ext == ".ts" || ext == ".m4s"
}

// ProbeDuration uses ffprobe to get the exact duration of a media file.
// Returns duration in time.Duration.
func ProbeDuration(ctx context.Context, path string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second) // Fast timeout for list view
	defer cancel()

	// ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 <file>
	c := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	out, err := c.Output()
	if err != nil {
		return 0, err
	}

	val := strings.TrimSpace(string(out))
	if val == "" || val == "N/A" {
		return 0, fmt.Errorf("no duration found")
	}

	secs, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, err
	}

	return time.Duration(secs * float64(time.Second)), nil
}

// escapeServiceRefPath percent-encodes a string for use in a URL path,
// but specifically preserves ':' and '/' as required by Enigma2 ServiceRefs.
// It escapes all other non-unreserved characters (including UTF-8 bytes).
func EscapeServiceRefPath(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if ShouldEscapeRefChar(c) {
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&15])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func ShouldEscapeRefChar(c byte) bool {
	// RFC 3986 Unreserved characters: ALPHA, DIGIT, "-", ".", "_", "~"
	// Plus we specifically want to KEEP ":" and "/" for Enigma2 service refs.
	if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' {
		return false
	}
	switch c {
	case '-', '.', '_', '~', ':', '/':
		return false
	}
	return true
}

// P10: Circuit Breaker Key Derivation
func (s *Server) getRecordingRootKey(serviceRef string) string {
	pathPart := ExtractPathFromServiceRef(serviceRef)
	s.mu.RLock()
	defer s.mu.RUnlock()

	for id, rootPath := range s.cfg.RecordingRoots {
		if strings.HasPrefix(pathPart, rootPath) {
			return id
		}
	}
	return "hdd"
}

func (s *Server) endRecordingBuildOps() {
	// No-op
}
