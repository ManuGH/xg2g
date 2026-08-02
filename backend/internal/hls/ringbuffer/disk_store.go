// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	dvrconfig "github.com/ManuGH/xg2g/internal/domain/dvr/config"
)

var (
	ErrDiskSegmentNotFound = errors.New("disk segment not found")
	ErrDiskSegmentReserved = errors.New("cannot delete segment: actively reserved")
)

// DVRStorageState tracks the operational health state of the NVMe DVR storage layer.
type DVRStorageState string

const (
	DVRStorageHealthy   DVRStorageState = "HEALTHY"
	DVRStoragePressure  DVRStorageState = "PRESSURE"
	DVRStorageEmergency DVRStorageState = "EMERGENCY"
	DVRStorageReadOnly  DVRStorageState = "READ_ONLY"
	DVRStorageOffline   DVRStorageState = "OFFLINE"
)

// EvictionReason documents why an unreserved NVMe segment was evicted.
type EvictionReason string

const (
	EvictionWindowExpired  EvictionReason = "WINDOW_EXPIRED"
	EvictionBudgetExceeded EvictionReason = "BUDGET_EXCEEDED"
	EvictionLowFreeSpace   EvictionReason = "LOW_FREE_SPACE"
)

// DiskSegment represents an authoritative HLS segment stored on NVMe disk.
type DiskSegment struct {
	ID             SegmentID           `json:"id"`
	ServiceRef     string              `json:"service_ref"`
	SessionID      string              `json:"session_id"`
	Path           string              `json:"path"`
	Sequence       uint64              `json:"sequence"`
	StartWallTime  time.Time           `json:"start_wall_time"`
	EndWallTime    time.Time           `json:"end_wall_time"`
	DurationSec    float64             `json:"duration_sec"`
	StartPTS90k    int64               `json:"start_pts_90k"`
	EndPTS90k      int64               `json:"end_pts_90k"`
	PTSEpoch       uint32              `json:"pts_epoch"`
	Discontinuity  bool                `json:"discontinuity"`
	CodecHash      string              `json:"codec_hash"`
	SizeBytes      int64               `json:"size_bytes"`
	State          SegmentState        `json:"state"`
	ReservationIDs map[string]struct{} `json:"reservation_ids"`
}

// IsReserved returns true if one or more active reservations hold this segment.
func (seg *DiskSegment) IsReserved() bool {
	return len(seg.ReservationIDs) > 0
}

// ChannelAvailabilityInfo provides honest metering of configured vs actual available Retro-DVR window per channel.
type ChannelAvailabilityInfo struct {
	ServiceRef          string    `json:"service_ref"`
	ConfiguredWindowSec int       `json:"configured_window_sec"`
	OldestSegmentTime   time.Time `json:"oldest_segment_time"`
	NewestSegmentTime   time.Time `json:"newest_segment_time"`
	ContinuousFrom      time.Time `json:"continuous_from"`
	ContinuousSeconds   int       `json:"continuous_seconds"`
	TotalCoveredSeconds int       `json:"total_covered_seconds"`
	TotalSegmentCount   int       `json:"total_segment_count"`
	GapCount            int       `json:"gap_count"`
	TotalSizeBytes      int64     `json:"total_size_bytes"`
}

// DiskSegmentStore manages NVMe disk segments per ServiceRef across multiple stream restarts.
type DiskSegmentStore struct {
	mu           sync.RWMutex
	byService    map[string][]*DiskSegment
	byID         map[string]*DiskSegment
	reservations map[string]*Reservation
	limits       ReservationLimits
	storagePath  string
	dvrRoot      string
	stopCh       chan struct{}
	storageState DVRStorageState
	degraded     bool
}

// NewDiskSegmentStore initializes an authoritative NVMe Disk Segment Store.
func NewDiskSegmentStore(dvrRoot string, limits ReservationLimits, storagePath string) *DiskSegmentStore {
	if limits.MaxPinnedBytesGlobal == 0 {
		limits = DefaultReservationLimits()
	}
	ds := &DiskSegmentStore{
		byService:    make(map[string][]*DiskSegment),
		byID:         make(map[string]*DiskSegment),
		reservations: make(map[string]*Reservation),
		limits:       limits,
		storagePath:  storagePath,
		dvrRoot:      dvrRoot,
		storageState: DVRStorageHealthy,
		stopCh:       make(chan struct{}),
	}
	go ds.reaperLoop()
	return ds
}

