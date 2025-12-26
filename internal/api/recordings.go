// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/profiles"
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
	} else if len(roots) == 0 {
		// Only set default if NO discovery happens later?
		// Actually, let's keep HDD default initially, but if discovery finds something else, good.
		// Use empty map initially so discovery can populate it alone if needed.
		// If both empty eventually, we add default.
	}

	// Dynamic Discovery: Fetch locations from OpenWebIF
	client := s.newOpenWebIFClient(cfg, snap)
	if locs, err := client.GetLocations(r.Context()); err == nil {
		for _, loc := range locs {
			// Generate an ID for the root. Use the name if available, else sanitized path base.
			id := loc.Name
			if id == "" {
				id = filepath.Base(loc.Path)
			}
			// Sanitize ID (simple slugification)
			id = strings.ToLower(strings.ReplaceAll(id, " ", "_"))

			// Only add if not already present (Config takes precedence)
			if _, exists := roots[id]; !exists {
				roots[id] = loc.Path
			}
		}
	} else {
		log.Ctx(r.Context()).Warn().Err(err).Msg("failed to discover recording locations")
	}

	// Final check: if still empty, assume standard HDD
	if len(roots) == 0 {
		roots["hdd"] = "/media/hdd/movie"
	}

	rootList := make([]RecordingRoot, 0, len(roots))
	for id, path := range roots {
		// Local vars for pointers
		i := id
		n := filepath.Base(path)
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
	// Security: Strict confinement using confineRelPath
	rootAbs, ok := roots[qRootID]
	if !ok {
		http.Error(w, "Invalid root ID", http.StatusBadRequest)
		return
	}

	// 3. Resolve & Validate Path
	// ConfineRelPath uses local FS checks which fail for remote receiver paths.
	// We switch to string-based validation only.
	cleanRel := filepath.Clean(qPath)
	if filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, "/") {
		// If qPath was absolute, treat it as relative to root if possible, or just reject.
		// For simplicity, force relative.
		cleanRel = strings.TrimPrefix(cleanRel, "/")
	}
	// Check for traversal
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		log.Warn().Str("path", qPath).Msg("path traversal attempt detected")
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Construct the target path to send to receiver
	// Note: rootAbs is the remote path on the receiver (e.g. /media/hdd/movie)
	cleanTarget := filepath.Join(rootAbs, cleanRel)

	// 4. Fetch from Receiver
	// client is already initialized above for discovery
	list, err := client.GetRecordings(r.Context(), cleanTarget)
	if err != nil {
		log.Ctx(r.Context()).Error().Err(err).Str("path", cleanTarget).Msg("failed to fetch recordings")
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
	// Process Directories (Bookmarks)
	for _, b := range list.Bookmarks {
		// Bookmarks are absolute paths on the receiver.
		// We just want to see if they are inside our current root to show them as folders.
		// Or if we should show them as "links"?
		// Actually, standard logic is just to list them if they are subdirs.

		// Simple string check: does bookmark start with rootAbs?
		if !strings.HasPrefix(b.Path, rootAbs) {
			continue
		}

		rel, err := filepath.Rel(rootAbs, b.Path)
		if err != nil {
			continue
		}

		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}

		directoriesList = append(directoriesList, DirectoryItem{
			Name: strPtr(b.Name),
			Path: strPtr(rel),
		})
	}

	breadcrumbsList := make([]Breadcrumb, 0)
	if qPath != "" && qPath != "." {
		parts := strings.Split(qPath, string(filepath.Separator))
		built := ""
		for _, p := range parts {
			if p == "" {
				continue
			}
			built = filepath.Join(built, p)
			breadcrumbsList = append(breadcrumbsList, Breadcrumb{
				Name: strPtr(p),
				Path: strPtr(built),
			})
		}
	}

	// Fix RootList to generated type
	genRoots := make([]RecordingRoot, 0, len(rootList))
	for _, r := range rootList {
		// Just copy the structs/pointers since they are already correct type
		genRoots = append(genRoots, r)
	}

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
		log.Error().Err(err).Msg("failed to encode response")
	}
}

// GetRecordingStream handles GET /api/v3/recordings/{recordingId}/stream
// Redirects to /stream/{recordingId} which handles proxying/HLS
func (s *Server) GetRecordingStream(w http.ResponseWriter, r *http.Request, recordingId string) {
	// Recording stream proxy deprecated
	http.Error(w, "recording streaming deprecated", http.StatusForbidden)
}

// GetRecordingHLSPlaylist handles GET /api/v3/recordings/{recordingId}/playlist.m3u8
// Creates a V3 session for the recording and redirects to the HLS playlist.
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

	// Fix scheme if OWIBase has it
	if strings.Contains(host, "://") {
		// Parse OWIBase to get host
		if u, err := url.Parse(host); err == nil {
			host = u.Hostname()
		}
	}

	// We use the resolved stream URL as the 'ServiceRef' in the V3 session
	streamURL := fmt.Sprintf("http://%s:%d/%s", host, streamPort, serviceRef)

	// 3. Create Session Record
	// Format: recording-<uuid>
	sessionID := "rec-" + uuid.New().String()

	// Default to generic High profile for recordings
	profileSpec := profiles.Resolve(profiles.ProfileHigh, r.UserAgent(), 0)

	session := &model.SessionRecord{
		SessionID:      sessionID,
		ServiceRef:     streamURL, // <--- The actual playable URL
		Profile:        profileSpec,
		State:          model.SessionNew,
		CreatedAtUnix:  time.Now().Unix(),
		UpdatedAtUnix:  time.Now().Unix(),
		LastAccessUnix: time.Now().Unix(),
		ContextData:    map[string]string{"client_ip": r.RemoteAddr, "type": "recording"},
	}

	// Persist Session (Atomic)
	if err := store.PutSession(r.Context(), session); err != nil {
		log.Error().Err(err).Msg("failed to persist recording session")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 4. Publish Start Event
	event := model.StartSessionEvent{
		Type:          model.EventStartSession,
		SessionID:     sessionID,
		ServiceRef:    streamURL,
		ProfileID:     profileSpec.Name,
		RequestedAtUN: time.Now().Unix(),
	}

	if err := bus.Publish(r.Context(), string(model.EventStartSession), event); err != nil {
		log.Error().Err(err).Msg("failed to publish recording start intent")
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

	log.Info().Str("sref", serviceRef).Msg("requesting recording deletion via OpenWebIF")

	if err := client.DeleteMovie(r.Context(), serviceRef); err != nil {
		log.Error().Err(err).Str("sref", serviceRef).Msg("failed to delete recording")
		// Map generic error to 500. We could try to parse "not found" but OWI usually returns generic "false"
		http.Error(w, fmt.Sprintf("Failed to delete recording: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
