// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
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
	"time"
	"unicode/utf8"

	"github.com/ManuGH/xg2g/internal/fsutil"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/netutil"
	"github.com/ManuGH/xg2g/internal/recordings"
	ffmpegexec "github.com/ManuGH/xg2g/internal/v3/exec/ffmpeg"
	"github.com/ManuGH/xg2g/internal/v3/profiles"
)

// Types are now generated in server_gen.go

var (
	errRecordingInvalid  = errors.New("invalid recording ref")
	errRecordingNotReady = errors.New("recording not ready")
	errRecordingNotFound = errors.New("recording not found")
)

const (
	recordingIDMinLen = 16
	recordingIDMaxLen = 2048
)

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
	roots := make(map[string]string)

	// Start with configured roots
	if len(cfg.RecordingRoots) > 0 {
		for k, v := range cfg.RecordingRoots {
			roots[k] = v
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
			// Sanitize ID (simple slugification)
			id = strings.ToLower(strings.ReplaceAll(id, " ", "_"))

			// Only add if not already present (Config takes precedence)
			if _, exists := roots[id]; !exists {
				roots[id] = loc.Path
			}
		}
	} else {
		log.L().Warn().Err(err).Msg("failed to discover recording locations")
	}

	// Final check: if still empty, assume standard HDD
	if len(roots) == 0 {
		roots["hdd"] = "/media/hdd/movie"
	}

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
	// Security: Strict confinement using sanitizeRecordingRelPath
	rootAbs, ok := roots[qRootID]
	if !ok {
		http.Error(w, "Invalid root ID", http.StatusBadRequest)
		return
	}

	// 3. Resolve & Validate Path
	// ConfineRelPath uses local FS checks which fail for remote receiver paths.
	// We switch to string-based validation only using our POSIX helper.
	cleanRel, blocked := sanitizeRecordingRelPath(qPath)
	if blocked {
		log.L().Warn().Str("path", qPath).Msg("path traversal attempt detected")
		http.Error(w, "Access denied", http.StatusForbidden)
		return
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

		durationSeconds, durationKnown := parseRecordingDurationSeconds(m.Length)

		// Fallback for missing length on local files (NAS)
		if (m.Length == "" || m.Length == "0") && s.recordingPathMapper != nil {
			// Extract filesystem path
			receiverPath := extractPathFromServiceRef(m.ServiceRef)
			if strings.HasPrefix(receiverPath, "/") {
				if localPath, ok := s.recordingPathMapper.ResolveLocal(receiverPath); ok {
					if info, err := os.Stat(localPath); err == nil {
						// Try robust probe first (Stufe 2)
						dur, pErr := probeDuration(r.Context(), localPath)
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

		recordingID := encodeRecordingID(m.ServiceRef)
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

// extractPathFromServiceRef extracts the filesystem path from an Enigma2 service reference.
// Enigma2 service references have the format: "1:0:0:0:0:0:0:0:0:0:/path/to/file.ts"
// Returns the path part (everything after the last colon) if it starts with "/",
// otherwise returns the original string unchanged (defensive).
func extractPathFromServiceRef(serviceRef string) string {
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
	serviceRef = strings.TrimSpace(serviceRef)
	if serviceRef == "" {
		return "", "", "", errRecordingInvalid
	}

	receiverPath := extractPathFromServiceRef(serviceRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return "", "", "", errRecordingInvalid
	}

	cleanRef := strings.TrimLeft(receiverPath, "/")
	cleanRef = path.Clean("/" + cleanRef)
	cleanRef = strings.TrimPrefix(cleanRef, "/")
	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		return "", "", "", errRecordingInvalid
	}
	if strings.ContainsAny(cleanRef, "?#") {
		return "", "", "", errRecordingInvalid
	}

	s.mu.RLock()
	host := s.cfg.OWIBase
	streamPort := s.cfg.StreamPort
	stableWindow := s.cfg.RecordingStableWindow
	policy := strings.ToLower(strings.TrimSpace(s.cfg.RecordingPlaybackPolicy))
	pathMapper := s.recordingPathMapper
	s.mu.RUnlock()

	allowLocal := policy != "receiver_only"
	allowReceiver := policy != "local_only"

	if allowLocal && pathMapper != nil {
		if localPath, ok := pathMapper.ResolveLocal(receiverPath); ok {
			parts, err := recordingParts(localPath)
			if err == nil && len(parts) > 0 {
				if recordings.IsStable(parts[len(parts)-1], stableWindow) {
					var durationSeconds string
					if dur, pErr := probeDuration(ctx, localPath); pErr == nil && dur > 0 {
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
				return "", "", "", errRecordingNotFound
			}
		} else if !allowReceiver {
			return "", "", "", errRecordingNotFound
		}
	}

	if !allowReceiver {
		return "", "", "", errRecordingNotFound
	}

	h, _, err := netutil.NormalizeAuthority(host, "http")
	if err != nil {
		return "", "", "", fmt.Errorf("invalid OWI base: %w", err)
	}
	host = h

	u := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host, streamPort),
		Path:   cleanRef,
	}
	streamURL := u.String()

	return "receiver", streamURL, "", nil
}

// GetRecordingHLSPlaylist handles GET /api/v3/recordings/{recordingId}/playlist.m3u8.
// Recording playback is asset-based (VOD) and does not use v3 sessions.
func (s *Server) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.decodeRecordingID(recordingId)
	if serviceRef == "" {
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
		return
	}

	playlistPath, err := s.ensureRecordingPlaylist(r.Context(), serviceRef)
	if err != nil {
		s.writeRecordingPlaybackError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, playlistPath)
}

// GetRecordingHLSCustomSegment serves the generated HLS segments.
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	serviceRef := s.decodeRecordingID(recordingId)
	if serviceRef == "" {
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
		return
	}

	segment = filepath.Base(segment)
	if segment == "." || segment == ".." || strings.Contains(segment, "\\") {
		http.Error(w, "invalid segment name", http.StatusBadRequest)
		return
	}
	if !recordingSegmentAllowed(segment) {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return
	}

	s.mu.RLock()
	hlsRoot := s.cfg.HLSRoot
	s.mu.RUnlock()

	cacheDir, err := recordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		log.L().Error().Err(err).Msg("recording cache dir unavailable")
		http.Error(w, "recording storage unavailable", http.StatusServiceUnavailable)
		return
	}

	filePath, err := fsutil.ConfineRelPath(cacheDir, segment)
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
	} else if strings.HasSuffix(segment, ".m4s") || strings.HasSuffix(segment, ".cmfv") {
		w.Header().Set("Content-Type", "video/iso.segment")
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		w.Header().Set("Content-Type", "video/MP2T")
		w.Header().Set("Cache-Control", "public, max-age=60")
	}

	f, err := os.Open(filePath) // #nosec G304 -- filePath is confined to recording cache dir.
	if err != nil {
		http.Error(w, "segment unavailable", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	http.ServeContent(w, r, segment, info.ModTime(), f)
}

func (s *Server) ensureRecordingPlaylist(ctx context.Context, serviceRef string) (string, error) {
	s.mu.RLock()
	hlsRoot := s.cfg.HLSRoot
	sfg := &s.recordingSfg
	s.mu.RUnlock()

	if err := validateRecordingRef(serviceRef); err != nil {
		return "", err
	}

	cacheDir, err := recordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}
	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	if recordingPlaylistReady(cacheDir) {
		return playlistPath, nil
	}

	_, err, _ = sfg.Do(cacheDir, func() (any, error) {
		if recordingPlaylistReady(cacheDir) {
			return playlistPath, nil
		}
		sourceType, source, _, err := s.resolveRecordingPlaybackSource(ctx, serviceRef)
		if err != nil {
			return nil, err
		}
		if err := os.RemoveAll(cacheDir); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		// #nosec G301 -- cache dir serves playback assets.
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return nil, err
		}
		if err := s.generateRecordingPlaylist(ctx, cacheDir, sourceType, source); err != nil {
			return nil, err
		}
		if !recordingPlaylistReady(cacheDir) {
			return nil, fmt.Errorf("recording playlist incomplete")
		}
		return playlistPath, nil
	})
	if err != nil {
		return "", err
	}

	return playlistPath, nil
}

