// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/fsutil"
	"github.com/rs/zerolog/log"
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
	roots := cfg.RecordingRoots
	if len(roots) == 0 {
		roots = map[string]string{"hdd": "/media/hdd/movie"}
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

	// fsutil.ConfineRelPath handles cleaning, symlinks, backslashes and strict confinement
	cleanTarget, err := fsutil.ConfineRelPath(rootAbs, qPath)
	if err != nil {
		log.Warn().Err(err).Str("root", rootAbs).Str("path", qPath).Msg("path traversal detected")
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	// Note: cleanTarget is the resolved absolute path

	// 4. Fetch from Receiver
	client := s.newOpenWebIFClient(cfg, snap)
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
		recordingsList = append(recordingsList, RecordingItem{
			ServiceRef:  strPtr(m.ServiceRef),
			Title:       strPtr(m.Title),
			Description: strPtr(m.Description),
			Begin:       int64Ptr(beginTs),
			Length:      strPtr(m.Length),
			Filename:    strPtr(m.Filename),
		})
	}

	directoriesList := make([]DirectoryItem, 0, len(list.Bookmarks))
	// Process Directories (Bookmarks)
	absRoot, _ := filepath.Abs(rootAbs)
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			realRoot = absRoot
		} else {
			realRoot = absRoot
		}
	}

	for _, b := range list.Bookmarks {
		resolvedBookmark, err := fsutil.ConfineAbsPath(rootAbs, b.Path)
		if err != nil {
			continue
		}

		rel, err := filepath.Rel(realRoot, resolvedBookmark)
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
// It acts as a reverse proxy to the internal Stream Proxy (port 18000),
// injecting the correct upstream URL extracted from the recording ID (service ref).
func (s *Server) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	// Recording stream proxy deprecated
	http.Error(w, "recording streaming deprecated", http.StatusForbidden)
}

// GetRecordingHLSCustomSegment handles GET /api/v3/recordings/{recordingId}/{segment}
// Proxies segment requests to the internal Stream Proxy.
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	// Recording stream proxy deprecated
	http.Error(w, "recording streaming deprecated", http.StatusForbidden)
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

	// Extract path from ServiceRef
	parts := strings.Split(serviceRef, ":")
	var filePath string
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasPrefix(parts[i], "/") {
			filePath = parts[i]
			break
		}
	}

	if filePath == "" {
		http.Error(w, "Invalid recording reference (no file path found)", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// SECURITY: Confinement Check
	allowed := false
	var resolvedPath string
	roots := cfg.RecordingRoots
	if len(roots) == 0 {
		roots = map[string]string{"hdd": "/media/hdd/movie"}
	}

	for _, rootPath := range roots {
		if abs, err := fsutil.ConfineAbsPath(rootPath, filePath); err == nil {
			allowed = true
			resolvedPath = abs
			break
		}
	}

	if !allowed {
		log.Warn().Str("path", filePath).Msg("recording delete blocked: path not in allowed roots")
		http.Error(w, "Access denied: Path not in allowed roots", http.StatusForbidden)
		return
	}

	// Delete Content
	log.Info().Str("path", resolvedPath).Msg("deleting recording")
	if err := os.Remove(resolvedPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Str("path", resolvedPath).Msg("failed to delete recording file")
		http.Error(w, "Failed to delete file", http.StatusInternalServerError)
		return
	}

	// Delete Sidecars (.eit, .ts.meta, .ts.cuts, .ts.ap, .ts.sc, .jpg)
	base := resolvedPath
	ext := filepath.Ext(base)
	noExt := strings.TrimSuffix(base, ext)

	sidecars := []string{
		base + ".meta",
		base + ".cuts",
		base + ".ap",
		base + ".sc",
		noExt + ".eit",
		noExt + ".jpg",
	}

	for _, sc := range sidecars {
		if err := os.Remove(sc); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("file", sc).Msg("failed to delete sidecar file")
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
