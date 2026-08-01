package ringbuffer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrReservationNotFound     = errors.New("reservation not found")
	ErrReservationExpired      = errors.New("reservation expired")
	ErrLimitExceededGlobal     = errors.New("global reservation byte/count limit exceeded")
	ErrLimitExceededService    = errors.New("service reservation byte/count limit exceeded")
	ErrLeaseExceedsMaxDuration = errors.New("lease duration exceeds maximum allowed limit")
	ErrNoSegmentsAvailable     = errors.New("no valid segments available in requested range")
)

// ReservationStore provides atomic, thread-safe reservation management backed by SegmentIndex.
type ReservationStore struct {
	mu           sync.Mutex
	index        *SegmentIndex
	reservations map[string]*Reservation
	limits       ReservationLimits
	storagePath  string
	onExpire     func(res *Reservation)
}

// NewReservationStore creates a new ReservationStore instance.
func NewReservationStore(index *SegmentIndex, limits ReservationLimits, storagePath string) *ReservationStore {
	if limits.MaxPinnedBytesGlobal == 0 {
		limits = DefaultReservationLimits()
	}
	return &ReservationStore{
		index:        index,
		reservations: make(map[string]*Reservation),
		limits:       limits,
		storagePath:  storagePath,
	}
}

// ProbeRange evaluates ringbuffer segment completeness without creating a reservation.
func (rs *ReservationStore) ProbeRange(serviceRef string, start, end time.Time) (RangeProbe, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	return rs.probeRangeLocked(serviceRef, start, end)
}

func (rs *ReservationStore) probeRangeLocked(serviceRef string, start, end time.Time) (RangeProbe, error) {
	segments := rs.index.SelectRange(serviceRef, start, end)
	probe := RangeProbe{
		RequestedStart: start,
		RequestedEnd:   end,
		SegmentCount:   len(segments),
	}

	if len(segments) == 0 {
		probe.Completeness = CompletenessUnavailable
		return probe, nil
	}

	availStart := segments[0].StartWallTime
	availEnd := segments[len(segments)-1].EndWallTime
	probe.AvailableStart = &availStart
	probe.AvailableEnd = &availEnd

	var totalBytes int64
	var codecChanges int
	var discontinuities int
	var gaps []Gap
	lastCodec := ""

	for i, seg := range segments {
		totalBytes += seg.SizeBytes

		if seg.Discontinuity {
			discontinuities++
		}
		if lastCodec != "" && seg.CodecHash != lastCodec {
			codecChanges++
		}
		lastCodec = seg.CodecHash

		// Check for internal temporal gaps between consecutive segments (>10s)
		if i > 0 {
			prevEnd := segments[i-1].EndWallTime
			if seg.StartWallTime.Sub(prevEnd) > 10*time.Second {
				gaps = append(gaps, Gap{
					StartPTS90k:   segments[i-1].EndPTS90k,
					EndPTS90k:     seg.StartPTS90k,
					StartWallTime: prevEnd,
					EndWallTime:   seg.StartWallTime,
					DurationSec:   seg.StartWallTime.Sub(prevEnd).Seconds(),
					Reason:        "TEMPORAL_GAP",
				})
			}
		}
	}

	probe.TotalBytes = totalBytes
	probe.CodecChanges = codecChanges
	probe.Discontinuities = discontinuities
	probe.Gaps = gaps

	hasStart := availStart.Before(start) || availStart.Equal(start)
	hasEnd := availEnd.After(end) || availEnd.Equal(end)

	if len(gaps) > 0 {
		probe.Completeness = CompletenessGapped
	} else if hasStart && hasEnd {
		probe.Completeness = CompletenessComplete
	} else if !hasStart && hasEnd {
		probe.Completeness = CompletenessPartialStart
	} else if hasStart && !hasEnd {
		probe.Completeness = CompletenessPartialEnd
	} else {
		probe.Completeness = CompletenessPartialBoth
	}

	return probe, nil
}

