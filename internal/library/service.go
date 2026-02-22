// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package library

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// Service provides business logic for library operations.
type Service struct {
	configs []RootConfig
	store   *Store
	scanner *Scanner

	// Per P0+ Gate #2: Deterministic 503 singleflight
	// Tracks active scans per root
	activeScans sync.Map // map[string]*sync.Mutex
}

// NewService creates a new library service.
func NewService(configs []RootConfig, store *Store) *Service {
	scanner := NewScanner(store)

	svc := &Service{
		configs: configs,
		store:   store,
		scanner: scanner,
	}

	// Initialize roots in DB
	ctx := context.Background()
	for _, cfg := range configs {
		if err := store.UpsertRoot(ctx, cfg.ID, cfg.Type); err != nil {
			log.L().Error().Err(err).Str("root_id", cfg.ID).Msg("failed to initialize library root")
		}
	}

	return svc
}

// GetRoots returns all library roots with current status.
// Per P0+ Gate #2: This endpoint NEVER blocks.
func (s *Service) GetRoots(ctx context.Context) ([]Root, error) {
	return s.store.GetRoots(ctx)
}

// GetRootItems returns paginated items for a library root.
// Per P0+ Gate #2: Triggers scan if status=never, returns 503 if scan running.
func (s *Service) GetRootItems(ctx context.Context, rootID string, limit, offset int) ([]Item, int, error) {
	// Get root status
	root, err := s.store.GetRoot(ctx, rootID)
	if err != nil {
		return nil, 0, fmt.Errorf("get root: %w", err)
	}
	if root == nil {
		return nil, 0, fmt.Errorf("%w: %s", ErrRootNotFound, rootID)
	}

	// Check if scan is running
	if root.LastScanStatus == RootStatusRunning {
		return nil, 0, ErrScanRunning
	}

	// Trigger scan if never scanned
	if root.LastScanStatus == RootStatusNever {
		if err := s.TriggerScan(ctx, rootID); err != nil {
			if err == ErrScanRunning {
				return nil, 0, err
			}
			return nil, 0, fmt.Errorf("trigger scan: %w", err)
		}
		// After successful scan, fetch items
	}

	// Return items
	return s.store.GetItems(ctx, rootID, limit, offset)
}

// TriggerScan triggers an on-demand scan for a root.
// Per P0+ Gate #2: Returns ErrScanRunning if scan already in progress.
func (s *Service) TriggerScan(ctx context.Context, rootID string) error {
	// Get root config
	var cfg *RootConfig
	for _, c := range s.configs {
		if c.ID == rootID {
			cfg = &c
			break
		}
	}
	if cfg == nil {
		return fmt.Errorf("root config not found: %s", rootID)
	}

	// Acquire scan lock (singleflight)
	lockKey := "scan:" + rootID
	mu, _ := s.activeScans.LoadOrStore(lockKey, &sync.Mutex{})
	scanMu := mu.(*sync.Mutex)

	// Try to acquire lock (non-blocking)
	if !scanMu.TryLock() {
		// Scan already running
		return ErrScanRunning
	}
	defer func() {
		scanMu.Unlock()
		s.activeScans.Delete(lockKey)
	}()

	// Mark scan as running
	scanStartTime := time.Now()
	if err := s.store.UpdateRootScanStatus(ctx, rootID, RootStatusRunning, scanStartTime, 0); err != nil {
		return fmt.Errorf("mark scan running: %w", err)
	}

	// Perform scan
	result, err := s.scanner.ScanRoot(ctx, *cfg)
	if err != nil {
		log.L().Error().
			Err(err).
			Str("root_id", rootID).
			Int("errors", result.ErrorCount).
			Msg("library scan failed")
	}

	// Update root status
	if err := s.store.UpdateRootScanStatus(ctx, rootID, result.FinalStatus, result.Finished, result.TotalScanned); err != nil {
		return fmt.Errorf("update scan status: %w", err)
	}

	log.L().Info().
		Str("root_id", rootID).
		Str("status", result.FinalStatus.String()).
		Int("scanned", result.TotalScanned).
		Int("errors", result.ErrorCount).
		Dur("duration", result.Finished.Sub(result.Started)).
		Msg("library scan complete")

	return nil
}

// GetStore returns the underlying persistence store.
func (s *Service) GetStore() *Store {
	return s.store
}

// GetConfigs returns the root configurations.
func (s *Service) GetConfigs() []RootConfig {
	return s.configs
}

var (
	// ErrRootNotFound signals that a requested library root does not exist.
	ErrRootNotFound = errors.New("root not found")
	// ErrScanRunning is returned when a scan is already in progress.
	// Per P0+ Gate #2: This triggers a 503 response with Retry-After header.
	ErrScanRunning = errors.New("scan already running")
)
