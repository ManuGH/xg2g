package v3

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// LibraryService returns the underlying library service.
func (s *Server) LibraryService() *library.Service {
	return s.libraryService
}

// authMiddleware is the default authentication middleware.
func (s *Server) authMiddleware(h http.Handler) http.Handler {
	if s.AuthMiddlewareOverride != nil {
		return s.AuthMiddlewareOverride(h)
	}
	return s.authMiddlewareImpl(h)
}

// AuthMiddleware exposes the canonical v3 authentication middleware.
func (s *Server) AuthMiddleware(h http.Handler) http.Handler {
	return s.authMiddleware(h)
}

// GetConfig returns a copy of the current config.
func (s *Server) GetConfig() config.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// GetStatus returns the current status.
func (s *Server) GetStatus() jobs.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Server) dataFilePath(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("data file path must be relative: %s", rel)
	}
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("data file path contains traversal: %s", rel)
	}

	s.mu.RLock()
	dataDir := s.cfg.DataDir
	s.mu.RUnlock()

	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}

	full := filepath.Join(root, clean)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = root
	}

	resolved := full
	//nolint:gosec // G703: path is strictly sanitized and bounded to configured DataDir
	if info, statErr := os.Stat(full); statErr == nil {
		if info.IsDir() {
			return "", fmt.Errorf("data file path points to directory: %s", rel)
		}
		if resolvedPath, evalErr := filepath.EvalSymlinks(full); evalErr == nil {
			resolved = resolvedPath
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat data file: %w", statErr)
	} else {
		// File might be generated later; still ensure parent directories stay within root.
		dir := filepath.Dir(full)
		//nolint:gosec // G703: path is strictly sanitized and bounded to configured DataDir
		if _, dirErr := os.Stat(dir); dirErr == nil {
			if realDir, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
				resolved = filepath.Join(realDir, filepath.Base(full))
			}
		}
	}

	relToRoot, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	if strings.HasPrefix(relToRoot, "..") || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("data file escapes data directory: %s", rel)
	}

	return resolved, nil
}

// owi returns a ReceiverControl, using the injected factory if present (tests)
// or falling back to the cached production client.
func (s *Server) owi(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
	if s.owiFactory != nil {
		return s.owiFactory(cfg, snap)
	}
	return s.newOpenWebIFClient(cfg, snap)
}

// newOpenWebIFClient gets or creates a cached client from config
func (s *Server) newOpenWebIFClient(cfg config.AppConfig, snap config.Snapshot) *openwebif.Client {
	// 1. Fast path: Read lock check
	s.mu.RLock()
	cachedClient := s.owiClient
	cachedEpoch := s.owiEpoch
	s.mu.RUnlock()

	// If cached match, assume safe to use (Client is thread-safe)
	if cachedClient != nil && cachedEpoch == snap.Epoch {
		return cachedClient
	}

	// 2. Slow path: Write lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double check
	if s.owiClient != nil && s.owiEpoch == snap.Epoch {
		return s.owiClient
	}

	// Rebuild
	log.L().Debug().Uint64("epoch", snap.Epoch).Msg("recreating OpenWebIF client")
	client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, openwebif.Options{
		Timeout:             cfg.Enigma2.Timeout,
		Username:            cfg.Enigma2.Username,
		Password:            cfg.Enigma2.Password,
		UseWebIFStreams:     cfg.Enigma2.UseWebIFStreams,
		StreamBaseURL:       snap.Runtime.OpenWebIF.StreamBaseURL,
		HTTPMaxConnsPerHost: snap.Runtime.OpenWebIF.HTTPMaxConnsPerHost,
	})

	s.owiClient = client
	s.owiEpoch = snap.Epoch

	return client
}
