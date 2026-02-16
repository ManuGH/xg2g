// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package legacy

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

// HandlePicons proxies picon requests to the receiver and caches them locally.
func HandlePicons(w http.ResponseWriter, r *http.Request, runtime Runtime) {
	cfg := runtime.CurrentConfig()

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

	processRef := strings.ReplaceAll(ref, "_", ":")
	cacheRef := strings.ReplaceAll(processRef, ":", "_")

	piconDir, err := fsplat.ConfineRelPath(cfg.DataDir, "picons")
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

	localPath, err := fsplat.ConfineRelPath(cfg.DataDir, filepath.Join("picons", cacheRef+".png"))
	if err != nil {
		http.Error(w, "Invalid picon reference", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(localPath); err == nil {
		logger := log.WithComponentFromContext(r.Context(), "picon")
		logger.Info().Str("ref", ref).Msg("Serving from cache")
		http.ServeFile(w, r, localPath)
		return
	}

	upstreamBase := cfg.PiconBase
	if upstreamBase == "" {
		upstreamBase = cfg.Enigma2.BaseURL
	}
	if upstreamBase == "" {
		http.Error(w, "Picon backend not configured", http.StatusServiceUnavailable)
		return
	}

	upstreamURL := openwebif.PiconURL(upstreamBase, processRef)
	logger := log.WithComponentFromContext(r.Context(), "picon")

	if sem := runtime.PiconSemaphore(); sem != nil {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-r.Context().Done():
			return
		}
	}

	logger.Info().Str("ref", processRef).Str("upstream_url", upstreamURL).Msg("Picon: Downloading to cache")

	client := http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create picon request")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	resp, err := client.Do(req)
	if err != nil || (resp != nil && resp.StatusCode != http.StatusOK) {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.L().Debug().Msg("Internal: Upstream returned 404, attempting fallback logic")
		}
		if resp != nil {
			_ = resp.Body.Close()
			resp = nil
		}

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
				resp = respFallback
			} else {
				if respFallback != nil {
					_ = respFallback.Body.Close()
				}
				logger.Debug().Err(errFallback).Msg("SD picon fallback failed")
			}
		}

		if resp == nil || resp.StatusCode != http.StatusOK {
			if err != nil {
				logger.Warn().Err(err).Str("url", upstreamURL).Msg("upstream fetch failed")
				http.Error(w, "Picon upstream unavailable", http.StatusBadGateway)
				return
			}
			if resp != nil && resp.StatusCode != http.StatusNotFound {
				logger.Warn().Int("status", resp.StatusCode).Str("url", upstreamURL).Msg("upstream returned error")
				if resp.StatusCode >= 500 {
					http.Error(w, "Picon upstream error", http.StatusBadGateway)
				} else {
					http.NotFound(w, r)
				}
				return
			}

			logger.Debug().Str("url", upstreamURL).Msg("upstream returned 404 (picon not found)")
			http.NotFound(w, r)
			return
		}
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

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
		http.Error(w, "Picon transfer failed", http.StatusInternalServerError)
		return
	}
	_ = tempFile.Close()

	if err := os.Rename(tempFile.Name(), localPath); err != nil {
		logger.Error().Err(err).Msg("failed to rename temp picon file to cache")
		if _, statErr := os.Stat(tempFile.Name()); statErr == nil {
			http.ServeFile(w, r, tempFile.Name())
		} else {
			http.Error(w, "Failed to cache picon", http.StatusInternalServerError)
		}
		return
	}

	if err := os.Chmod(localPath, 0600); err != nil {
		logger.Warn().Err(err).Msg("failed to set picon file permissions")
	}

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
