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

// InventoryIssue captures explicit manifest corruption or read errors during inventory scanning.
type InventoryIssue struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// JobInventory returns all successfully read jobs alongside any manifest issues encountered.
type JobInventory struct {
	Jobs   []*RecordingJob  `json:"jobs"`
	Issues []InventoryIssue `json:"issues"`
}

// JobRepository defines persistent CRUD operations for RecordingJobs.
type JobRepository interface {
	Save(ctx context.Context, job *RecordingJob) error
	Get(ctx context.Context, id string) (*RecordingJob, error)
	ListAllInventory(ctx context.Context) (JobInventory, error)
	ListRecoverable(ctx context.Context) ([]*RecordingJob, error)
	Delete(ctx context.Context, id string) error
}

// DiskJobRepository implements JobRepository saving job_manifest.json per job under stagingRoot.
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

// ManifestPath returns the absolute path to job_manifest.json for jobID.
func (r *DiskJobRepository) ManifestPath(jobID string) string {
	return filepath.Join(r.stagingRoot, "jobs", jobID, "job_manifest.json")
}

// LegacyManifestPath returns the absolute path to legacy manifest.json for jobID.
func (r *DiskJobRepository) LegacyManifestPath(jobID string) string {
	return filepath.Join(r.stagingRoot, "jobs", jobID, "manifest.json")
}

// Save atomically writes job_manifest.json with .tmp -> fsync -> rename.
func (r *DiskJobRepository) Save(ctx context.Context, job *RecordingJob) error {
	if job == nil {
		return fmt.Errorf("cannot save nil RecordingJob")
	}
	if err := ValidateJobID(job.ID); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	manifestFile := r.ManifestPath(job.ID)
	jobDir := filepath.Dir(manifestFile)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return fmt.Errorf("failed to create job directory: %w", err)
	}

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job manifest: %w", err)
	}

	tmpFile := manifestFile + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open tmp manifest file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to write tmp manifest file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to fsync tmp manifest file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to close tmp manifest file cleanly: %w", err)
	}

	if err := os.Rename(tmpFile, manifestFile); err != nil {
		return fmt.Errorf("failed to rename manifest file: %w", err)
	}

	pDir, err := os.Open(jobDir)
	if err != nil {
		return fmt.Errorf("failed to open job directory for fsync: %w", err)
	}
	if err := pDir.Sync(); err != nil {
		_ = pDir.Close()
		return fmt.Errorf("failed to fsync job directory: %w", err)
	}
	_ = pDir.Close()

	return nil
}

// Get reads and parses job_manifest.json (or legacy manifest.json) for jobID.
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
			// Fallback to legacy manifest.json
			legacyFile := r.LegacyManifestPath(id)
			data, err = os.ReadFile(legacyFile)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, ErrJobNotFound
				}
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	var job RecordingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestCorrupt, err)
	}
	if job.ID == "" {
		return nil, fmt.Errorf("%w: missing job ID in manifest", ErrManifestCorrupt)
	}

	return &job, nil
}

// ListAllInventory scans and returns all RecordingJobs on disk alongside explicit manifest corruption issues.
func (r *DiskJobRepository) ListAllInventory(ctx context.Context) (JobInventory, error) {
	if err := ctx.Err(); err != nil {
		return JobInventory{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var inventory JobInventory
	jobsDir := filepath.Join(r.stagingRoot, "jobs")
	if _, err := os.Stat(jobsDir); os.IsNotExist(err) {
		return inventory, nil
	}

	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return inventory, fmt.Errorf("failed to read jobs dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()
		jobDir := filepath.Join(jobsDir, jobID)
		manifestFile := filepath.Join(jobDir, "job_manifest.json")
		data, err := os.ReadFile(manifestFile)
		if err != nil {
			if os.IsNotExist(err) {
				// Fallback to legacy manifest.json
				legacyFile := filepath.Join(jobDir, "manifest.json")
				data, err = os.ReadFile(legacyFile)
				if err != nil {
					// Job directory exists but job manifest is missing!
					inventory.Issues = append(inventory.Issues, InventoryIssue{
						Path:  jobDir,
						Error: "job manifest missing (neither job_manifest.json nor manifest.json found)",
					})
					continue
				}
			} else {
				inventory.Issues = append(inventory.Issues, InventoryIssue{Path: manifestFile, Error: err.Error()})
				continue
			}
		}

		var job RecordingJob
		if err := json.Unmarshal(data, &job); err != nil {
			inventory.Issues = append(inventory.Issues, InventoryIssue{Path: manifestFile, Error: fmt.Sprintf("%v: %v", ErrManifestCorrupt, err)})
			continue
		}
		if job.ID == "" {
			inventory.Issues = append(inventory.Issues, InventoryIssue{Path: manifestFile, Error: fmt.Sprintf("%v: missing job ID", ErrManifestCorrupt)})
			continue
		}

		allJob := job
		inventory.Jobs = append(inventory.Jobs, &allJob)
	}

	return inventory, nil
}

// ListRecoverable scans and returns all non-terminal RecordingJobs requiring recovery.
func (r *DiskJobRepository) ListRecoverable(ctx context.Context) ([]*RecordingJob, error) {
	inventory, err := r.ListAllInventory(ctx)
	if err != nil {
		return nil, err
	}

	var recoverable []*RecordingJob
	for _, job := range inventory.Jobs {
		if !job.State.IsTerminal() {
			recoverable = append(recoverable, job)
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