// ReserveRange performs range probing, limit checking, and segment locking in a single atomic transaction.
func (rs *ReservationStore) ReserveRange(serviceRef string, start, end time.Time, ownerID string, leaseDuration time.Duration) (Reservation, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Clean up any expired reservations prior to evaluating limits
	rs.purgeExpiredLocked()

	if leaseDuration > rs.limits.MaxLeaseDuration {
		return Reservation{}, ErrLeaseExceedsMaxDuration
	}

	probe, err := rs.probeRangeLocked(serviceRef, start, end)
	if err != nil {
		return Reservation{}, err
	}
	if probe.Completeness == CompletenessUnavailable || probe.SegmentCount == 0 {
		return Reservation{}, ErrNoSegmentsAvailable
	}

	// Evaluate limits
	var globalBytes int64
	var globalCount int
	var serviceBytes int64
	var serviceCount int

	for _, res := range rs.reservations {
		globalBytes += res.TotalBytes
		globalCount++
		if res.ServiceRef == serviceRef {
			serviceBytes += res.TotalBytes
			serviceCount++
		}
	}

	if globalCount+1 > rs.limits.MaxReservationsGlobal || globalBytes+probe.TotalBytes > rs.limits.MaxPinnedBytesGlobal {
		return Reservation{}, ErrLimitExceededGlobal
	}
	if serviceCount+1 > rs.limits.MaxReservationsPerService || serviceBytes+probe.TotalBytes > rs.limits.MaxPinnedBytesPerService {
		return Reservation{}, ErrLimitExceededService
	}

	segments := rs.index.SelectRange(serviceRef, start, end)
	var segIDs []SegmentID
	for _, seg := range segments {
		seg.State = SegmentReserved
		segIDs = append(segIDs, seg.ID)
	}

	now := time.Now()
	res := &Reservation{
		ID:         fmt.Sprintf("res_%d_%s", now.UnixNano(), ownerID),
		OwnerID:    ownerID,
		ServiceRef: serviceRef,
		SegmentIDs: segIDs,
		Start:      start,
		End:        end,
		Status:     probe.Completeness,
		CreatedAt:  now,
		ExpiresAt:  now.Add(leaseDuration),
		TotalBytes: probe.TotalBytes,
	}

	rs.reservations[res.ID] = res
	_ = rs.saveStateLocked()

	return *res, nil
}

// GetReservation fetches a reservation by ID.
func (rs *ReservationStore) GetReservation(reservationID string) (Reservation, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	res, ok := rs.reservations[reservationID]
	if !ok {
		return Reservation{}, ErrReservationNotFound
	}
	if time.Now().After(res.ExpiresAt) {
		return Reservation{}, ErrReservationExpired
	}
	return *res, nil
}

// ListReservedSegments returns immutable SegmentHandles for an active reservation.
func (rs *ReservationStore) ListReservedSegments(reservationID string) ([]SegmentHandle, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	res, ok := rs.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if time.Now().After(res.ExpiresAt) {
		return nil, ErrReservationExpired
	}

	var handles []SegmentHandle
	for _, segID := range res.SegmentIDs {
		seg, ok := rs.index.GetByID(segID)
		if !ok || seg.State == SegmentDeleting || seg.State == SegmentMissing {
			continue
		}
		handles = append(handles, SegmentHandle{
			ID:            seg.ID,
			Path:          seg.Path,
			Sequence:      seg.Sequence,
			StartPTS90k:   seg.StartPTS90k,
			EndPTS90k:     seg.EndPTS90k,
			PTSEpoch:      seg.PTSEpoch,
			StartWallTime: seg.StartWallTime,
			EndWallTime:   seg.EndWallTime,
			SizeBytes:     seg.SizeBytes,
			Discontinuity: seg.Discontinuity,
			CodecHash:     seg.CodecHash,
		})
	}

	return handles, nil
}

// RenewReservation updates the lease expiration timestamp from now, capped by MaxLeaseDuration.
func (rs *ReservationStore) RenewReservation(reservationID string, leaseDuration time.Duration) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	res, ok := rs.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}
	if leaseDuration > rs.limits.MaxLeaseDuration {
		leaseDuration = rs.limits.MaxLeaseDuration
	}

	res.ExpiresAt = time.Now().Add(leaseDuration)
	return rs.saveStateLocked()
}

// ReleaseReservation unlocks segments and removes the reservation.
func (rs *ReservationStore) ReleaseReservation(reservationID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	res, ok := rs.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	rs.releaseReservationLocked(res)
	return rs.saveStateLocked()
}

func (rs *ReservationStore) releaseReservationLocked(res *Reservation) {
	for _, segID := range res.SegmentIDs {
		if seg, ok := rs.index.GetByID(segID); ok && seg.State == SegmentReserved {
			seg.State = SegmentActive
		}
	}
	delete(rs.reservations, res.ID)
}

func (rs *ReservationStore) purgeExpiredLocked() {
	now := time.Now()
	for id, res := range rs.reservations {
		if now.After(res.ExpiresAt) {
			rs.releaseReservationLocked(res)
			delete(rs.reservations, id)
		}
	}
}

// Atomic file persistence (reservations.json.tmp -> fsync -> rename)
func (rs *ReservationStore) saveStateLocked() error {
	if rs.storagePath == "" {
		return nil
	}

	dir := filepath.Dir(rs.storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := rs.storagePath + ".tmp"
	data, err := json.MarshalIndent(rs.reservations, "", "  ")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, rs.storagePath); err != nil {
		return err
	}

	// Sync parent directory
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}
