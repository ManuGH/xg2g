// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/resolver"
	"github.com/ManuGH/xg2g/internal/control/http/v3/types"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"

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
	errRecordingInvalid  = v3recordings.ErrInvalidRecordingRef
	errRecordingNotReady = errors.New("recording not ready")
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
		// #nosec G304
		_ = resp.Body.Close()

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
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)

	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
		return fmt.Errorf("%w: HTTP %d", ErrSourceUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("source error: HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetRecordingsRecordingIdStatus handles GET /api/v3/recordings/{recordingId}/status.
// This endpoint is metadata-only and performs no synchronous filesystem checks.
// Path computations (cacheDir) are used only for in-memory job lookups.
func (s *Server) GetRecordingsRecordingIdStatus(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		s.writeRecordingPlaybackError(w, r, "", errRecordingInvalid)
		return
	}

	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	cacheDir, err := v3recordings.RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	// 1. Check Active Build (Phase B: vodManager)
	job, jobOk := s.vodManager.Get(r.Context(), cacheDir)
	meta, metaOk := s.vodManager.GetMetadata(serviceRef)
	var metaPtr *vod.Metadata
	if metaOk {
		metaPtr = &meta
	}
	resp := mapRecordingStatus(job, jobOk, metaPtr, metaOk)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func mapRecordingStatus(job *vod.JobStatus, jobOk bool, meta *vod.Metadata, metaOk bool) RecordingBuildStatus {
	if jobOk {
		resp := RecordingBuildStatus{State: RecordingBuildStatusStateRUNNING}
		switch job.State {
		case vod.JobStateBuilding, vod.JobStateFinalizing:
			resp.State = RecordingBuildStatusStateRUNNING
		case vod.JobStateFailed:
			resp.State = RecordingBuildStatusStateFAILED
			if job.Reason != "" {
				msg := job.Reason
				resp.Error = &msg
			}
		case vod.JobStateSucceeded:
			resp.State = RecordingBuildStatusStateREADY
		default:
			resp.State = RecordingBuildStatusStateRUNNING
		}
		return resp
	}

	resp := RecordingBuildStatus{State: RecordingBuildStatusStateIDLE}
	if !metaOk || meta == nil {
		return resp
	}

	switch meta.State {
	case vod.ArtifactStateUnknown, vod.ArtifactStatePreparing:
		resp.State = RecordingBuildStatusStateRUNNING
	case vod.ArtifactStateReady:
		resp.State = RecordingBuildStatusStateREADY
	case vod.ArtifactStateFailed:
		resp.State = RecordingBuildStatusStateFAILED
		if meta.Error != "" {
			msg := meta.Error
			resp.Error = &msg
		}
	case vod.ArtifactStateMissing:
		resp.State = RecordingBuildStatusStateFAILED
		msg := "MISSING"
		resp.Error = &msg
	}

	return resp
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

	// Ensure standard HDD path is always available if not discovered
	// This fixes the case where only NFS bookmarks are returned but internal HDD is desired.
	const standardHddPath = "/media/hdd/movie"
	hddFound := false
	for _, p := range rootsRaw {
		if p == standardHddPath {
			hddFound = true
			break
		}
	}
	if !hddFound {
		rootsRaw["hdd"] = standardHddPath
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
			writeProblem(w, r, http.StatusInternalServerError, "recordings/no_roots", "Internal Server Error", "INTERNAL_ERROR", "No recording roots configured", nil)
			return
		}
	}

	// 3. Resolve & Validate Path
	// Security: Strict confinement using SanitizeRecordingRelPath
	rootAbs, ok := roots[qRootID]
	if !ok {
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid_root", "Invalid Root", "INVALID_ROOT", "The specified root ID is invalid", nil)
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
		writeProblem(w, r, http.StatusForbidden, "recordings/access_denied", "Access Denied", "FORBIDDEN", "Path traversal or security violation detected", nil)
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
	start := time.Now()
	list, err := client.GetRecordings(r.Context(), cleanTarget)
	owiMs := time.Since(start).Milliseconds()
	if err != nil {
		log.L().Error().Err(err).Str("event", "openwebif.recordings_failed").Msg("failed to fetch recordings from receiver")
		RespondError(w, r, http.StatusBadGateway, ErrUpstreamUnavailable)
		return
	}

	// Handle cases where Result is false (or missing)
	if !list.Result {
		if len(list.Movies) > 0 {
			// Found movies, so missing "result" field is fine. Set Result=true for consistency.
			// Log this occurrence as requested to track receiver behavior.
			log.L().Info().Str("event", "openwebif.result_override").Str("rel_path", cleanRel).Int("count", len(list.Movies)).Msg("overriding result=false because movies were found")
			list.Result = true
		} else {
			log.L().Warn().Str("event", "openwebif.result_false").Str("rel_path", cleanRel).Msg("receiver returned result=false (treating as empty dir)")
			// Ensure empty slices
			list.Movies = []openwebif.Movie{}
			list.Bookmarks = []openwebif.MovieLocation{}
		}
	}

	// 5. Build Response
	// Helper for pointers
	strPtr := func(s string) *string { return &s }
	int64Ptr := func(i int64) *int64 { return &i }

	metaLookupStart := time.Now()
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

		durationSeconds, durErr := ParseRecordingDurationSeconds(m.Length)
		durationKnown := (durErr == nil)

		// ENRICHMENT: Consult VOD Manager for cached metadata (Non-blocking)
		if !durationKnown && s.vodManager != nil {
			if meta, ok := s.vodManager.GetMetadata(m.ServiceRef); ok {
				if meta.Duration > 0 {
					durationSeconds = int64(meta.Duration)
					durationKnown = true
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
	metaMs := time.Since(metaLookupStart).Milliseconds()

	// Enrich with Resume Data
	if principal := auth.PrincipalFromContext(r.Context()); principal != nil {
		s.attachResumeSummaries(r.Context(), principal.ID, recordingsList)
	}

	// LOG: SLO Metrics
	log.L().Info().
		Int64("openwebif_ms", owiMs).
		Int64("meta_cache_ms", metaMs).
		Int("count", len(recordingsList)).
		Msg("GetRecordings handled")

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
	// 1. Adapter: Inputs
	intent := types.IntentMetadata
	clientProfile := s.mapProfile(r)
	profile := toPlaybackProfile(clientProfile)

	// 2. Adapter: Call Resolver
	// Enforce SLO for UI responsiveness
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Millisecond)
	defer cancel()

	res, prob := s.resolver.Resolve(ctx, recordingId, intent, profile)

	// 3. Adapter: Error Mapping
	if prob != nil {
		s.writeResolveError(w, r, prob)
		return
	}

	// 4. Adapter: Success Mapping
	streamURL, err := s.mapDecisionToURL(recordingId, res.Decision, res.MediaInfo)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, recordingId, err)
		return
	}

	mode, err := mapDecisionToPlaybackMode(res.Decision)
	if err != nil {
		s.writeRecordingPlaybackError(w, r, recordingId, err)
		return
	}

	resp := types.VODPlaybackResponse{
		Mode:            mode,
		URL:             streamURL,
		DurationSeconds: int64(res.MediaInfo.Duration),
		Reason:          res.Reason,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.L().Error().Err(err).Msg("failed to encode playback info")
	}
}

func (s *Server) writeResolveError(w http.ResponseWriter, r *http.Request, err *resolver.ResolveError) {
	status := http.StatusInternalServerError
	code := string(err.Code)

	switch err.Code {
	case resolver.CodeInvalid:
		status = http.StatusBadRequest
	case resolver.CodeNotFound:
		status = http.StatusNotFound
	case resolver.CodePreparing:
		status = http.StatusServiceUnavailable
		if err.RetryAfter > 0 {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(err.RetryAfter.Seconds())))
		}
	case resolver.CodeUpstream:
		status = http.StatusBadGateway
	case resolver.CodeFailed, resolver.CodeInternal:
		status = http.StatusInternalServerError
	}

	// Use lowercase code for problem type slug
	slug := strings.ToLower(code)
	writeProblem(w, r, status, "vod/"+slug, "VOD Error", code, err.Error(), nil)
}

