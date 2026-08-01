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
	ErrProfileNotFound = errors.New("recording profile not found")
)

// ProfileRepository defines persistent CRUD operations for RecordingProfiles.
type ProfileRepository interface {
	Save(ctx context.Context, profile *RecordingProfile) error
	Get(ctx context.Context, id string) (*RecordingProfile, error)
	List(ctx context.Context) ([]*RecordingProfile, error)
	Delete(ctx context.Context, id string) error
}

// DiskProfileRepository implements ProfileRepository using profiles.json.
type DiskProfileRepository struct {
	mu          sync.RWMutex
	storagePath string
}

// NewDiskProfileRepository initializes DiskProfileRepository storing profiles at storagePath.
func NewDiskProfileRepository(storagePath string) (*DiskProfileRepository, error) {
	if storagePath == "" {
		return nil, fmt.Errorf("storagePath cannot be empty")
	}
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for profile repository: %w", err)
	}
	return &DiskProfileRepository{
		storagePath: storagePath,
	}, nil
}

// Save persists profile into profiles.json atomically with deep copy isolation.
func (r *DiskProfileRepository) Save(ctx context.Context, profile *RecordingProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if profile == nil || profile.ID == "" {
		return fmt.Errorf("profile or profile ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	profiles, err := r.loadLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if profiles == nil {
		profiles = make(map[string]*RecordingProfile)
	}

	// Deep clone profile before storing
	profCopy := *profile
	profiles[profCopy.ID] = &profCopy

	return r.saveLocked(profiles)
}

// Get fetches a deep copy of profile by id.
func (r *DiskProfileRepository) Get(ctx context.Context, id string) (*RecordingProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles, err := r.loadLocked()
	if err != nil {
		return nil, err
	}

	profile, ok := profiles[id]
	if !ok {
		return nil, ErrProfileNotFound
	}
	cp := *profile
	return &cp, nil
}

// List returns deep copies of all active RecordingProfiles.
func (r *DiskProfileRepository) List(ctx context.Context) ([]*RecordingProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	profilesMap, err := r.loadLocked()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []*RecordingProfile
	for _, p := range profilesMap {
		cp := *p
		list = append(list, &cp)
	}
	return list, nil
}

// Delete removes a profile by id.
func (r *DiskProfileRepository) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	profiles, err := r.loadLocked()
	if err != nil {
		return err
	}

	if _, ok := profiles[id]; !ok {
		return ErrProfileNotFound
	}

	delete(profiles, id)
	return r.saveLocked(profiles)
}

func (r *DiskProfileRepository) loadLocked() (map[string]*RecordingProfile, error) {
	data, err := os.ReadFile(r.storagePath)
	if err != nil {
		return nil, err
	}

	var profiles map[string]*RecordingProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profiles: %w", err)
	}
	return profiles, nil
}

func (r *DiskProfileRepository) saveLocked(profiles map[string]*RecordingProfile) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := r.storagePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open tmp profile file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tmp profile file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to fsync tmp profile file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close tmp profile file cleanly: %w", err)
	}

	if err := os.Rename(tmpPath, r.storagePath); err != nil {
		return fmt.Errorf("failed to rename profile repository file: %w", err)
	}

	// Parent directory fsync with explicit error propagation
	dirPath := filepath.Dir(r.storagePath)
	pDir, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("failed to open profile repository directory for fsync: %w", err)
	}
	if err := pDir.Sync(); err != nil {
		_ = pDir.Close()
		return fmt.Errorf("failed to fsync profile repository parent directory: %w", err)
	}
	_ = pDir.Close()

	return nil
}
