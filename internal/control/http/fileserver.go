// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
	"golang.org/x/text/unicode/norm"
)

var (
	allowedPublicFiles = map[string]bool{
		"playlist.m3u": true,
		"xmltv.xml":    true,
		"epg.xml":      true,
	}
	sensitiveFileExtensions = []string{
		".yaml", ".yml", ".key", ".pem", ".env", ".db", ".json", ".ini", ".conf",
	}
)

var (
	errSecureFileNotFound  = errors.New("secure file not found")
	errSecurePathEscape    = errors.New("secure path escape")
	errSecureDirectoryPath = errors.New("secure directory path")
)

// SecureFileServer creates a handler that serves files from the data directory
// with comprehensive security checks against path traversal, symlink escapes,
// and directory listing.
func SecureFileServer(dataDir string, metrics FileMetrics) http.Handler {
	if metrics == nil {
		metrics = NewNoopFileMetrics()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithComponentFromContext(r.Context(), "control.http")

		path, ok := validateSecureFileRequest(w, r, logger, metrics)
		if !ok {
			return
		}

		realPath, err := resolveSecureFilePath(dataDir, path)
		if err != nil {
			handleSecureFileResolveError(w, path, dataDir, realPath, err, logger, metrics)
			return
		}

		if err := serveSecureFileContent(w, r, realPath, path, logger, metrics); err != nil {
			logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", realPath).Msg("could not serve file")
			metrics.Denied("internal_error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
}

func validateSecureFileRequest(w http.ResponseWriter, r *http.Request, logger zerolog.Logger, metrics FileMetrics) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "method_not_allowed").Msg("method not allowed")
		metrics.Denied("method_not_allowed")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}

	path := r.URL.Path
	filename := filepath.Base(path)
	if !allowedPublicFiles[filename] {
		ext := strings.ToLower(filepath.Ext(filename))
		if isSensitiveExtension(ext) {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("reason", "forbidden_extension").Msg("attempted access to sensitive file extension")
			metrics.Denied("forbidden_extension")
		} else {
			logger.Warn().Str("event", "file_req.denied").Str("path", path).Str("reason", "not_allowlisted").Msg("file not in allowlist")
			metrics.Denied("forbidden_file")
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return "", false
	}

	if isPathTraversal(path) {
		logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "path_escape").Msg("detected traversal sequence")
		metrics.Denied("path_escape")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return "", false
	}
	if strings.HasSuffix(path, "/") || path == "" {
		logger.Warn().Str("event", "file_req.denied").Str("path", r.URL.Path).Str("reason", "directory_listing").Msg("directory listing forbidden")
		metrics.Denied("directory_listing")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return "", false
	}

	return path, true
}

func isSensitiveExtension(ext string) bool {
	for _, denied := range sensitiveFileExtensions {
		if ext == denied {
			return true
		}
	}
	return false
}

func resolveSecureFilePath(dataDir, requestPath string) (string, error) {
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data dir: %w", err)
	}

	fullPath := filepath.Join(absDataDir, requestPath)
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fullPath, fmt.Errorf("%w: %s", errSecureFileNotFound, fullPath)
		}
		return fullPath, fmt.Errorf("eval symlinks for request path: %w", err)
	}

	realDataDir, err := filepath.EvalSymlinks(absDataDir)
	if err != nil {
		return realPath, fmt.Errorf("eval symlinks for data dir: %w", err)
	}

	relPath, err := filepath.Rel(realDataDir, realPath)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
		return realPath, fmt.Errorf("%w: %s", errSecurePathEscape, realPath)
	}

	info, err := os.Stat(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			return realPath, fmt.Errorf("%w: %s", errSecureFileNotFound, realPath)
		}
		return realPath, fmt.Errorf("stat resolved path: %w", err)
	}
	if info.IsDir() {
		return realPath, fmt.Errorf("%w: %s", errSecureDirectoryPath, realPath)
	}

	return realPath, nil
}

func handleSecureFileResolveError(w http.ResponseWriter, requestPath, dataDir, resolvedPath string, err error, logger zerolog.Logger, metrics FileMetrics) {
	switch {
	case errors.Is(err, errSecureFileNotFound):
		logger.Info().Str("event", "file_req.not_found").Str("path", resolvedPath).Msg("file not found")
		metrics.Denied("not_found")
		http.Error(w, "Not found", http.StatusNotFound)
	case errors.Is(err, errSecurePathEscape):
		logger.Warn().
			Str("event", "file_req.denied").
			Str("path", requestPath).
			Str("resolved_path", resolvedPath).
			Str("data_dir", dataDir).
			Str("reason", "path_escape").
			Msg("path escapes data directory")
		metrics.Denied("path_escape")
		http.Error(w, "Forbidden", http.StatusForbidden)
	case errors.Is(err, errSecureDirectoryPath):
		logger.Warn().Str("event", "file_req.denied").Str("path", requestPath).Str("reason", "directory_listing").Msg("resolved path is a directory")
		metrics.Denied("directory_listing")
		http.Error(w, "Forbidden", http.StatusForbidden)
	default:
		logger.Error().Err(err).Str("event", "file_req.internal_error").Str("path", resolvedPath).Msg("could not resolve secure path")
		metrics.Denied("internal_error")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func serveSecureFileContent(w http.ResponseWriter, r *http.Request, realPath, requestPath string, logger zerolog.Logger, metrics FileMetrics) error {
	f, err := os.Open(realPath) // #nosec G304
	if err != nil {
		return fmt.Errorf("open resolved path: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			logger.Warn().Err(closeErr).Str("path", realPath).Msg("failed to close file")
		}
	}()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat opened file: %w", err)
	}

	etag := fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		metrics.CacheHit()
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	setSecureFileContentType(w, info.Name())

	logger.Info().Str("event", "file_req.allowed").Str("path", requestPath).Msg("serving file")
	metrics.Allowed()
	metrics.CacheMiss()
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return nil
}

func setSecureFileContentType(w http.ResponseWriter, filename string) {
	lowerName := strings.ToLower(filename)
	if strings.HasSuffix(lowerName, ".xml") {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		return
	}
	if strings.HasSuffix(lowerName, ".m3u") || strings.HasSuffix(lowerName, ".m3u8") {
		w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	}
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