// StorageState returns the current operational state of the NVMe DVR store.
func (ds *DiskSegmentStore) StorageState() DVRStorageState {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.storageState
}

// Close gracefully stops the background lease reaper.
func (ds *DiskSegmentStore) Close() {
	select {
	case <-ds.stopCh:
	default:
		close(ds.stopCh)
	}
}

func (ds *DiskSegmentStore) reaperLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.mu.Lock()
			purged := ds.purgeExpiredLocked()
			if purged {
				if err := ds.saveStateLocked(); err != nil {
					ds.degraded = true
					ds.storageState = DVRStoragePressure
				}
			}
			ds.mu.Unlock()
		}
	}
}

// GetChannelAvailability calculates honest available window, gap count, and continuous segment statistics for serviceRef.
func (ds *DiskSegmentStore) GetChannelAvailability(serviceRef string, configuredWindowSec int) ChannelAvailabilityInfo {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	segments := ds.byService[serviceRef]
	var activeSegs []*DiskSegment
	var totalBytes int64

	for _, seg := range segments {
		if seg.State != SegmentDeleting && seg.State != SegmentMissing {
			activeSegs = append(activeSegs, seg)
			totalBytes += seg.SizeBytes
		}
	}

	if len(activeSegs) == 0 {
		return ChannelAvailabilityInfo{
			ServiceRef:          serviceRef,
			ConfiguredWindowSec: configuredWindowSec,
		}
	}

	oldest := activeSegs[0].StartWallTime
	newest := activeSegs[len(activeSegs)-1].EndWallTime
	totalCoveredSec := int(newest.Sub(oldest).Seconds())
	if totalCoveredSec < 0 {
		totalCoveredSec = 0
	}

	// Calculate gaps and continuous range starting from newest backwards
	gapCount := 0
	continuousFrom := newest

	for i := len(activeSegs) - 1; i >= 0; i-- {
		seg := activeSegs[i]
		if i < len(activeSegs)-1 {
			nextSeg := activeSegs[i+1]
			seqGap := nextSeg.Sequence > seg.Sequence+1
			timeGap := nextSeg.StartWallTime.Sub(seg.EndWallTime) > 3*time.Second
			if seqGap || timeGap {
				gapCount++
				if continuousFrom.Equal(newest) {
					continuousFrom = nextSeg.StartWallTime
				}
			}
		}
	}
	if continuousFrom.Equal(newest) {
		continuousFrom = oldest
	}
	continuousSec := int(newest.Sub(continuousFrom).Seconds())
	if continuousSec < 0 {
		continuousSec = 0
	}

	return ChannelAvailabilityInfo{
		ServiceRef:          serviceRef,
		ConfiguredWindowSec: configuredWindowSec,
		OldestSegmentTime:   oldest,
		NewestSegmentTime:   newest,
		ContinuousFrom:      continuousFrom,
		ContinuousSeconds:   continuousSec,
		TotalCoveredSeconds: totalCoveredSec,
		TotalSegmentCount:   len(activeSegs),
		GapCount:            gapCount,
		TotalSizeBytes:      totalBytes,
	}
}