func (s *Server) generateRecordingPlaylist(ctx context.Context, cacheDir, sourceType, source string) error {
	tmpPlaylist, finalPlaylist := ffmpegexec.PlaylistPaths(cacheDir)
	_ = os.Remove(tmpPlaylist)
	_ = os.Remove(finalPlaylist)

	input := ffmpegexec.InputSpec{
		StreamURL: source,
	}

	var concatFile string
	if sourceType == "local" {
		parts, err := recordingParts(source)
		if err != nil {
			return err
		}
		if len(parts) != 1 || parts[0] != source {
			concatFile = filepath.Join(cacheDir, "concat.txt")
			if err := writeConcatList(concatFile, parts); err != nil {
				return err
			}
			input.StreamURL = concatFile
		}
	}

	output := ffmpegexec.OutputSpec{
		HLSPlaylist:        tmpPlaylist,
		SegmentFilename:    ffmpegexec.SegmentPattern(cacheDir, ".ts"),
		SegmentDuration:    6,
		PlaylistWindowSize: 0,
	}

	profileSpec := profiles.Resolve(profiles.ProfileDVR, "", 0)
	profileSpec.VOD = true
	profileSpec.DVRWindowSec = 0
	profileSpec.LLHLS = false

	args, err := ffmpegexec.BuildHLSArgs(input, output, profileSpec)
	if err != nil {
		return err
	}
	if concatFile != "" {
		args = insertArgsBefore(args, "-i", []string{"-f", "concat", "-safe", "0"})
	}

	s.mu.RLock()
	ffmpegBin := s.cfg.FFmpegBin
	s.mu.RUnlock()
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	cmd := exec.CommandContext(ctx, ffmpegBin, args...) // #nosec G204 -- args are constructed internally.
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.L().Error().
			Err(err).
			Str("source_type", sourceType).
			Str("source", source).
			Str("ffmpeg", ffmpegBin).
			Msgf("ffmpeg failed: %s", strings.TrimSpace(string(out)))
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	if err := os.Rename(tmpPlaylist, finalPlaylist); err != nil {
		return fmt.Errorf("promote playlist: %w", err)
	}

	return nil
}

