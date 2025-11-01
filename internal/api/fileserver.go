// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"golang.org/x/text/unicode/norm"
)

// secureFileServer creates a handler that serves files from the data directory
// with comprehensive security checks against path traversal, symlink escapes,
// and directory listing.
func (s *Server) secureFileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithComponentFromContext(r.Context(), "api")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "method_not_allowed").Msg("method not allowed")
			recordFileRequestDenied("method_not_allowed")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Path
		// Enhanced traversal detection including multiple URL-decode passes,
		// Unicode normalization, mixed-case encodings, and NUL bytes.
		if isPathTraversal(path) {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "path_escape").Msg("detected traversal sequence")
			recordFileRequestDenied("path_escape")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.HasSuffix(path, "/") || path == "" {
			logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "directory_listing").Msg("directory listing forbidden")
			recordFileRequestDenied("directory_listing")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		absDataDir, err := filepath.Abs(s.cfg.DataDir)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Msg("could not get absolute data dir")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		fullPath := filepath.Join(absDataDir, path)

		// Evaluate symlinks and clean the path
		realPath, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				logger.Info().Str("event", "file_req.not_found").Str("path", fullPath).Msg("file not found")
				recordFileRequestDenied("not_found")
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", fullPath).Msg("could not evaluate symlinks")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Also evaluate symlinks on the data directory itself to get a consistent base path.
		realDataDir, err := filepath.EvalSymlinks(absDataDir)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Msg("could not evaluate symlinks on data dir")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Security check: ensure the real path is within the real data directory
		// Use filepath.Rel for robust path containment check (protects against symlink escapes)
		relPath, err := filepath.Rel(realDataDir, realPath)
		if err != nil || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
			logger.Warn().
				Str("event", "file_req.denied").
				Str("path", path).
				Str("resolved_path", realPath).
				Str("data_dir", realDataDir).
				Str("reason", "path_escape").
				Msg("path escapes data directory")
			recordFileRequestDenied("path_escape")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Security check: ensure we are not serving a directory
		info, err := os.Stat(realPath)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not stat real path")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if info.IsDir() {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("reason", "directory_listing").Msg("resolved path is a directory")
			recordFileRequestDenied("directory_listing")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// --- ETag Caching Implementation ---
		// #nosec G304 -- realPath is validated to reside inside the data directory
		f, err := os.Open(realPath)
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not open real path for serving")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer func() {
			if err := f.Close(); err != nil {
				logger.Warn().Err(err).Str("path", realPath).Msg("failed to close file")
			}
		}()

		// Re-fetch stat info from the opened file handle
		info, err = f.Stat()
		if err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not stat opened file")
			recordFileRequestDenied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Generate a weak ETag based on file modtime and size.
		// W/ prefix indicates a weak validator, suitable for content that is semantically
		// equivalent but not necessarily byte-for-byte identical.
		etag := fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size())
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=3600") // Also set cache-control

		// Check if the client already has the same version of the file.
		if match := r.Header.Get("If-None-Match"); match != "" {
			if match == etag {
				recordFileCacheHit()
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		// All checks passed, serve the file content.
		// http.ServeContent is preferred over http.ServeFile when we already have an
		// open file, as it handles Range requests and sets Content-Type,
		// Content-Length, and Last-Modified headers correctly.

		// Set explicit charset for XML/M3U files to ensure proper UTF-8 handling
		lowerName := strings.ToLower(info.Name())
		if strings.HasSuffix(lowerName, ".xml") {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		} else if strings.HasSuffix(lowerName, ".m3u") || strings.HasSuffix(lowerName, ".m3u8") {
			w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
		}

		logger.Info().Str("event", "file_req.allowed").Str("path", path).Msg("serving file")
		recordFileRequestAllowed()
		recordFileCacheMiss()
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
}

// checkFile verifies that a file exists and is readable
func checkFile(ctx context.Context, path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	// #nosec G304 -- callers must provide paths checked by dataFilePath
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	if err := f.Close(); err != nil {
		// Log the error, but the function's outcome is already determined.
		log.FromContext(ctx).Warn().Err(err).Str("path", path).Msg("failed to close file during check")
	}
	return true
}

// isPathTraversal performs robust checks against path traversal attempts.
// It decodes the input multiple times to catch double-encoding, applies
// Unicode normalization, and searches for dangerous sequences including NULs.
func isPathTraversal(p string) bool {
	// Work on a copy
	decoded := p
	// Attempt multiple decode passes to catch double/triple encodings
	for i := 0; i < 3; i++ {
		prev := decoded
		if d, err := url.PathUnescape(decoded); err == nil {
			decoded = d
		} else {
			// As a fallback, try query unescape in case of stray '+' or query-like encoding
			if d2, err2 := url.QueryUnescape(decoded); err2 == nil {
				decoded = d2
			}
		}
		if decoded == prev {
			break
		}
	}

	lower := strings.ToLower(decoded)
	// Immediate dangerous byte patterns, independent of platform
	dangerSubstrings := []string{
		"..",        // parent traversal
		"..\\",      // windows-style backslash
		"%00",       // encoded NUL
		"\x00",      // literal NUL escape (defense-in-depth; may not appear literally)
		"%c0%ae",    // overlong UTF-8 for '.'
		"%e0%80%ae", // another overlong variant
	}
	for _, pat := range dangerSubstrings {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	// Literal NUL after decoding
	if strings.Contains(decoded, "\x00") || strings.IndexByte(decoded, 0x00) >= 0 {
		return true
	}

	// Normalize and check again for dot-dot
	normalized := strings.ToLower(norm.NFC.String(decoded))
	if strings.Contains(normalized, "..") || strings.Contains(normalized, "..\\") {
		return true
	}

	return false
}
