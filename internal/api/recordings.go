// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/netutil"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/ManuGH/xg2g/internal/v3/lease"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/profiles"
	v3store "github.com/ManuGH/xg2g/internal/v3/store"
	"github.com/google/uuid"
)

// Types are now generated in server_gen.go

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
		beginTs := int64(m.Begin)
		desc := m.Description
		if m.ExtendedDescription != "" {
			if desc != "" {
				desc += "\n\n"
			}
			desc += m.ExtendedDescription
		}

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
								log.L().Debug().Str("file", localPath).Int64("size", info.Size()).Msgf("estimated heuristic duration: %s", m.Length)
							}
						}
					}
				}
			}
		}

		recordingsList = append(recordingsList, RecordingItem{
			ServiceRef:  strPtr(m.ServiceRef),
			Title:       strPtr(m.Title),
			Description: strPtr(desc),
			Begin:       int64Ptr(beginTs),
			Length:      strPtr(m.Length),
			Filename:    strPtr(m.Filename),
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

// GetRecordingStream handles GET /api/v3/recordings/{recordingId}/stream
// Redirects to /stream/{recordingId} which handles proxying/HLS
func (s *Server) GetRecordingStream(w http.ResponseWriter, r *http.Request, recordingId string) {
	// Recording stream proxy deprecated
	http.Error(w, "recording streaming deprecated", http.StatusForbidden)
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

// GetRecordingHLSPlaylist handles GET /api/v3/recordings/{recordingId}/playlist.m3u8
// Creates a V3 session for the recording and redirects to the HLS playlist.
func (s *Server) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	serviceRef := s.decodeRecordingID(recordingId)
	if serviceRef == "" {
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
		return
	}

	// 1. Check V3 availability
	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if bus == nil || store == nil {
		http.Error(w, "V3 components not initialized", http.StatusServiceUnavailable)
		return
	}

	// 2. Determine Receiver Stream URL
	s.mu.RLock()
	host := s.cfg.OWIBase
	streamPort := s.cfg.StreamPort
	s.mu.RUnlock()

	// Fix scheme if OWIBase has it using robust normalization
	// Extract just the hostname/ip (ignoring port from OWIBase as we use StreamPort)
	h, _, err := netutil.NormalizeAuthority(host, "http")
	if err != nil {
		log.L().Error().Err(err).Str("owi_base", host).Msg("invalid OWI base")
		http.Error(w, "Invalid OWI base configuration", http.StatusInternalServerError)
		return
	}
	host = h

	// We use the resolved stream URL as the 'ServiceRef' in the V3 session
	// Ensure serviceRef doesn't start with / to avoid double slash with port
	// Sanitize ref using path.Clean to prevent double-slashes or traversal characters
	cleanRef := strings.TrimLeft(serviceRef, "/")
	cleanRef = path.Clean("/" + cleanRef) // force absolute for cleaning
	cleanRef = strings.TrimPrefix(cleanRef, "/")

	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		http.Error(w, "Invalid recording ref", http.StatusBadRequest)
		return
	}

	if strings.ContainsAny(cleanRef, "?#") {
		http.Error(w, "Invalid recording ref (query/fragment not allowed)", http.StatusBadRequest)
		return
	}

	// Construct URL safely with encoding (handles spaces etc.)
	u := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host, streamPort),
		Path:   cleanRef,
	}
	streamURL := u.String()

	// 2.3 Determine Playback Source (Local vs Receiver)
	sourceType := "receiver"
	source := streamURL

	// If path mappings configured, try local-first playback
	if s.recordingPathMapper != nil {
		// Extract filesystem path from Enigma2 service reference
		// Format: "1:0:0:0:0:0:0:0:0:0:/path/to/file.ts" -> "/path/to/file.ts"
		receiverPath := extractPathFromServiceRef(serviceRef)

		// Check if it's an absolute path (required for mapping)
		if strings.HasPrefix(receiverPath, "/") {
			if localPath, ok := s.recordingPathMapper.ResolveLocal(receiverPath); ok {
				// Check if file exists locally
				if _, err := os.Stat(localPath); err == nil {
					// Check file stability (avoid streaming files being written)
					s.mu.RLock()
					stableWindow := s.cfg.RecordingStableWindow
					s.mu.RUnlock()

					if recordings.IsStable(localPath, stableWindow) {
						sourceType = "local"
						source = localPath
					} else {
						log.L().Debug().
							Str("local_path", localPath).
							Dur("stable_window", stableWindow).
							Msg("file unstable, falling back to receiver")
					}
				}
			}
		}
	}

	// Log playback source decision
	log.L().Info().
		Str("recording_id", recordingId).
		Str("source_type", sourceType).
		Str("receiver_ref", serviceRef).
		Str("source", source).
		Msg("recording playback source selected")

	// 2.5 Admission Control (Hardening)
	// We must acquire a lease to ensure we respect tuner/transcoder slot limits.
	// If we skip this, the Worker will terminate the session immediately with R_LEASE_BUSY.
	config := s.GetConfig()
	if len(config.TunerSlots) == 0 {
		http.Error(w, "Service Unavailable (No Slots)", http.StatusServiceUnavailable)
		return
	}

	admissionLeaseTTL := 30 * time.Second
	dedupKey := lease.LeaseKeyService(streamURL)
	var acquiredLeases []v3store.Lease

	releaseLeases := func() {
		if len(acquiredLeases) == 0 {
			return
		}
		ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, l := range acquiredLeases {
			_ = store.ReleaseLease(ctxRel, l.Key(), l.Owner())
		}
	}

	// For recordings, we treat each playback as unique (no dedup sharing for now to keep it simple,
	// because user might want independent seek positions).
	sessionID := uuid.New().String()
	correlationID := uuid.New().String()

	// Dedup Lease (Service Ref)
	// For local playback, we bypass global deduplication (allow multiple stats)
	// by using the unique SessionID as the key.
	if sourceType == "local" {
		dedupKey = lease.LeaseKeyService("local:" + sessionID)
	}

	dedupLease, ok, err := store.TryAcquireLease(r.Context(), dedupKey, sessionID, admissionLeaseTTL)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to acquire dedup lease")
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		// If playing the same recording in another tab, we might hit this.
		// For now, fail fast.
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Recording already active", http.StatusConflict)
		return
	}
	acquiredLeases = append(acquiredLeases, dedupLease)

	// Tuner/Transcoder Lease
	// Optimization: Skip tuner lease for local playback (no tuner needed)
	var tunerLease v3store.Lease
	if sourceType != "local" {
		// Since we are same-package, we can call the unexported helper from handlers_v3.go
		var ok bool
		_, tunerLease, ok, err = tryAcquireTunerLease(r.Context(), store, sessionID, config.TunerSlots, admissionLeaseTTL)
		if err != nil {
			releaseLeases()
			log.L().Error().Err(err).Msg("failed to acquire tuner check")
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			releaseLeases()
			w.Header().Set("Retry-After", "5")
			// 409 Conflict implies "try again later"
			http.Error(w, "All tuners busy", http.StatusConflict)
			return
		}
		acquiredLeases = append(acquiredLeases, tunerLease)
	} else {
		log.L().Info().Str("sid", sessionID).Msg("skipping tuner lease for local playback")
	}

	// 3. Create Session Record
	// Format: recording-<uuid>
	// sessionID generated above

	// Default to generic High profile for recordings
	profileSpec := profiles.Resolve(profiles.ProfileHigh, r.UserAgent(), 0)

	session := &model.SessionRecord{
		SessionID:      sessionID,
		ServiceRef:     source, // Local path or Receiver URL
		Profile:        profileSpec,
		State:          model.SessionNew,
		CorrelationID:  correlationID,
		CreatedAtUnix:  time.Now().Unix(),
		UpdatedAtUnix:  time.Now().Unix(),
		LastAccessUnix: time.Now().Unix(),
		ContextData: map[string]string{
			"client_ip":   r.RemoteAddr,
			"type":        "recording",
			"source_type": sourceType,
		},
	}

	// Persist Session (Atomic)
	if err := store.PutSession(r.Context(), session); err != nil {
		releaseLeases()
		log.L().Error().Err(err).Msg("failed to persist recording session")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 4. Publish Start Event
	event := model.StartSessionEvent{
		Type:          model.EventStartSession,
		SessionID:     sessionID,
		ServiceRef:    source,
		ProfileID:     profileSpec.Name,
		CorrelationID: correlationID,
		StartMs:       0,
		RequestedAtUN: time.Now().Unix(),
	}

	if err := bus.Publish(r.Context(), string(model.EventStartSession), event); err != nil {
		releaseLeases()
		log.L().Error().Err(err).Msg("failed to publish recording start intent")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 5. Redirect to HLS Playlist
	// The V3 HLS handler waits for the playlist file to appear, so immediate redirect is fine.
	target := fmt.Sprintf("/api/v3/sessions/%s/hls/index.m3u8", sessionID)
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// GetRecordingHLSCustomSegment proxies to the V3 session handler.
// Since we redirect to the standard V3 HLS endpoint, this shouldn't be called directly by the client
// unless they manually constructed the URL. We can deprecate or redirect.
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	http.Error(w, "use HLS playlist", http.StatusNotFound)
}

// decodeRecordingID helper (factored out)
func (s *Server) decodeRecordingID(id string) string {
	if decodedBytes, err := base64.RawURLEncoding.DecodeString(id); err == nil {
		return string(decodedBytes)
	}
	if decodedBytes2, err2 := base64.URLEncoding.DecodeString(id); err2 == nil {
		return string(decodedBytes2)
	}
	if decoded, err3 := url.PathUnescape(id); err3 == nil {
		return decoded
	}
	return id
}

// DeleteRecording handles DELETE /api/v3/recordings/{recordingId}
// Deletes the recording file and associated sidecar files from the local disk.
func (s *Server) DeleteRecording(w http.ResponseWriter, r *http.Request, recordingId string) {
	recordingID := recordingId
	if recordingID == "" {
		http.Error(w, "Missing recording ID", http.StatusBadRequest)
		return
	}

	// Decode ID
	var serviceRef string
	if decodedBytes, err := base64.RawURLEncoding.DecodeString(recordingID); err == nil {
		serviceRef = string(decodedBytes)
	} else {
		if decodedBytes2, err2 := base64.URLEncoding.DecodeString(recordingID); err2 == nil {
			serviceRef = string(decodedBytes2)
		} else if decoded, err3 := url.PathUnescape(recordingID); err3 == nil {
			serviceRef = decoded
		} else {
			serviceRef = recordingID
		}
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