func recordingCacheDir(hlsRoot, serviceRef string) (string, error) {
	if strings.TrimSpace(hlsRoot) == "" {
		return "", fmt.Errorf("hls root not configured")
	}
	return filepath.Join(hlsRoot, "recordings", recordingCacheKey(serviceRef)), nil
}

func recordingCacheKey(serviceRef string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(serviceRef)))
	return hex.EncodeToString(sum[:])
}

func validateRecordingRef(serviceRef string) error {
	serviceRef = strings.TrimSpace(serviceRef)
	if serviceRef == "" {
		return errRecordingInvalid
	}
	receiverPath := extractPathFromServiceRef(serviceRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return errRecordingInvalid
	}
	cleanRef := strings.TrimLeft(receiverPath, "/")
	cleanRef = path.Clean("/" + cleanRef)
	cleanRef = strings.TrimPrefix(cleanRef, "/")
	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		return errRecordingInvalid
	}
	if strings.ContainsAny(cleanRef, "?#") {
		return errRecordingInvalid
	}
	return nil
}

func recordingPlaylistReady(cacheDir string) bool {
	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	info, err := os.Stat(playlistPath)
	if err != nil || info.IsDir() {
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
		if recordingSegmentFile(entry.Name()) {
			return true
		}
	}
	return false
}

