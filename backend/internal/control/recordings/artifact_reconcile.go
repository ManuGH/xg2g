package recordings

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ManuGH/xg2g/internal/control/vod"
)

type recordingArtifactManager interface {
	Get(ctx context.Context, dir string) (*vod.JobStatus, bool)
	GetMetadata(id string) (vod.Metadata, bool)
	MarkProbed(id string, resolvedPath string, info *vod.StreamInfo, fp *vod.Fingerprint)
}

type recordingArtifactFailurePromoter interface {
	PromoteFailedToReadyIfPlaylist(id string) (vod.Metadata, bool)
}

func recordingFinalPlaylistPath(hlsRoot, serviceRef, variant string) (string, error) {
	cacheDir, err := RecordingVariantCacheDir(hlsRoot, serviceRef, variant)
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "index.m3u8"), nil
}

// ReconcileRecordingPlaylistMetadata rehydrates READY metadata from a canonical final playlist on disk.
func ReconcileRecordingPlaylistMetadata(hlsRoot string, manager recordingArtifactManager, serviceRef, variant string) (vod.Metadata, bool, error) {
	if manager == nil {
		return vod.Metadata{}, false, nil
	}

	finalPath, err := recordingFinalPlaylistPath(hlsRoot, serviceRef, variant)
	if err != nil {
		return vod.Metadata{}, false, err
	}

	info, statErr := os.Stat(finalPath)
	if statErr != nil || info.IsDir() {
		return vod.Metadata{}, false, nil
	}

	metaID := RecordingVariantMetadataKey(serviceRef, variant)
	manager.MarkProbed(metaID, finalPath, nil, nil)
	meta, ok := manager.GetMetadata(metaID)
	return meta, ok, nil
}

// LoadRecordingBuildState returns the current build/job view and rehydrates READY playlist metadata from disk when needed.
func LoadRecordingBuildState(ctx context.Context, hlsRoot string, manager recordingArtifactManager, serviceRef, variant string) (*vod.JobStatus, bool, vod.Metadata, bool, error) {
	cacheDir, err := RecordingVariantCacheDir(hlsRoot, serviceRef, variant)
	if err != nil {
		return nil, false, vod.Metadata{}, false, err
	}

	job, jobOk := manager.Get(ctx, cacheDir)
	metaID := RecordingVariantMetadataKey(serviceRef, variant)
	meta, metaOk := manager.GetMetadata(metaID)

	if metaOk && meta.State == vod.ArtifactStateFailed && meta.HasPlaylist() {
		if promoter, ok := manager.(recordingArtifactFailurePromoter); ok {
			if promoted, changed := promoter.PromoteFailedToReadyIfPlaylist(metaID); changed {
				meta = promoted
			}
		}
	}

	if !metaOk || (!meta.HasPlaylist() && meta.State != vod.ArtifactStateReady) {
		if repaired, ok, repairErr := ReconcileRecordingPlaylistMetadata(hlsRoot, manager, serviceRef, variant); repairErr != nil {
			return nil, false, vod.Metadata{}, false, repairErr
		} else if ok {
			meta = repaired
			metaOk = true
		}
	}

	return job, jobOk, meta, metaOk, nil
}
