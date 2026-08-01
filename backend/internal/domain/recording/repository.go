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
)

var (
	ErrJobNotFound     = errors.New("recording job manifest not found")
	ErrJobExists       = errors.New("recording job manifest already exists")
	ErrManifestCorrupt = errors.New("recording job manifest corrupted")
)

// JobRepository defines persistent CRUD operations for RecordingJobs.
type JobRepository interface {
	Save(ctx context.Context, job *RecordingJob) error
	Get(ctx context.Context, id string) (*RecordingJob, error)
	ListRecoverable(ctx context.Context) ([]*RecordingJob, error)
	Delete(ctx context.Context, id string) error
}

// DiskJobRepository implements JobRepository saving manifest.json per job under stagingRoot.
type DiskJobRepository struct {
	mu          sync.RWMutex
	stagingRoot string
}

// NewDiskJobRepository initializes a DiskJobRepository bound to stagingRoot.
func NewDiskJobRepository(stagingRoot string) (*DiskJobRepository, error) {
	if stagingRoot == "" {
		return nil, fmt.Errorf("stagingRoot cannot be empty")
	}
	if err := os.MkdirAll(stagingRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stagingRoot: %w", err)
	}
	return &DiskJobRepository{
		stagingRoot: stagingRoot,
	}, nil
}

// ManifestPath returns the absolute path to manifest.json for jobID.
func (r *DiskJobRepository) ManifestPath(jobID string) string {
	return filepath.Join(r.stagingRoot, "jobs", jobID, "manifest.json")
}

// Save atomically writes manifest.json with .tmp -> fsync -> rename.
func (r *DiskJobRepository) Save(ctx context.Context, job *RecordingJob) error {
	if err := ValidateJobID(job.ID); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	manifestFile := r.ManifestPath(job.ID)
	jobDir := filepath.Dir(manifestFile)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return fmt.Errorf("failed to create job dir: %w", err)
	}

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job manifest: %w", err)
	}

	tmpPath := manifestFile + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open tmp manifest file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tmp manifest: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync tmp manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close tmp manifest: %w", err)
	}

	if err := os.Rename(tmpPath, manifestFile); err != nil {
		return fmt.Errorf("failed to rename manifest: %w", err)
	}

	// Parent directory fsync to guarantee persistence across power outages
	if parentDir, err := os.Open(jobDir); err == nil {
		_ = parentDir.Sync()
		_ = parentDir.Close()
	}

	return nil
}

// Get reads and unmarshals manifest.json for jobID.
func (r *DiskJobRepository) Get(ctx context.Context, id string) (*RecordingJob, error) {
	if err := ValidateJobID(id); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	manifestFile := r.ManifestPath(id)
	data, err := os.ReadFile(manifestFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}

	var job RecordingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestCorrupt, err)
	}

	return &job, nil
}

// ListRecoverable inventories all active or non-terminal jobs requiring crash recovery after process restart.
func (r *DiskJobRepository) ListRecoverable(ctx context.Context) ([]*RecordingJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	jobsDir := filepath.Join(r.stagingRoot, "jobs")
	if _, err := os.Stat(jobsDir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read jobs dir: %w", err)
	}

	var recoverable []*RecordingJob
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()
		manifestFile := filepath.Join(jobsDir, jobID, "manifest.json")
		data, err := os.ReadFile(manifestFile)
		if err != nil {
			continue
		}

		var job RecordingJob
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}

		// Non-terminal jobs require crash recovery
		if !job.State.IsTerminal() {
			recoverable = append(recoverable, &job)
		}
	}

	return recoverable, nil
}

// Delete removes job directory from disk.
func (r *DiskJobRepository) Delete(ctx context.Context, id string) error {
	if err := ValidateJobID(id); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	manifestFile := r.ManifestPath(id)
	jobDir := filepath.Dir(manifestFile)
	return os.RemoveAll(jobDir)
}
