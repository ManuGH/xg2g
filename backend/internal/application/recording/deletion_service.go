// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"context"
	"errors"
	"fmt"

	"github.com/ManuGH/xg2g/internal/domain/recording"
	"github.com/ManuGH/xg2g/internal/infra/storage"
)

var (
	ErrBackendOfflineForDeletion = errors.New("storage backend is offline or unregistered; physical file deletion required by policy cannot be completed")
)

// AssetDeletionService coordinates physical file removal and metadata deletion across application boundaries.
type AssetDeletionService struct {
	assetRepo recording.AssetRepository
	backends  map[string]storage.StorageBackend
}

// NewAssetDeletionService initializes AssetDeletionService.
func NewAssetDeletionService(assetRepo recording.AssetRepository, backends []storage.StorageBackend) *AssetDeletionService {
	backendMap := make(map[string]storage.StorageBackend)
	for _, b := range backends {
		if b != nil {
			backendMap[b.ID()] = b
		}
	}
	return &AssetDeletionService{
		assetRepo: assetRepo,
		backends:  backendMap,
	}
}

// DeleteAsset evaluates asset profile snapshot rules and executes physical file deletion before metadata removal.
func (s *AssetDeletionService) DeleteAsset(ctx context.Context, assetID string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if assetID == "" {
		return fmt.Errorf("assetID cannot be empty")
	}

	asset, err := s.assetRepo.Get(ctx, assetID)
	if err != nil {
		return err
	}

	// 1. Evaluate pure domain policy using asset's embedded snapshot
	errPolicy := recording.CanDeletePhysicalFile(asset.ManagementMode, asset.DeletePolicy, force)
	requiresPhysicalDelete := (errPolicy == nil || (asset.DeletePolicy == recording.DeleteAssetAndFile))

	// 2. Resolve Backend
	backend, backendAvailable := s.backends[asset.BackendID]

	// SAFETY GUARD: If physical deletion is required by policy but backend is offline/unregistered, REFUSE METADATA DELETION!
	if requiresPhysicalDelete && (!backendAvailable || backend == nil) {
		return fmt.Errorf("%w: backend '%s' unavailable for asset '%s'", ErrBackendOfflineForDeletion, asset.BackendID, asset.ID)
	}

	// 3. Physical File Deletion (if policy permits or force requested)
	if (errPolicy == nil || force) && backendAvailable && backend != nil {
		if err := backend.DeleteFile(ctx, asset.ObjectKey); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
			return fmt.Errorf("failed to delete physical file on backend '%s': %w", asset.BackendID, err)
		}

		// Verify removal via Stat
		_, statErr := backend.Stat(ctx, asset.ObjectKey)
		if statErr != nil {
			if !errors.Is(statErr, storage.ErrObjectNotFound) {
				return fmt.Errorf("failed to verify physical file deletion via stat: %w", statErr)
			}
			// Confirmed deleted (ErrObjectNotFound)
		} else {
			return fmt.Errorf("physical file still exists on backend '%s' after deletion", asset.BackendID)
		}
	}

	// 4. Remove metadata from AssetRepository ONLY after physical file deletion is verified
	return s.assetRepo.Delete(ctx, assetID)
}
