// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/recording"
)

// StagingManager manages local NVMe workspace directories for recording jobs.
type StagingManager struct {
	mu         sync.RWMutex
	stagingRoot string
}

// NewStagingManager initializes a StagingManager bound to stagingRoot.
func NewStagingManager(stagingRoot string) (*StagingManager, error) {
	if stagingRoot == "" {
		return nil, fmt.Errorf("stagingRoot cannot be empty")
	}
	if err := os.MkdirAll(stagingRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stagingRoot %s: %w", stagingRoot, err)
	}
	return &StagingManager{
		stagingRoot: stagingRoot,
	}, nil
}

// JobDir returns the dedicated local staging workspace directory for a job.
func (sm *StagingManager) JobDir(jobID string) string {
	return filepath.Join(sm.stagingRoot, "jobs", jobID)
}

// PrepareWorkspace creates a clean local staging directory for a job.
func (sm *StagingManager) PrepareWorkspace(job *recording.RecordingJob) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	dir := sm.JobDir(job.ID)
	_ = os.RemoveAll(dir) // Clean old draft if any
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create job staging workspace: %w", err)
	}

	job.LocalStagingPath = dir
	return dir, nil
}

// AssembleSegments concatenates ordered segment files in job workspace into a single finalized .ts file.
func (sm *StagingManager) AssembleSegments(jobID string, outputFilename string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	dir := sm.JobDir(jobID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read staging dir %s: %w", dir, err)
	}

	var segPaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "seg_") && strings.HasSuffix(name, ".ts") {
			segPaths = append(segPaths, filepath.Join(dir, name))
		}
	}

	if len(segPaths) == 0 {
		return "", fmt.Errorf("no segment files found in staging dir %s", dir)
	}

	// Sort segments in numeric sequence order
	sort.Slice(segPaths, func(i, j int) bool {
		var seqI, seqJ uint64
		_, _ = fmt.Sscanf(filepath.Base(segPaths[i]), "seg_%d.ts", &seqI)
		_, _ = fmt.Sscanf(filepath.Base(segPaths[j]), "seg_%d.ts", &seqJ)
		return seqI < seqJ
	})

	finalPath := filepath.Join(dir, outputFilename)
	tmpPath := finalPath + ".tmp"

	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create final output file: %w", err)
	}

	for _, segPath := range segPaths {
		in, err := os.Open(segPath)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("failed to open segment %s: %w", segPath, err)
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = in.Close()
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("failed to copy segment %s: %w", segPath, err)
		}
		_ = in.Close()
	}

	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to sync final output file: %w", err)
	}
	_ = out.Close()

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("failed to rename finalized file: %w", err)
	}

	return finalPath, nil
}

// CleanupWorkspace removes a job's staging directory after successful commit.
func (sm *StagingManager) CleanupWorkspace(jobID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	dir := sm.JobDir(jobID)
	return os.RemoveAll(dir)
}