func (s *Server) writePreparingResponse(w http.ResponseWriter, r *http.Request, recordingId, state string, retryAfter int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Preparing", "PREPARING", "Recording is being prepared for playback", map[string]interface{}{
		"recording_id": recordingId,
		"state":        state,
	})
}

func (s *Server) StreamRecordingDirect(w http.ResponseWriter, r *http.Request, recordingId string) {
	start := time.Now()
	serviceRef := s.DecodeRecordingID(recordingId)
	if serviceRef == "" {
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid_id", "Invalid ID", "INVALID_ID", "The provided recording ID is invalid", nil)
		return
	}

	// 1. Artifact State Machine Check (Non-blocking)
	meta, exists := s.vodManager.GetMetadata(serviceRef)
	lookupMs := time.Since(start).Milliseconds()

	if !exists || (meta.State != vod.ArtifactStateReady) {
		// If not ready, kick off async probe/build and return 503
		// We use "" as input path to let TriggerProbe handle resolution if it can,
		// or we can pass a hint if we have it. But Metadata cache should be enough.
		s.vodManager.TriggerProbe(serviceRef, "")

		state := "UNKNOWN"
		if exists {
			state = string(meta.State)
		}
		s.writePreparingResponse(w, r, recordingId, state, 5)

		log.L().Info().
			Int64("meta_cache_ms", lookupMs).
			Str("state", state).
			Str("recording_id", recordingId).
			Msg("StreamRecordingDirect deferred (preparing)")
		return
	}

	// 2. Use Authoritative Path from Metadata
	cachePath := meta.ArtifactPath
	if cachePath == "" {
		// Fallback for safety/races, or log error.
		// Ideally this should not happen if state is READY.
		// We can try to recompute or just fail.
		// For now, let's trigger a re-probe if path is missing but state says READY (inconsistent).
		s.vodManager.MarkUnknown(serviceRef)
		s.vodManager.TriggerProbe(serviceRef, "")
		s.writePreparingResponse(w, r, recordingId, "REPAIR", 5)
		return
	}

	// 3. Serve cached artifact
	// Since state is READY, the file MUST exist in cachePath (either as file or symlink).
	f, err := os.Open(cachePath)
	if err != nil {
		log.L().Warn().Err(err).Str("path", cachePath).Msg("failed to open cached vod file in READY state")

		// Atomic Demotion: Stop the retry loop immediately
		s.vodManager.DemoteOnOpenFailure(serviceRef, err)

		w.Header().Set("Retry-After", "5")
		// We return 503 so the client retries later, by which time the probe will have run
		writeProblem(w, r, http.StatusServiceUnavailable, "recordings/recovery", "Recovery", "PREPARING", "Artifact momentarily unavailable", nil)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		s.writeRecordingPlaybackError(w, r, serviceRef, err)
		return
	}

	log.L().Info().
		Int64("meta_cache_ms", lookupMs).
		Str("state", "READY").
		Str("recording_id", recordingId).
		Msg("StreamRecordingDirect serving artifact")

	http.ServeContent(w, r, "stream.mp4", info.ModTime(), f)
}

