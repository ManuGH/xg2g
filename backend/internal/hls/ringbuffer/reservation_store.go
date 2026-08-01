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
	ErrReservationNotFound      = errors.New("reservation not found")
	ErrReservationExpired       = errors.New("reservation expired")
	ErrInvalidLeaseDuration     = errors.New("invalid lease duration: must be greater than 0")
	ErrLimitExceededGlobal      = errors.New("global reservation byte/count limit exceeded")
	ErrLimitExceededService     = errors.New("service reservation byte/count limit exceeded")
	ErrLimitExceededMaxSegments = errors.New("reservation max segment limit exceeded")
	ErrLeaseExceedsMaxDuration  = errors.New("lease duration exceeds maximum allowed limit")
	ErrNoSegmentsAvailable      = errors.New("no valid segments available in requested range")
	ErrSegmentMissing           = errors.New("one or more reserved segments are missing or deleting")
)

// ReservationStore provides atomic, thread-safe reservation management backed by SegmentIndex.
type ReservationStore struct {
	mu           sync.Mutex
	index        *SegmentIndex
	reservations map[string]*Reservation
	limits       ReservationLimits
	storagePath  string
	onExpire     func(res *Reservation)
	stopCh       chan struct{}
}

// NewReservationStore creates a new ReservationStore instance.
func NewReservationStore(index *SegmentIndex, limits ReservationLimits, storagePath string) *ReservationStore {
	if limits.MaxPinnedBytesGlobal == 0 {
		limits = DefaultReservationLimits()
	}
	rs := &ReservationStore{
		index:        index,
		reservations: make(map[string]*Reservation),
		limits:       limits,
		storagePath:  storagePath,
		stopCh:       make(chan struct{}),
	}
	go rs.reaperLoop()
	return rs
}

func (rs *ReservationStore) Close() {
	select {
	case <-rs.stopCh:
	default:
		close(rs.stopCh)
	}
}

func (rs *ReservationStore) reaperLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-rs.stopCh:
			return
		case <-ticker.C:
			rs.mu.Lock()
			purged := rs.purgeExpiredLocked()
			if purged {
				_ = rs.saveStateLocked()
			}
			rs.mu.Unlock()
		}
	}
}