// EnforceEvictionPolicy executes 4-step dual eviction (Time Window -> System Budget -> Low Free Space -> Emergency Pause).
func (ds *DiskSegmentStore) EnforceEvictionPolicy(userCfg dvrconfig.RetroDVRUserConfig, sysCfg dvrconfig.RetroDVRSystemConfig, currentDiskFreeBytes int64) (int, DVRStorageState, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !userCfg.Enabled {
		return 0, ds.storageState, nil
	}

	windowSec := userCfg.SelectedWindowSeconds
	if sysCfg.MaxWindowSeconds > 0 && windowSec > sysCfg.MaxWindowSeconds {
		windowSec = sysCfg.MaxWindowSeconds
	}
	cutoff := time.Now().Add(-time.Duration(windowSec) * time.Second)

	budget := sysCfg.StorageBudgetBytes
	if sysCfg.MaxStorageBytes > 0 && (budget <= 0 || budget > sysCfg.MaxStorageBytes) {
		budget = sysCfg.MaxStorageBytes
	}

	evictedCount := 0

	// Step 1: Time Window Eviction (WINDOW_EXPIRED)
	var timeCandidates []*DiskSegment
	for _, segments := range ds.byService {
		for _, seg := range segments {
			if seg.State == SegmentActive && !seg.IsReserved() {
				if seg.EndWallTime.Before(cutoff) {
					timeCandidates = append(timeCandidates, seg)
				}
			}
		}
	}

	for _, cand := range timeCandidates {
		cand.State = SegmentDeleting
		if cand.Path != "" {
			_ = os.Remove(cand.Path)
		}
		delete(ds.byID, cand.ID.String())
		evictedCount++
	}

	ds.rebuildServiceIndexLocked()

	// Step 2: System Storage Budget Eviction (BUDGET_EXCEEDED)
	if budget > 0 {
		var allUnreserved []*DiskSegment
		var totalDVRBytes int64

		for _, segments := range ds.byService {
			for _, seg := range segments {
				totalDVRBytes += seg.SizeBytes
				if seg.State == SegmentActive && !seg.IsReserved() {
					allUnreserved = append(allUnreserved, seg)
				}
			}
		}

		if totalDVRBytes > budget && len(allUnreserved) > 0 {
			sort.Slice(allUnreserved, func(i, j int) bool {
				return allUnreserved[i].StartWallTime.Before(allUnreserved[j].StartWallTime)
			})

			for _, cand := range allUnreserved {
				if totalDVRBytes <= budget {
					break
				}
				cand.State = SegmentDeleting
				if cand.Path != "" {
					_ = os.Remove(cand.Path)
				}
				delete(ds.byID, cand.ID.String())
				totalDVRBytes -= cand.SizeBytes
				evictedCount++
			}

			ds.rebuildServiceIndexLocked()
		}
	}

	// Step 3: Low Free Space Aggressive Eviction (LOW_FREE_SPACE)
	if currentDiskFreeBytes > 0 && sysCfg.MinimumFreeSpaceBytes > 0 && currentDiskFreeBytes < sysCfg.MinimumFreeSpaceBytes {
		ds.storageState = DVRStoragePressure
		var remainingUnreserved []*DiskSegment
		for _, segments := range ds.byService {
			for _, seg := range segments {
				if seg.State == SegmentActive && !seg.IsReserved() {
					remainingUnreserved = append(remainingUnreserved, seg)
				}
			}
		}

		sort.Slice(remainingUnreserved, func(i, j int) bool {
			return remainingUnreserved[i].StartWallTime.Before(remainingUnreserved[j].StartWallTime)
		})

		freedBytes := int64(0)
		bytesToFree := sysCfg.MinimumFreeSpaceBytes - currentDiskFreeBytes

		for _, cand := range remainingUnreserved {
			if freedBytes >= bytesToFree {
				break
			}
			cand.State = SegmentDeleting
			if cand.Path != "" {
				_ = os.Remove(cand.Path)
			}
			delete(ds.byID, cand.ID.String())
			freedBytes += cand.SizeBytes
			evictedCount++
		}

		ds.rebuildServiceIndexLocked()
	} else if ds.storageState == DVRStoragePressure {
		ds.storageState = DVRStorageHealthy
	}

	// Step 4: Emergency Free Space Protection (EMERGENCY)
	if currentDiskFreeBytes > 0 && sysCfg.EmergencyFreeSpaceBytes > 0 && currentDiskFreeBytes < sysCfg.EmergencyFreeSpaceBytes {
		ds.storageState = DVRStorageEmergency
	}

	return evictedCount, ds.storageState, nil
}

func (ds *DiskSegmentStore) rebuildServiceIndexLocked() {
	newByService := make(map[string][]*DiskSegment)
	for _, seg := range ds.byID {
		if seg.State != SegmentDeleting && seg.State != SegmentMissing {
			newByService[seg.ServiceRef] = append(newByService[seg.ServiceRef], seg)
		}
	}
	for service, segs := range newByService {
		sort.Slice(segs, func(i, j int) bool {
			return segs[i].StartWallTime.Before(segs[j].StartWallTime)
		})
		newByService[service] = segs
	}
	ds.byService = newByService
}