func recordingSegmentAllowed(name string) bool {
	if name == "init.mp4" {
		return true
	}
	return recordingSegmentFile(name)
}

func recordingSegmentFile(name string) bool {
	if !strings.HasPrefix(name, "seg_") {
		return false
	}
	switch {
	case strings.HasSuffix(name, ".ts"):
		return true
	case strings.HasSuffix(name, ".m4s"):
		return true
	case strings.HasSuffix(name, ".mp4"):
		return true
	case strings.HasSuffix(name, ".cmfv"):
		return true
	default:
		return false
	}
}

func recordingParts(basePath string) ([]string, error) {
	basePath = filepath.Clean(basePath)
	baseInfo, err := os.Stat(basePath)
	baseExists := err == nil && baseInfo.Mode().IsRegular()

	dir := filepath.Dir(basePath)
	baseName := filepath.Base(basePath)
	entries, readErr := os.ReadDir(dir)
	if readErr != nil && !baseExists {
		if os.IsNotExist(readErr) {
			return nil, errRecordingNotFound
		}
		return nil, readErr
	}

	type part struct {
		index int
		path  string
	}
	var parts []part
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, baseName+".") {
			continue
		}
		suffix := strings.TrimPrefix(name, baseName+".")
		if suffix == "" {
			continue
		}
		if !allDigits(suffix) {
			continue
		}
		idx, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		parts = append(parts, part{index: idx, path: filepath.Join(dir, name)})
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
	return nil, errRecordingNotFound
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func writeConcatList(path string, parts []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	for _, part := range parts {
		line := "file " + concatEscape(part) + "\n"
		if _, err := io.WriteString(f, line); err != nil {
			return err
		}
	}
	return f.Sync()
}

func concatEscape(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\\' || r == '\'' || r == ' ' || r == '#' || r == '\t' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func insertArgsBefore(args []string, needle string, insert []string) []string {
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

func (s *Server) writeRecordingPlaybackError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRecordingInvalid):
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
	case errors.Is(err, errRecordingNotFound):
		http.Error(w, "Recording not found", http.StatusNotFound)
	case errors.Is(err, errRecordingNotReady):
		http.Error(w, "Recording not ready", http.StatusConflict)
	default:
		log.L().Error().Err(err).Msg("recording playback failed")
		http.Error(w, "Failed to prepare recording", http.StatusInternalServerError)
	}
}

func encodeRecordingID(serviceRef string) string {
	if strings.TrimSpace(serviceRef) == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(serviceRef))
}

func validRecordingID(id string) bool {
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

// decodeRecordingID helper (factored out)
func (s *Server) decodeRecordingID(id string) string {
	id = strings.TrimSpace(id)
	if !validRecordingID(id) {
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
		return decoded
	}
	return ""
}

func parseRecordingDurationSeconds(length string) (int64, bool) {
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
			seconds, err2 := strconv.Atoi(parts[1])
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
	serviceRef := s.decodeRecordingID(recordingId)
	if serviceRef == "" || validateRecordingRef(serviceRef) != nil {
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

// sanitizeRecordingRelPath implementation for POSIX paths
// Returns the cleaned relative path and whether it was blocked (traversal detected).
func sanitizeRecordingRelPath(qPath string) (string, bool) {
	// Use path.Clean for POSIX/receiver paths
	cleanRel := path.Clean(qPath)

	// path.Clean("") -> "."
	if cleanRel == "." {
		cleanRel = ""
	}

	// Reject absolute paths (treat as relative foundation)
	cleanRel = strings.TrimPrefix(cleanRel, "/")

	// Check for traversal
	if cleanRel == ".." || strings.HasPrefix(cleanRel, "../") {
		return "", true
	}

	return cleanRel, false
}

// probeDuration uses ffprobe to get the exact duration of a media file.
// Returns duration in time.Duration.
func probeDuration(ctx context.Context, path string) (time.Duration, error) {
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
