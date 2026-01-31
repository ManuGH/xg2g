package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	fsplat "github.com/ManuGH/xg2g/internal/platform/fs"
	"github.com/go-chi/chi/v5"
)

// handlePicons proxies picon requests to the backend receiver and caches them locally
// Path: /logos/{ref}.png
func (s *Server) handlePicons(w http.ResponseWriter, r *http.Request) {
	rawRef := chi.URLParam(r, "ref")
	if rawRef == "" {
		http.Error(w, "Missing picon reference", http.StatusBadRequest)
		return
	}

	ref, err := parsePiconRef(rawRef)
	if err != nil {
		http.Error(w, "Invalid picon reference", http.StatusBadRequest)
		return
	}

	// normalizeRef is used for Upstream requests (needs colons usually)
	// cacheRef is used for Local Filesystem (needs underscores for safety)

	// Ensure we have a "Colon-style" ref for logical processing / upstream
	processRef := strings.ReplaceAll(ref, "_", ":")

	// Ensure we have an "Underscore-style" ref for filesystem
	cacheRef := strings.ReplaceAll(processRef, ":", "_")

	// Local Cache Path
	piconDir, err := fsplat.ConfineRelPath(s.cfg.DataDir, "picons")
	if err != nil {
		log.L().Error().Err(err).Msg("failed to confine picon cache dir")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(piconDir, 0750); err != nil {
		log.L().Error().Err(err).Msg("failed to create picon cache dir")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	localPath, err := fsplat.ConfineRelPath(s.cfg.DataDir, filepath.Join("picons", cacheRef+".png"))
	if err != nil {
		http.Error(w, "Invalid picon reference", http.StatusBadRequest)
		return
	}

	// 1. CACHE HIT
	if _, err := os.Stat(localPath); err == nil {
		logger := log.WithComponentFromContext(r.Context(), "picon")
		logger.Info().Str("ref", ref).Msg("Serving from cache")
		http.ServeFile(w, r, localPath)
		return
	}

	// 2. CACHE MISS -> Download
	upstreamBase := s.cfg.PiconBase
	if upstreamBase == "" {
		upstreamBase = s.cfg.Enigma2.BaseURL
	}
	if upstreamBase == "" {
		http.Error(w, "Picon backend not configured", http.StatusServiceUnavailable)
		return
	}

	// Use processRef (Colons) for upstream URL generation as Enigma2 expects colons or underscores depending on config
	// Usually PiconURL converts to underscores internally, but let's be safe.
	// Actually openwebif.PiconURL *already* converts to underscores!
	// So passing processRef (colons) is fine.
	upstreamURL := openwebif.PiconURL(upstreamBase, processRef)
	logger := log.WithComponentFromContext(r.Context(), "picon")

	// Acquire semaphore to protect upstream limit
	select {
	case s.piconSemaphore <- struct{}{}:
		defer func() { <-s.piconSemaphore }()
	case <-r.Context().Done():
		return // Client gave up
	}

	logger.Info().Str("ref", processRef).Str("upstream_url", upstreamURL).Msg("Picon: Downloading to cache")

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	// Use request context so client disconnect cancels the download
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create picon request")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	resp, err := client.Do(req)

	// Fallback Logic
	// Enter fallback/error handling if: Request failed OR Status is not OK (e.g. 404, 500, 403)
	if err != nil || (resp != nil && resp.StatusCode != http.StatusOK) {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			// It's a 404, we might try fallback
			log.L().Debug().Msg("Internal: Upstream returned 404, attempting fallback logic")
		}
		// It's 500, 403, etc.
		if resp != nil {
			_ = resp.Body.Close()
			resp = nil // Prevent double-close
		}

		// Normalize processRef (HD->SD fallback)
		// e.g. 1:0:19... -> 1:0:1...
		normalizedRef := openwebif.NormalizeServiceRefForPicon(processRef)
		if normalizedRef != processRef {
			fallbackURL := openwebif.PiconURL(upstreamBase, normalizedRef)
			logger.Info().
				Str("original_ref", processRef).
				Str("normalized_ref", normalizedRef).
				Str("fallback_url", fallbackURL).
				Msg("Picon: attempting fallback to SD picon")

			fallbackReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, fallbackURL, nil)
			respFallback, errFallback := client.Do(fallbackReq)
			if errFallback == nil && respFallback.StatusCode == http.StatusOK {
				// Success! Use the fallback response
				resp = respFallback
			} else {
				// Fallback failed
				if respFallback != nil {
					_ = respFallback.Body.Close()
				}
				logger.Debug().Err(errFallback).Msg("SD picon fallback failed")
			}
		}

		// If we still don't have a valid response, return error
		if resp == nil || resp.StatusCode != http.StatusOK {
			if err != nil {
				logger.Warn().Err(err).Str("url", upstreamURL).Msg("upstream fetch failed")
				http.Error(w, "Picon upstream unavailable", http.StatusBadGateway)
				return
			} else if resp != nil && resp.StatusCode != http.StatusNotFound {
				logger.Warn().Int("status", resp.StatusCode).Str("url", upstreamURL).Msg("upstream returned error")
				// Pass through 5xx errors from upstream
				if resp.StatusCode >= 500 {
					http.Error(w, "Picon upstream error", http.StatusBadGateway)
				} else {
					http.NotFound(w, r)
				}
				return
			} else {
				logger.Debug().Str("url", upstreamURL).Msg("upstream returned 404 (picon not found)")
			}

			http.NotFound(w, r)
			return
		}
	}

	// Ensure response body is always closed
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	// 3. SAVE TO CACHE
	tempFile, err := os.CreateTemp(piconDir, "picon-*.tmp")
	if err != nil {
		logger.Warn().Err(err).Msg("skipping picon cache (write failed)")
		_, _ = io.Copy(w, resp.Body)
		return
	}
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
	}()

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		logger.Warn().Err(err).Msg("skipping picon cache (copy failed)")
		// If write fails, we can't rewind the body easily if it's a stream,
		// but typically we can't save it. However, the client needs the data.
		// Since we wrote to tempFile, we failed mid-stream.
		// Actually, we should io.TeeReader if we want to be robust, or just buffer it.
		// But for picons (small), copy to temp, then serve file is safer.
		// If copy fails, we abort.
		http.Error(w, "Picon transfer failed", http.StatusInternalServerError)
		return
	}
	_ = tempFile.Close() // Close before rename on Windows

	if err := os.Rename(tempFile.Name(), localPath); err != nil {
		logger.Error().Err(err).Msg("failed to rename temp picon file to cache")
		// If rename fails, serve from the temp file if it still exists
		if _, statErr := os.Stat(tempFile.Name()); statErr == nil {
			http.ServeFile(w, r, tempFile.Name())
		} else {
			http.Error(w, "Failed to cache picon", http.StatusInternalServerError)
		}
		return
	}

	// Fix permissions so file can be read by http.ServeFile
	if err := os.Chmod(localPath, 0600); err != nil {
		logger.Warn().Err(err).Msg("failed to set picon file permissions")
	}

	// 4. SERVE
	http.ServeFile(w, r, localPath)
}

func parsePiconRef(raw string) (string, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", err
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", fmt.Errorf("empty ref")
	}
	if strings.Contains(decoded, "/") || strings.Contains(decoded, "\\") {
		return "", fmt.Errorf("path separator not allowed")
	}
	if strings.Contains(decoded, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("control characters not allowed")
		}
	}
	return decoded, nil
}