// CommitSegment registers a fully fsync'd and committed NVMe segment into the store.
func (ds *DiskSegmentStore) CommitSegment(seg *DiskSegment) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// If in emergency state, refuse new disk segment persistence to protect NVMe!
	if ds.storageState == DVRStorageEmergency {
		return
	}

	if seg.ReservationIDs == nil {
		seg.ReservationIDs = make(map[string]struct{})
	}
	seg.State = SegmentActive

	key := seg.ID.String()
	if existing, ok := ds.byID[key]; ok {
		existing.SizeBytes = seg.SizeBytes
		existing.EndWallTime = seg.EndWallTime
		existing.DurationSec = seg.DurationSec
		return
	}

	ds.byID[key] = seg
	service := seg.ServiceRef
	segments := ds.byService[service]
	segments = append(segments, seg)
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].StartWallTime.Equal(segments[j].StartWallTime) {
			return segments[i].Sequence < segments[j].Sequence
		}
		return segments[i].StartWallTime.Before(segments[j].StartWallTime)
	})
	ds.byService[service] = segments
}

// SelectRange filters non-deleting NVMe segments falling within [start, end] for a serviceRef across all session generations.
func (ds *DiskSegmentStore) SelectRange(serviceRef string, start, end time.Time) []*DiskSegment {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	segments := ds.byService[serviceRef]
	var matched []*DiskSegment

	for _, seg := range segments {
		if seg.State == SegmentDeleting || seg.State == SegmentMissing {
			continue
		}
		if seg.EndWallTime.After(start) && seg.StartWallTime.Before(end) {
			matched = append(matched, seg)
		}
	}

	return matched
}