func (s *Server) resolveRecordingPlaybackSource(ctx context.Context, serviceRef string) (string, string, string, error) {
	if err := v3recordings.ValidateRecordingRef(serviceRef); err != nil {
		return "", "", "", err
	}
	serviceRef = strings.TrimSpace(serviceRef)
	receiverPath := recordings.ExtractPathFromServiceRef(serviceRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return "", "", "", errRecordingInvalid
	}

	s.mu.RLock()
	host := s.cfg.Enigma2.BaseURL
	streamPort := s.cfg.Enigma2.StreamPort
	policy := strings.ToLower(strings.TrimSpace(s.cfg.RecordingPlaybackPolicy))
	pathMapper := s.recordingPathMapper
	username := s.cfg.Enigma2.Username
	password := s.cfg.Enigma2.Password
	s.mu.RUnlock()

	allowLocal := policy != "receiver_only"
	allowReceiver := policy != "local_only"

	if allowLocal && pathMapper != nil {
		if localPath, ok := pathMapper.ResolveLocalUnsafe(receiverPath); ok {
			return "local", localPath, "", nil
		}
	}

	if !allowReceiver {
		return "", "", "", ErrRecordingNotFound
	}

	u := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host, streamPort),
		Path:   "/" + serviceRef, // Let url.URL encode this safely
	}
	// Verify if custom escaping is needed (e.g. for colons).
	// Legacy logic used manual escaping. If we trust url.URL, we just use Path.
	// If we need to preserve colons (Enigma2 sensitive?), we might need RawPath.
	// User advice: "Set only Path... and let url.URL escape it".
	// Enigma2 refs contain colons. url.URL normally escapes them in Path? No, usually not.
	// But it escapes spaces, etc.
	// Let's stick to simple Path for now as per recommendation unless legacy required manual escaping for specific chars.
	// Actually, Enigma2 requires colons to be unescaped. Path usually keeps them unescaped in Go.
	if username != "" && password != "" {
		u.User = url.UserPassword(username, password)
	}
	return "receiver", u.String(), "", nil
}