// HasReservationsForSession checks if any active reservation belongs to sessionID.
func (rs *ReservationStore) HasReservationsForSession(sessionID string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	now := time.Now()
	for _, res := range rs.reservations {
		if now.After(res.ExpiresAt) {
			continue
		}
		for _, segID := range res.SegmentIDs {
			if segID.SessionID == sessionID {
				return true
			}
		}
	}
	return false
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

		// Check for sequence or PTS continuity gaps between consecutive segments
		if i > 0 {
			prev := segments[i-1]
			seqGap := seg.Sequence > prev.Sequence+1
			ptsGap := seg.StartPTS90k > prev.EndPTS90k+9000 // >100ms PTS jump
			timeGap := seg.StartWallTime.Sub(prev.EndWallTime) > 3*time.Second

			if seqGap || ptsGap || timeGap {
				gaps = append(gaps, Gap{
					StartPTS90k:   prev.EndPTS90k,
					EndPTS90k:     seg.StartPTS90k,
					StartWallTime: prev.EndWallTime,
					EndWallTime:   seg.StartWallTime,
					DurationSec:   seg.StartWallTime.Sub(prev.EndWallTime).Seconds(),
					Reason:        fmt.Sprintf("GAP_SEQ_%d_PTS_%d", seg.Sequence-prev.Sequence, seg.StartPTS90k-prev.EndPTS90k),
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

// ReserveRange performs range probing, limit checking, and multi-job segment locking in a single atomic transaction.
func (rs *ReservationStore) ReserveRange(serviceRef string, start, end time.Time, ownerID string, leaseDuration time.Duration) (Reservation, error) {
	if leaseDuration <= 0 {
		return Reservation{}, ErrInvalidLeaseDuration
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

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
	if probe.SegmentCount > rs.limits.MaxSegmentsPerReservation {
		return Reservation{}, ErrLimitExceededMaxSegments
	}

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
	now := time.Now()
	resID := fmt.Sprintf("res_%d_%s", now.UnixNano(), ownerID)

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
		Status:     probe.Completeness,
		CreatedAt:  now,
		ExpiresAt:  now.Add(leaseDuration),
		TotalBytes: probe.TotalBytes,
	}

	rs.reservations[res.ID] = res

	if err := rs.saveStateLocked(); err != nil {
		for _, seg := range segments {
			delete(seg.ReservationIDs, resID)
			if len(seg.ReservationIDs) == 0 {
				seg.State = SegmentActive
			}
		}
		delete(rs.reservations, resID)
		return Reservation{}, fmt.Errorf("failed to persist reservation state: %w", err)
	}

	return *res, nil
}

// GetReservation fetches an unexpired reservation by ID.
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

// ListReservedSegments returns immutable SegmentHandles for an active reservation or error if missing.
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
			return nil, ErrSegmentMissing
		}
		handles = append(handles, SegmentHandle{
			ID:            seg.ID,
			Location:      seg.Location,
			DurationSec:   seg.DurationSec,
			Sequence:      seg.Sequence,
			StartPTS90k:   seg.StartPTS90k,
			EndPTS90k:     seg.EndPTS90k,
			PTSEpoch:      seg.PTSEpoch,
			StartWallTime: seg.StartWallTime,
			EndWallTime:   seg.EndWallTime,
			SizeBytes:     seg.SizeBytes,
			Discontinuity: seg.Discontinuity,
			CodecHash:     seg.CodecHash,
			Data:          seg.Data,
		})
	}

	return handles, nil
}

// RenewReservation updates the lease expiration timestamp from now, capped by MaxLeaseDuration.
func (rs *ReservationStore) RenewReservation(reservationID string, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		return ErrInvalidLeaseDuration
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

	res, ok := rs.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	if time.Now().After(res.ExpiresAt) {
		return ErrReservationExpired
	}

	if leaseDuration > rs.limits.MaxLeaseDuration {
		leaseDuration = rs.limits.MaxLeaseDuration
	}

	oldExpiry := res.ExpiresAt
	res.ExpiresAt = time.Now().Add(leaseDuration)
	if err := rs.saveStateLocked(); err != nil {
		res.ExpiresAt = oldExpiry // ROLLBACK!
		return fmt.Errorf("failed to persist renewed reservation state: %w", err)
	}
	return nil
}

// ReleaseReservation unlocks segments and removes the reservation.
func (rs *ReservationStore) ReleaseReservation(reservationID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	res, ok := rs.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	delete(rs.reservations, reservationID)
	if err := rs.saveStateLocked(); err != nil {
		rs.reservations[reservationID] = res // ROLLBACK!
		return fmt.Errorf("failed to persist released reservation state: %w", err)
	}

	rs.releaseReservationLocked(res)
	return nil
}

func (rs *ReservationStore) releaseReservationLocked(res *Reservation) {
	for _, segID := range res.SegmentIDs {
		if seg, ok := rs.index.GetByID(segID); ok {
			delete(seg.ReservationIDs, res.ID)
			if len(seg.ReservationIDs) == 0 && seg.State == SegmentReserved {
				seg.State = SegmentActive
			}
		}
	}
	delete(rs.reservations, res.ID)
}

func (rs *ReservationStore) purgeExpiredLocked() bool {
	now := time.Now()
	purged := false
	for _, res := range rs.reservations {
		if now.After(res.ExpiresAt) {
			if rs.onExpire != nil {
				rs.onExpire(res)
			}
			rs.releaseReservationLocked(res)
			purged = true
		}
	}
	return purged
}

// Atomic file persistence (reservations.json.tmp -> fsync -> rename)
func (rs *ReservationStore) saveStateLocked() error {
	if rs.storagePath == "" {
		return nil
	}

	dir := filepath.Dir(rs.storagePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	tmpPath := rs.storagePath + ".tmp"
	data, err := json.MarshalIndent(rs.reservations, "", "  ")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: tmpPath is derived from operator-configured storagePath
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

	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}
