// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	dvrconfig "github.com/ManuGH/xg2g/internal/domain/dvr/config"
)

func TestValidateRetroDVRConfig(t *testing.T) {
	userCfg := dvrconfig.DefaultRetroDVRUserConfig()
	sysCfg := dvrconfig.DefaultRetroDVRSystemConfig()

	// Valid config
	if err := dvrconfig.ValidateRetroDVRConfig(userCfg, sysCfg, 500*1024*1024*1024); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	// Invalid window > max
	userCfgBad := userCfg
	userCfgBad.SelectedWindowSeconds = 20000
	if err := dvrconfig.ValidateRetroDVRConfig(userCfgBad, sysCfg, 500*1024*1024*1024); err == nil {
		t.Errorf("Expected error for window > admin max, got nil")
	}

	// Invalid emergency >= minimum free space
	sysCfgBad := sysCfg
	sysCfgBad.EmergencyFreeSpaceBytes = sysCfgBad.MinimumFreeSpaceBytes
	if err := dvrconfig.ValidateRetroDVRConfig(userCfg, sysCfgBad, 500*1024*1024*1024); err == nil {
		t.Errorf("Expected error for emergency >= minimum free space, got nil")
	}
}

func TestNVMeDiskSegmentStore_4StepEvictionAndStorageStates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nvme_dvr_4step_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storagePath := filepath.Join(tmpDir, "reservations.json")
	diskStore := NewDiskSegmentStore(tmpDir, DefaultReservationLimits(), storagePath)
	defer diskStore.Close()

	serviceRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	sessionID := "sess_nvme_3003"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	now := time.Now()
	startTime := now.Add(-3 * time.Hour) // 3 hours of segments

	// Commit 180 segments (1 segment per minute, 1MB each)
	for i := uint64(1); i <= 180; i++ {
		segPath := filepath.Join(sessionDir, fmt.Sprintf("seg_%06d.ts", i))
		payload := make([]byte, 1024*1024)
		if err := os.WriteFile(segPath, payload, 0644); err != nil {
			t.Fatalf("WriteFile seg_%d failed: %v", i, err)
		}

		segStart := startTime.Add(time.Duration(i-1) * time.Minute)
		segEnd := startTime.Add(time.Duration(i) * time.Minute)

		seg := &DiskSegment{
			ID: SegmentID{
				ServiceRef: serviceRef,
				SessionID:  sessionID,
				Kind:       SegmentKindComplete,
				Sequence:   i,
			},
			ServiceRef:    serviceRef,
			SessionID:     sessionID,
			Path:          segPath,
			Sequence:      i,
			StartWallTime: segStart,
			EndWallTime:   segEnd,
			DurationSec:   60.0,
			SizeBytes:     int64(len(payload)),
			State:         SegmentActive,
		}
		diskStore.CommitSegment(seg)
	}

	// Test honest channel availability metering
	avail := diskStore.GetChannelAvailability(serviceRef, 7200)
	if avail.TotalSegmentCount != 180 {
		t.Errorf("Expected 180 segments, got %d", avail.TotalSegmentCount)
	}

	// Run 4-Step Eviction with SelectedWindow=2h (7200s), Budget=50MB, MinimumFree=20GB
	userCfg := dvrconfig.RetroDVRUserConfig{
		Enabled:               true,
		SelectedWindowSeconds: 7200, // 2h cutoff
		Mode:                  dvrconfig.RetroModeActiveOnly,
	}
	sysCfg := dvrconfig.RetroDVRSystemConfig{
		StorageBudgetBytes:      50 * 1024 * 1024, // 50MB limit
		MaxWindowSeconds:        14400,
		MaxStorageBytes:         200 * 1024 * 1024,
		MinimumFreeSpaceBytes:   20 * 1024 * 1024 * 1024,
		EmergencyFreeSpaceBytes: 10 * 1024 * 1024 * 1024,
	}

	evicted, state, err := diskStore.EnforceEvictionPolicy(userCfg, sysCfg, 30*1024*1024*1024)
	if err != nil {
		t.Fatalf("EnforceEvictionPolicy failed: %v", err)
	}

	if evicted == 0 {
		t.Errorf("Expected evicted segments, got 0")
	}
	if state != DVRStorageHealthy {
		t.Errorf("Expected DVRStorageHealthy under 30GB free space, got %v", state)
	}

	// Test Emergency state trigger
	_, emergencyState, _ := diskStore.EnforceEvictionPolicy(userCfg, sysCfg, 5*1024*1024*1024) // 5GB < 10GB emergency threshold
	if emergencyState != DVRStorageEmergency {
		t.Errorf("Expected DVRStorageEmergency state under 5GB free space, got %v", emergencyState)
	}
}