func (s *Server) ensureRecordingVODPlaylist(ctx context.Context, serviceRef string) (string, error) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	if err := v3recordings.ValidateRecordingRef(serviceRef); err != nil {
		return "", err
	}

	cacheDir, err := v3recordings.RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}

	if RecordingPlaylistReady(cacheDir) {
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return filepath.Join(cacheDir, "index.m3u8"), nil
	}

	if path, ok := RecordingLivePlaylistReady(cacheDir); ok {
		now := time.Now()
		_ = os.Chtimes(cacheDir, now, now)
		return path, nil
	}

	_, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
	if err != nil {
		return "", err
	}

	finalPath := filepath.Join(cacheDir, "index.m3u8")
	_, err = s.vodManager.EnsureSpec(ctx, cacheDir, serviceRef, source, cacheDir, "index.live.m3u8", finalPath, vod.ProfileDefault)
	if err != nil {
		return "", errRecordingNotReady
	}

	return "", errRecordingNotReady
}

func (s *Server) ensureRecordingTimeshiftPlaylist(ctx context.Context, serviceRef string) (string, error) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	if err := v3recordings.ValidateRecordingRef(serviceRef); err != nil {
		return "", err
	}

	cacheDir, err := v3recordings.RecordingCacheDir(hlsRoot, serviceRef)
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

	_, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
	if err != nil {
		return "", err
	}

	finalPath := filepath.Join(cacheDir, "timeshift.m3u8")
	_, err = s.vodManager.EnsureSpec(ctx, cacheDir, serviceRef, source, cacheDir, "index.live.m3u8", finalPath, vod.ProfileDefault)
	if err != nil {
		return "", errRecordingNotReady
	}

	return "", errRecordingNotReady
}

// scheduleRecordingVODPlaylist queues a background build without synchronous filesystem I/O.
func (s *Server) scheduleRecordingVODPlaylist(ctx context.Context, serviceRef string) error {
	s.mu.RLock()
	hlsRoot := s.cfg.HLS.Root
	s.mu.RUnlock()

	if err := v3recordings.ValidateRecordingRef(serviceRef); err != nil {
		return err
	}

	cacheDir, err := v3recordings.RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return err
	}

	kind, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
	if err != nil {
		return err
	}
	if kind == "local" {
		s.vodManager.SetResolvedPathIfEmpty(serviceRef, source)
	}

	finalPath := filepath.Join(cacheDir, "index.m3u8")
	_, err = s.vodManager.EnsureSpec(ctx, cacheDir, serviceRef, source, cacheDir, "index.live.m3u8", finalPath, vod.ProfileDefault)
	if err != nil {
		return errRecordingNotReady
	}

	return nil
}

