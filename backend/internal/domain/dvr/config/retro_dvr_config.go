// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidWindowSeconds         = errors.New("selected window seconds must be greater than 0")
	ErrWindowExceedsAdminMax        = errors.New("selected window seconds exceeds administrator maximum")
	ErrInvalidStorageBudget         = errors.New("storage budget bytes must be greater than 0")
	ErrStorageBudgetExceedsAdminMax = errors.New("storage budget bytes exceeds administrator maximum")
	ErrInvalidEmergencyFreeSpace    = errors.New("emergency free space bytes must be less than minimum free space bytes")
	ErrUnsatisfiableStorageConfig   = errors.New("storage budget plus minimum free space exceeds total usable capacity")
)

// RetroDVRMode defines how streams are buffered on NVMe for Retro-DVR.
type RetroDVRMode string

const (
	RetroModeActiveOnly          RetroDVRMode = "active_only"
	RetroModeActiveAndRecordings RetroDVRMode = "active_and_recordings"
	RetroModeSelectedChannels    RetroDVRMode = "selected_channels"
)

// RetroDVRUserConfig holds user-configurable Retro-DVR settings.
type RetroDVRUserConfig struct {
	Enabled               bool         `json:"enabled"`
	SelectedWindowSeconds int          `json:"selected_window_seconds"` // 1800 (30m), 3600 (1h), 7200 (2h), 14400 (4h)
	Mode                  RetroDVRMode `json:"mode"`
	SelectedChannelRefs   []string     `json:"selected_channel_refs,omitempty"`
}

// DefaultRetroDVRUserConfig returns standard user defaults.
func DefaultRetroDVRUserConfig() RetroDVRUserConfig {
	return RetroDVRUserConfig{
		Enabled:               true,
		SelectedWindowSeconds: 3600, // 1 hour default
		Mode:                  RetroModeActiveOnly,
	}
}

// RetroDVRSystemConfig defines system-wide administrative storage limits on NVMe.
type RetroDVRSystemConfig struct {
	StorageBudgetBytes      int64 `json:"storage_budget_bytes"`       // Default: 100 GB
	MaxWindowSeconds        int   `json:"max_window_seconds"`         // Default: 14400 (4h max)
	MaxStorageBytes         int64 `json:"max_storage_bytes"`          // Default: 200 GB
	MinimumFreeSpaceBytes   int64 `json:"minimum_free_space_bytes"`   // Default: 20 GB
	EmergencyFreeSpaceBytes int64 `json:"emergency_free_space_bytes"` // Default: 10 GB
}

// DefaultRetroDVRSystemConfig returns standard system-wide defaults.
func DefaultRetroDVRSystemConfig() RetroDVRSystemConfig {
	return RetroDVRSystemConfig{
		StorageBudgetBytes:      107374182400, // 100 GB
		MaxWindowSeconds:        14400,        // 4h max
		MaxStorageBytes:         214748364800, // 200 GB
		MinimumFreeSpaceBytes:   21474836480,  // 20 GB
		EmergencyFreeSpaceBytes: 10737418240,  // 10 GB
	}
}

// ValidateRetroDVRConfig validates user settings and system limits against physical disk capacity.
func ValidateRetroDVRConfig(userCfg RetroDVRUserConfig, sysCfg RetroDVRSystemConfig, usableDiskCapacityBytes int64) error {
	if userCfg.Enabled {
		if userCfg.SelectedWindowSeconds <= 0 {
			return ErrInvalidWindowSeconds
		}
		if sysCfg.MaxWindowSeconds > 0 && userCfg.SelectedWindowSeconds > sysCfg.MaxWindowSeconds {
			return fmt.Errorf("%w: selected %d > max %d", ErrWindowExceedsAdminMax, userCfg.SelectedWindowSeconds, sysCfg.MaxWindowSeconds)
		}
	}

	if sysCfg.StorageBudgetBytes <= 0 {
		return ErrInvalidStorageBudget
	}
	if sysCfg.MaxStorageBytes > 0 && sysCfg.StorageBudgetBytes > sysCfg.MaxStorageBytes {
		return fmt.Errorf("%w: budget %d > max %d", ErrStorageBudgetExceedsAdminMax, sysCfg.StorageBudgetBytes, sysCfg.MaxStorageBytes)
	}

	if sysCfg.EmergencyFreeSpaceBytes >= sysCfg.MinimumFreeSpaceBytes {
		return fmt.Errorf("%w: emergency %d >= minimum %d", ErrInvalidEmergencyFreeSpace, sysCfg.EmergencyFreeSpaceBytes, sysCfg.MinimumFreeSpaceBytes)
	}

	if usableDiskCapacityBytes > 0 {
		requiredCapacity := sysCfg.StorageBudgetBytes + sysCfg.MinimumFreeSpaceBytes
		if requiredCapacity > usableDiskCapacityBytes {
			return fmt.Errorf("%w: required %d > available %d", ErrUnsatisfiableStorageConfig, requiredCapacity, usableDiskCapacityBytes)
		}
	}

	return nil
}