// ReserveRange locks a range of NVMe disk segments for serviceRef across multiple stream sessions.
func (ds *DiskSegmentStore) ReserveRange(serviceRef string, start, end time.Time, ownerID string, leaseDuration time.Duration) (Reservation, error) {
	if leaseDuration <= 0 {
		return Reservation{}, ErrInvalidLeaseDuration
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.degraded || ds.storageState == DVRStorageEmergency {
		return Reservation{}, ErrStoreDegraded
	}

	ds.purgeExpiredLocked()

	if leaseDuration > ds.limits.MaxLeaseDuration {
		return Reservation{}, ErrLeaseExceedsMaxDuration
	}

	segments := ds.SelectRange(serviceRef, start, end)
	if len(segments) == 0 {
		return Reservation{}, ErrNoSegmentsAvailable
	}
	if len(segments) > ds.limits.MaxSegmentsPerReservation {
		return Reservation{}, ErrLimitExceededMaxSegments
	}

	var totalBytes int64
	for _, seg := range segments {
		totalBytes += seg.SizeBytes
	}

	now := time.Now()
	resID := fmt.Sprintf("res_nvme_%d_%s", now.UnixNano(), ownerID)

	var segIDs []SegmentID
	for _, seg := range segments {
		if seg.ReservationIDs == nil {
			seg.ReservationIDs = make(map[string]struct{})
		}
		seg.ReservationIDs[resID] = struct{}{}
		seg.State = SegmentReserved
		segIDs = append(segIDs, seg.ID)
	}

	res := &Reservation{
		ID:         resID,
		OwnerID:    ownerID,
		ServiceRef: serviceRef,
		SegmentIDs: segIDs,
		Start:      start,
		End:        end,
		Status:     CompletenessComplete,
		CreatedAt:  now,
		ExpiresAt:  now.Add(leaseDuration),
		TotalBytes: totalBytes,
	}

	ds.reservations[res.ID] = res

	if err := ds.saveStateLocked(); err != nil {
		for _, seg := range segments {
			delete(seg.ReservationIDs, resID)
			if len(seg.ReservationIDs) == 0 {
				seg.State = SegmentActive
			}
		}
		delete(ds.reservations, resID)
		return Reservation{}, fmt.Errorf("failed to persist disk reservation state: %w", err)
	}

	return *res, nil
}

// TryDeleteExpired checks reservation status under store.mu and physically removes unreserved NVMe files.
func (ds *DiskSegmentStore) TryDeleteExpired(segID SegmentID) (bool, error) {
	ds.mu.Lock()
	key := segID.String()
	seg, ok := ds.byID[key]
	if !ok {
		ds.mu.Unlock()
		return true, nil
	}

	if seg.IsReserved() {
		ds.mu.Unlock()
		return false, ErrDiskSegmentReserved
	}

	seg.State = SegmentDeleting
	ds.mu.Unlock()

	if seg.Path != "" {
		_ = os.Remove(seg.Path)
	}

	ds.mu.Lock()
	delete(ds.byID, key)
	segments := ds.byService[seg.ServiceRef]
	var filtered []*DiskSegment
	for _, s := range segments {
		if s.ID != segID {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == 0 {
		delete(ds.byService, seg.ServiceRef)
	} else {
		ds.byService[seg.ServiceRef] = filtered
	}
	ds.mu.Unlock()

	return true, nil
}

// ReleaseReservation unlocks NVMe disk segments and deletes reservation.
func (ds *DiskSegmentStore) ReleaseReservation(reservationID string) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	res, ok := ds.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	delete(ds.reservations, reservationID)
	if err := ds.saveStateLocked(); err != nil {
		ds.reservations[reservationID] = res
		return fmt.Errorf("failed to persist release state: %w", err)
	}

	for _, segID := range res.SegmentIDs {
		if seg, ok := ds.byID[segID.String()]; ok {
			delete(seg.ReservationIDs, reservationID)
			if len(seg.ReservationIDs) == 0 && seg.State == SegmentReserved {
				seg.State = SegmentActive
			}
		}
	}

	return nil
}

// RecoverFromDisk inventories existing NVMe session directories on startup.
func (ds *DiskSegmentStore) RecoverFromDisk() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.dvrRoot == "" {
		return nil
	}

	sessionsDir := filepath.Join(ds.dvrRoot, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil
	}

	sessionEntries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return fmt.Errorf("failed to read sessions dir: %w", err)
	}

	for _, sEntry := range sessionEntries {
		if !sEntry.IsDir() {
			continue
		}
		sessionID := sEntry.Name()
		sDir := filepath.Join(sessionsDir, sessionID)

		files, err := os.ReadDir(sDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if f.IsDir() || !strings.HasPrefix(f.Name(), "seg_") || !strings.HasSuffix(f.Name(), ".ts") {
				continue
			}

			filePath := filepath.Join(sDir, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}

			var seq uint64
			_, _ = fmt.Sscanf(f.Name(), "seg_%d.ts", &seq)

			segID := SegmentID{
				ServiceRef: sessionID,
				SessionID:  sessionID,
				Kind:       SegmentKindComplete,
				Sequence:   seq,
			}

			ds.byID[segID.String()] = &DiskSegment{
				ID:             segID,
				ServiceRef:     sessionID,
				SessionID:      sessionID,
				Path:           filePath,
				Sequence:       seq,
				StartWallTime:  info.ModTime().Add(-2 * time.Second),
				EndWallTime:    info.ModTime(),
				DurationSec:    2.0,
				SizeBytes:      info.Size(),
				State:          SegmentActive,
				ReservationIDs: make(map[string]struct{}),
			}
		}
	}

	return nil
}

func (ds *DiskSegmentStore) purgeExpiredLocked() bool {
	now := time.Now()
	purged := false
	for _, res := range ds.reservations {
		if now.After(res.ExpiresAt) {
			for _, segID := range res.SegmentIDs {
				if seg, ok := ds.byID[segID.String()]; ok {
					delete(seg.ReservationIDs, res.ID)
					if len(seg.ReservationIDs) == 0 && seg.State == SegmentReserved {
						seg.State = SegmentActive
					}
				}
			}
			delete(ds.reservations, res.ID)
			purged = true
		}
	}
	return purged
}

func (ds *DiskSegmentStore) saveStateLocked() error {
	if ds.storagePath == "" {
		return nil
	}

	dir := filepath.Dir(ds.storagePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	tmpPath := ds.storagePath + ".tmp"
	data, err := json.MarshalIndent(ds.reservations, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, ds.storagePath)
}