// RecordingLivePlaylistReady checks if a valid progressive playlist exists.
// Criteria: index.live.m3u8 exists AND references at least one existing segment file.
func RecordingLivePlaylistReady(cacheDir string) (string, bool) {
	return v3recordings.RecordingLivePlaylistReady(cacheDir)
}

func RewritePlaylistType(content, playlistType string) string {
	return v3recordings.RewritePlaylistType(content, playlistType)
}

// VODCacheVersion identifies the current generation of VOD transcoding logic.
// Increment this when changing ffmpeg flags (e.g. +cgop) to invalidate old caches.
const VODCacheVersion = 8

func (s *Server) runRecordingBuild(ctx context.Context, cacheDir, sourceType, source string, transcode bool, probeSize, analyzeDur string) error {
	return errors.New("legacy build removed - use vod.Manager")
}
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
	// #nosec G304
	data, err := os.ReadFile(filepath.Clean(livePath))
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
	if err := os.WriteFile(tmpFinal, []byte(strings.Join(newLines, "\n")), 0600); err != nil {
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

func RecordingPlaylistReady(cacheDir string) bool {
	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	info, err := os.Stat(playlistPath)
	if err != nil || info.IsDir() {
		return false
	}
	// #nosec G304
	data, err := os.ReadFile(filepath.Clean(playlistPath))
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
	// #nosec G304
	f, err := os.Create(filepath.Clean(path))
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
	switch {
	case errors.Is(err, errRecordingInvalid):
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid recording id")
		return
	case errors.Is(err, ErrRecordingNotFound):
		RespondError(w, r, http.StatusNotFound, ErrRecordingNotFound, nil)
		return
	case errors.Is(err, errTooManyBuilds):
		// 429 Too Many Requests
		w.Header().Set("Retry-After", "10")
		RespondError(w, r, http.StatusTooManyRequests, ErrConcurrentBuildsExceeded, map[string]any{
			"state":          "REJECTED",
			"max_concurrent": s.cfg.VODMaxConcurrent,
		})
		return
	case errors.Is(err, errRecordingNotReady):
		w.Header().Set("Retry-After", strconv.Itoa(recordingRetryAfterSeconds))
		state := "IDLE"
		if serviceRef != "" {
			s.mu.RLock()
			hlsRoot := s.cfg.HLS.Root
			s.mu.RUnlock()
			if cacheDir, cacheErr := v3recordings.RecordingCacheDir(hlsRoot, serviceRef); cacheErr == nil {
				// Phase B: Check Active Build
				if status, exists := s.vodManager.Get(r.Context(), cacheDir); exists {
					if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
						state = "RUNNING"
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
	// Safety window for active builds or recent touches (10 minutes)
	// This prevents evicting files that are being written to, even if the active build check is imperfect.
	const safetyWindow = 10 * time.Minute

	isActive := func(p string) bool {
		// active build check removal: we cannot reliably map file path to job ID here.
		// Robust fallback: Check Mtime. If modified recently, assume active/safe.
		if info, err := os.Stat(p); err == nil {
			if time.Since(info.ModTime()) < safetyWindow {
				return true
			}
		}

		// Legacy Lock check? (Removed Phase A, but kept here for safety if locks exist)
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
	const MinFreeSpace uint64 = 5 * 1024 * 1024 * 1024 // 5GB

	var stat syscall.Statfs_t
	// Check space of DataDir
	if err := syscall.Statfs(dataDir, &stat); err == nil {
		// Available blocks * size per block
		// #nosec G115 -- block size is small and Bavail is within range for positive blocks
		freeSpace := stat.Bavail * uint64(stat.Bsize)

		if freeSpace < MinFreeSpace {
			log.L().Warn().Uint64("free_bytes", freeSpace).Msg("disk pressure detected, triggering aggressive eviction")

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
	return v3recordings.EncodeRecordingID(serviceRef)
}

func ValidRecordingID(id string) bool {
	return v3recordings.ValidRecordingID(id)
}

// DecodeRecordingID helper (factored out)
// DecodeRecordingID helper (delegated)
func (s *Server) DecodeRecordingID(id string) string {
	ref, ok := v3recordings.DecodeRecordingID(id)
	if !ok {
		return ""
	}
	return ref
}

// ParseRecordingDurationSeconds parses duration strings from the receiver.
//
// Invariants:
// 1. Strictly non-panicking (uses defensive checks/strconv).
// 2. Returns error for invalid format, overflow, or negative values.
// 3. Supported formats: "HH:MM:SS", "MM:SS", "M min|mins|minutes|m".
// 4. Enforces range validation: MM and SS must be 0-59 in colon formats.
func ParseRecordingDurationSeconds(length string) (int64, error) {
	length = strings.TrimSpace(length)
	if length == "" || length == "0" {
		return 0, ErrDurationInvalid
	}

	// Case 1: HH:MM:SS or MM:SS
	if strings.Contains(length, ":") {
		parts := strings.Split(length, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0, ErrDurationInvalid
		}

		cleanParts := make([]int64, len(parts))
		for i := range parts {
			s := strings.TrimSpace(parts[i])
			if s == "" {
				return 0, ErrDurationInvalid
			}
			val, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return 0, ErrDurationInvalid
			}
			if val < 0 {
				return 0, ErrDurationNegative
			}
			cleanParts[i] = val
		}

		// Enforce range validation:
		// - HH:MM:SS -> MM < 60, SS < 60
		// - MM:SS    -> SS < 60 (MM can be arbitrary)
		if len(parts) == 3 {
			if cleanParts[1] >= 60 || cleanParts[2] >= 60 {
				return 0, ErrDurationInvalid
			}
		} else { // len(parts) == 2
			if cleanParts[1] >= 60 {
				return 0, ErrDurationInvalid
			}
		}

		var total int64
		if len(parts) == 3 {
			// HH:MM:SS
			if cleanParts[0] > math.MaxInt64/3600 {
				return 0, ErrDurationOverflow
			}
			total = cleanParts[0] * 3600

			term2 := cleanParts[1] * 60
			if total > math.MaxInt64-term2 {
				return 0, ErrDurationOverflow
			}
			total += term2

			if total > math.MaxInt64-cleanParts[2] {
				return 0, ErrDurationOverflow
			}
			total += cleanParts[2]
		} else {
			// MM:SS
			if cleanParts[0] > math.MaxInt64/60 {
				return 0, ErrDurationOverflow
			}
			total = cleanParts[0] * 60

			if total > math.MaxInt64-cleanParts[1] {
				return 0, ErrDurationOverflow
			}
			total += cleanParts[1]
		}

		if total <= 0 {
			return 0, ErrDurationInvalid
		}
		return total, nil
	}

	// Case 2: Numeric with suffix (e.g. "90 min")
	fields := strings.Fields(length)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, ErrDurationInvalid
	}

	suffixes := []string{"minutes", "mins", "min", "min.", "m"}
	minStr := fields[0]

	if len(fields) == 2 {
		found := false
		suffix := strings.ToLower(fields[1])
		for _, s := range suffixes {
			if suffix == s {
				found = true
				break
			}
		}
		if !found {
			return 0, ErrDurationInvalid
		}
	} else {
		foundSuffix := ""
		lowerStr := strings.ToLower(minStr)
		for _, s := range suffixes {
			if strings.HasSuffix(lowerStr, s) {
				foundSuffix = s
				break
			}
		}
		if foundSuffix != "" {
			minStr = minStr[:len(minStr)-len(foundSuffix)]
		}
	}

	minutes, err := strconv.ParseInt(minStr, 10, 64)
	if err != nil {
		return 0, ErrDurationInvalid
	}
	if minutes < 0 {
		return 0, ErrDurationNegative
	}
	if minutes > math.MaxInt64/60 {
		return 0, ErrDurationOverflow
	}
	total := minutes * 60
	if total <= 0 {
		return 0, ErrDurationInvalid
	}
	return total, nil
}

// DeleteRecording handles DELETE /api/v3/recordings/{recordingId}
// Deletes the recording via OpenWebIF on the receiver.
func (s *Server) DeleteRecording(w http.ResponseWriter, r *http.Request, recordingId string) {
	v3recordings.DeleteRecording(w, r, recordingId, v3recordings.DeleteDeps{
		NewOWIClient: func() v3recordings.OpenWebIFClient {
			return openwebif.New(s.cfg.Enigma2.BaseURL)
		},
		WriteProblem: func(w http.ResponseWriter, r *http.Request, status int, typ, title, code, detail string) {
			writeProblem(w, r, status, typ, title, code, detail, nil)
		},
		Logger: func(msg string, keyvals ...any) {
			log.L().Error().Fields(keyvals).Msg(msg)
		},
	})
}

// SanitizeRecordingRelPath delegates to generic sanitizer
func SanitizeRecordingRelPath(p string) (string, bool) {
	return v3recordings.SanitizeRecordingRelPath(p)
}

// IsAllowedVideoSegment delegates to generic validator
func IsAllowedVideoSegment(path string) bool {
	return v3recordings.IsAllowedVideoSegment(path)
}

// ProbeDuration uses ffprobe to get the exact duration of a media file.
// Returns duration in time.Duration.
func (s *Server) ProbeDuration(ctx context.Context, path string) (time.Duration, error) {
	if s.vodManager == nil {
		return 0, errors.New("vod manager not initialized")
	}

	info, err := s.vodManager.Probe(ctx, path)
	if err != nil {
		return 0, err
	}

	if info.Video.Duration > 0 {
		return time.Duration(info.Video.Duration * float64(time.Second)), nil
	} else if info.Video.StartTime > 0 {
		// Fallback if needed? Usually duration is duration.
	}

	// Probe might return 0 duration for some streams, check usage.
	// Original logic returned "no updated duration" equivalent.
	if info.Video.Duration == 0 {
		return 0, errors.New("no duration found")
	}

	return time.Duration(info.Video.Duration * float64(time.Second)), nil
}

// escapeServiceRefPath percent-encodes a string for use in a URL path,
// but specifically preserves ':' and '/' as required by Enigma2 ServiceRefs.
// It escapes all other non-unreserved characters (including UTF-8 bytes).
func EscapeServiceRefPath(s string) string {
	return v3recordings.EscapeServiceRefPath(s)
}

func ShouldEscapeRefChar(c byte) bool {
	return v3recordings.ShouldEscapeRefChar(c)
}

// hasMP4Magic performs a shallow magic bytes check for genuine MP4 containers.
// It looks for the "ftyp" box within the first few bytes.
func hasMP4Magic(path string) bool {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	defer f.Close()

	// Read first 1024 bytes to be safe, although ftyp is usually at offset 4.
	// This covers potential preamble or variations in ISO base media file format.
	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil || n < 8 {
		return false
	}

	// Standard MP4: [4-byte size] + 'ftyp'
	if string(buf[4:8]) == "ftyp" {
		return true
	}

	// Some MP4 variants might have a small offset or different structure (M4A, etc.)
	// But for our VOD remux/symlink logic, we want the standard ISO MP4 header.
	return false
}

// ProbeDuration delegates to the VOD manager to get stream info.
