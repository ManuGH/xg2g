// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/fsutil"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// RecordingResponse is the DTO for the recordings view
type RecordingResponse struct {
	Roots       []RecordingRoot `json:"roots"`                  // Available roots
	CurrentRoot string          `json:"current_root,omitempty"` // ID of current root
	CurrentPath string          `json:"current_path,omitempty"` // Relative path inside root
	Breadcrumbs []Breadcrumb    `json:"breadcrumbs,omitempty"`  // Navigation trail
	Directories []DirectoryItem `json:"directories,omitempty"`  // Subdirectories
	Recordings  []RecordingItem `json:"recordings,omitempty"`   // Files
}

type RecordingRoot struct {
	ID   string `json:"id"`
	Name string `json:"name"` // Label (e.g. "HDD")
}

type Breadcrumb struct {
	Name string `json:"name"`
	Path string `json:"path"` // Relative path for API query
}

type DirectoryItem struct {
	Name string `json:"name"`
	Path string `json:"path"` // Relative path for API query
}

type RecordingItem struct {
	ServiceRef  string `json:"service_ref"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Begin       int64  `json:"begin"`
	Length      string `json:"length"` // OWI returns string for length (e.g. "90 min" or seconds string)
	Filename    string `json:"filename"`
}

// GetRecordingsHandler handles GET /api/v2/recordings
// Query: ?root=<id>&path=<rel_path>
func (s *Server) GetRecordingsHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	// 1. Prepare Roots
	roots := cfg.RecordingRoots
	if len(roots) == 0 {
		roots = map[string]string{"hdd": "/media/hdd/movie"}
	}

	rootList := make([]RecordingRoot, 0, len(roots))
	for id, path := range roots {
		rootList = append(rootList, RecordingRoot{ID: id, Name: filepath.Base(path)})
	}
	// Sort for stability
	sort.Slice(rootList, func(i, j int) bool { return rootList[i].ID < rootList[j].ID })

	// 2. Parse Query
	qRootID := r.URL.Query().Get("root")
	qPath := r.URL.Query().Get("path")

	// If no root specified, return roots list (using first default for display)
	if qRootID == "" {
		if _, ok := roots["hdd"]; ok {
			qRootID = "hdd"
		} else if len(rootList) > 0 {
			qRootID = rootList[0].ID
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
	response := RecordingResponse{
		Roots:       rootList,
		CurrentRoot: qRootID,
		CurrentPath: qPath,
		Recordings:  make([]RecordingItem, 0, len(list.Movies)),
		Directories: make([]DirectoryItem, 0, len(list.Bookmarks)),
	}

	// Process Movies
	for _, m := range list.Movies {
		// Begin time: OWI might return string or int, now normalized by IntOrStringInt64
		beginTs := int64(m.Begin)

		response.Recordings = append(response.Recordings, RecordingItem{
			ServiceRef:  m.ServiceRef,
			Title:       m.Title,
			Description: m.Description,
			Begin:       beginTs,
			Length:      m.Length,
			Filename:    m.Filename,
		})
	}

	// Process Directories (Bookmarks)
	// We need to resolve the root symlinks to correctly compute relative paths for bookmarks
	// We mirror the canonicalization logic of fsutil.ConfineAbsPath: Abs -> EvalSymlinks
	absRoot, _ := filepath.Abs(rootAbs)
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		// If we can't resolve the root, fall back to Abs
		// Same as fsutil.ConfineAbsPath fallback logic (though confineAbsPath handles non-exist as error in some cases, here we tolerate for listing? No, existing behavior)
		if os.IsNotExist(err) {
			// If root effectively doesn't exist, we probably won't find bookmarks inside it anyway,
			// but let's keep consistent path base.
			realRoot = absRoot
		} else {
			// For other errors, fallback (or fail?)
			// confineAbsPath falls back to absRoot if IsNotExist, or returns err?
			// Actually fsutil.ConfineAbsPath currently: if err!=nil && !IsNotExist { return err } -> Fails closed.
			// But here we might want to be slightly lenient if only listing?
			// User requested: "mirror the exact root canonicalization logic"
			// if err != nil { realRoot = absRoot } (simple fallback as requested in option 2)
			realRoot = absRoot
		}
	}

	for _, b := range list.Bookmarks {
		// b.Path is absolute. Validate it is within the root.
		// We use fsutil.ConfineAbsPath to ensure it doesn't escape.
		resolvedBookmark, err := fsutil.ConfineAbsPath(rootAbs, b.Path)
		if err != nil {
			continue
		}

		// Compute relative display path against the resolved root
		// resolvedBookmark is already resolved/real path. realRoot is resolved/real root.
		rel, err := filepath.Rel(realRoot, resolvedBookmark)
		if err != nil {
			continue
		}

		// Ensure it's not "." (root itself) or escaping (though confine checked escape)
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}

		response.Directories = append(response.Directories, DirectoryItem{
			Name: b.Name,
			Path: rel,
		})
	}

	// Breadcrumbs
	if qPath != "" && qPath != "." {
		parts := strings.Split(qPath, string(filepath.Separator))
		built := ""
		for _, p := range parts {
			if p == "" {
				continue
			}
			built = filepath.Join(built, p)
			response.Breadcrumbs = append(response.Breadcrumbs, Breadcrumb{
				Name: p,
				Path: built,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error().Err(err).Msg("failed to encode response")
	}
}

// GetRecordingStreamHandler handles GET /api/v2/recordings/stream/{ref}
// Redirects to /stream/{ref} which handles proxying/HLS
func (s *Server) GetRecordingStreamHandler(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "ref")

	// Valid Service Ref?
	// Basic check: must start with 1:0:
	if !strings.HasPrefix(ref, "1:0:") {
		http.Error(w, "Invalid service reference", http.StatusBadRequest)
		return
	}

	// Redirect to existing stream endpoint
	target := fmt.Sprintf("/stream/%s", url.PathEscape(ref))
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// GetRecordingHLSPlaylistHandler handles GET /api/v2/recordings/{recordingId}/playlist.m3u8
// It acts as a reverse proxy to the internal Stream Proxy (port 18000),
// injecting the correct upstream URL extracted from the recording ID (service ref).
func (s *Server) GetRecordingHLSPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	if recordingID == "" {
		http.Error(w, "Missing recording ID", http.StatusBadRequest)
		return
	}

	// Decode ID (Base64RawURL expected)
	// Strategy: Try Base64RawURL decode.
	var serviceRef string
	if decodedBytes, err := base64.RawURLEncoding.DecodeString(recordingID); err == nil {
		serviceRef = string(decodedBytes)
	} else {
		// Fallback for transition/legacy: try standard URL (with padding) if Raw failed
		if decodedBytes2, err2 := base64.URLEncoding.DecodeString(recordingID); err2 == nil {
			serviceRef = string(decodedBytes2)
		} else if decoded, err3 := url.PathUnescape(recordingID); err3 == nil {
			// Last resort: PathUnescape
			serviceRef = decoded
		} else {
			serviceRef = recordingID
		}
	}

	if serviceRef == "" {
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg // Guarded access
	snap := s.snap
	s.mu.RUnlock()

	// Logic: Extract the file path from the Service Ref
	// Standard Enigma2 ref ends with :/path/to/file.ts
	// Robust extraction: Find the part starting with /
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

	// SECURITY: Confinement Check
	// Prevent access to files outside of configured RecordingRoots
	allowed := false

	// Check against all configured roots
	// Check against all configured roots
	roots := cfg.RecordingRoots
	if len(roots) == 0 {
		// Default fallback if no roots configured
		roots = map[string]string{"hdd": "/media/hdd/movie"}
	}

	var resolvedPath string
	for _, rootPath := range roots {
		// We use fsutil.ConfineAbsPath because HLS ServiceRef IDs contain absolute paths (usually)
		if abs, err := fsutil.ConfineAbsPath(rootPath, filePath); err == nil {
			allowed = true
			resolvedPath = abs
			break
		}
	}

	if !allowed {
		log.Warn().Str("path", filePath).Msg("recording path traversal blocked")
		http.Error(w, "Access denied: Recording path not in allowed roots", http.StatusForbidden)
		return
	}

	// Use Local File Path as Upstream
	// Since we have direct access to the file (verified above), using the local path
	// avoids network overhead and dependency on the receiver's HTTP streaming capabilities.
	upstreamURL := resolvedPath

	// Proxy to internal Stream Proxy HLS handler
	// We use Base64RawURL Encoded ServiceRef as the ID for HLS session.
	sessID := base64.RawURLEncoding.EncodeToString([]byte(serviceRef))

	proxyHost := "127.0.0.1:18000"
	if addr := snap.Runtime.StreamProxy.ListenAddr; addr != "" {
		_, p, _ := net.SplitHostPort(addr)
		if p != "" {
			proxyHost = "127.0.0.1:" + p
		}
	}

	targetPath := fmt.Sprintf("/hls/%s/playlist.m3u8", sessID)
	// Simplify Reverse Proxy: Target is just host
	proxyTarget, _ := url.Parse(fmt.Sprintf("http://%s", proxyHost))

	// Create reverse proxy
	p := httputil.NewSingleHostReverseProxy(proxyTarget)

	p.Director = func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = proxyHost
		req.URL.Path = targetPath

		// Set query params
		q := req.URL.Query()
		q.Set("upstream", upstreamURL)

		if r.URL.Query().Get("llhls") == "1" {
			q.Set("llhls", "1")
		}

		req.URL.RawQuery = q.Encode()
		req.Host = proxyHost // Important for keeping host header correct for internal proxy
	}

	p.ServeHTTP(w, r)
}

// GetRecordingHLSCustomSegmentHandler handles GET /api/v2/recordings/{recordingId}/{segment}
// Proxies segment requests to the internal Stream Proxy.
func (s *Server) GetRecordingHLSCustomSegmentHandler(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingId")
	segment := chi.URLParam(r, "segment")

	if recordingID == "" || segment == "" {
		http.NotFound(w, r)
		return
	}

	// Security: Validate Segment Name using shared regex
	if !segmentAllowList.MatchString(segment) {
		http.Error(w, "Invalid segment name", http.StatusBadRequest)
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

	// If we used Base64 as SessID in playlist, we must use it here too.
	sessID := base64.RawURLEncoding.EncodeToString([]byte(serviceRef))

	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()

	proxyHost := "127.0.0.1:18000"
	if addr := snap.Runtime.StreamProxy.ListenAddr; addr != "" {
		_, p, _ := net.SplitHostPort(addr)
		if p != "" {
			proxyHost = "127.0.0.1:" + p
		}
	}

	targetPath := fmt.Sprintf("/hls/%s/%s", sessID, segment)
	proxyTarget, _ := url.Parse(fmt.Sprintf("http://%s", proxyHost))

	p := httputil.NewSingleHostReverseProxy(proxyTarget)
	p.Director = func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = proxyHost
		req.URL.Path = targetPath
		req.Host = proxyHost
	}

	p.ServeHTTP(w, r)
}

// DeleteRecordingHandler handles DELETE /api/v2/recordings/{ref}
// Deletes the recording file and associated sidecar files from the local disk.
func (s *Server) DeleteRecordingHandler(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "ref")
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
