// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrAssetNotFound    = errors.New("recording asset not found")
	ErrAssetConcurrency = errors.New("optimistic concurrency check failed for asset")
)

// AssetRepository defines persistent CRUD operations for RecordingAssets with optimistic concurrency.
type AssetRepository interface {
	Save(ctx context.Context, asset *RecordingAsset, expectedVersion uint64) error
	Get(ctx context.Context, id string) (*RecordingAsset, error)
	List(ctx context.Context) ([]*RecordingAsset, error)
	Delete(ctx context.Context, id string) error
}

// DiskAssetRepository implements AssetRepository storing assets in assets.json.
type DiskAssetRepository struct {
	mu          sync.RWMutex
	storagePath string
}

// NewDiskAssetRepository initializes DiskAssetRepository storing assets at storagePath.
func NewDiskAssetRepository(storagePath string) (*DiskAssetRepository, error) {
	if storagePath == "" {
		return nil, fmt.Errorf("storagePath cannot be empty")
	}
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for asset repository: %w", err)
	}
	return &DiskAssetRepository{
		storagePath: storagePath,
	}, nil
}

// Save persists asset into assets.json with strict expectedVersion verification.
func (r *DiskAssetRepository) Save(ctx context.Context, asset *RecordingAsset, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if asset == nil || asset.ID == "" {
		return fmt.Errorf("asset or asset ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	assets, err := r.loadLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if assets == nil {
		assets = make(map[string]*RecordingAsset)
	}

	existing, ok := assets[asset.ID]
	if ok {
		if existing.Version != expectedVersion {
			return fmt.Errorf("%w: existing version %d != expected version %d", ErrAssetConcurrency, existing.Version, expectedVersion)
		}
	} else if expectedVersion != 0 {
		return fmt.Errorf("%w: new asset requires expectedVersion 0, got %d", ErrAssetConcurrency, expectedVersion)
	}

	// Deep clone asset before storing
	assetCopy := asset.Clone()
	assetCopy.Version = expectedVersion + 1
	assetCopy.UpdatedAt = time.Now()

	assets[assetCopy.ID] = assetCopy

	if err := r.saveLocked(assets); err != nil {
		return err
	}

	// Update caller's instance upon successful persist
	asset.Version = assetCopy.Version
	asset.UpdatedAt = assetCopy.UpdatedAt

	return nil
}

// Get fetches a deep clone of asset by id.
func (r *DiskAssetRepository) Get(ctx context.Context, id string) (*RecordingAsset, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	assets, err := r.loadLocked()
	if err != nil {
		return nil, err
	}

	asset, ok := assets[id]
	if !ok {
		return nil, ErrAssetNotFound
	}
	return asset.Clone(), nil
}

// List returns deep clones of all active RecordingAssets.
func (r *DiskAssetRepository) List(ctx context.Context) ([]*RecordingAsset, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	assetsMap, err := r.loadLocked()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []*RecordingAsset
	for _, a := range assetsMap {
		list = append(list, a.Clone())
	}
	return list, nil
}

// Delete removes an asset by id.
func (r *DiskAssetRepository) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	assets, err := r.loadLocked()
	if err != nil {
		return err
	}

	if _, ok := assets[id]; !ok {
		return ErrAssetNotFound
	}

	delete(assets, id)
	return r.saveLocked(assets)
}

func (r *DiskAssetRepository) loadLocked() (map[string]*RecordingAsset, error) {
	data, err := os.ReadFile(r.storagePath)
	if err != nil {
		return nil, err
	}

	var assets map[string]*RecordingAsset
	if err := json.Unmarshal(data, &assets); err != nil {
		return nil, fmt.Errorf("failed to unmarshal assets: %w", err)
	}
	return assets, nil
}

func (r *DiskAssetRepository) saveLocked(assets map[string]*RecordingAsset) error {
	data, err := json.MarshalIndent(assets, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := r.storagePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open tmp asset file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tmp asset file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to fsync tmp asset file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close tmp asset file cleanly: %w", err)
	}

	if err := os.Rename(tmpPath, r.storagePath); err != nil {
		return fmt.Errorf("failed to rename asset repository file: %w", err)
	}

	// Parent directory fsync with explicit error propagation
	dirPath := filepath.Dir(r.storagePath)
	pDir, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("failed to open asset repository directory for fsync: %w", err)
	}
	if err := pDir.Sync(); err != nil {
		_ = pDir.Close()
		return fmt.Errorf("failed to fsync asset repository parent directory: %w", err)
	}
	_ = pDir.Close()

	return nil
}
