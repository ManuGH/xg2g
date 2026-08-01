package ringbuffer

import (
	"time"
)

// ReservationLimits configures strict global and per-service boundaries for reservations.
type ReservationLimits struct {
	MaxPinnedBytesGlobal      int64         `json:"max_pinned_bytes_global"`
	MaxPinnedBytesPerService  int64         `json:"max_pinned_bytes_per_service"`
	MaxReservationsGlobal     int           `json:"max_reservations_global"`
	MaxReservationsPerService int           `json:"max_reservations_per_service"`
	MaxSegmentsPerReservation int           `json:"max_segments_per_reservation"`
	MaxLeaseDuration          time.Duration `json:"max_lease_duration"`
}

// DefaultReservationLimits provides sane defaults for Retro-DVR reservations.
func DefaultReservationLimits() ReservationLimits {
	return ReservationLimits{
		MaxPinnedBytesGlobal:      10 * 1024 * 1024 * 1024, // 10 GB
		MaxPinnedBytesPerService:  4 * 1024 * 1024 * 1024,  // 4 GB
		MaxReservationsGlobal:     20,
		MaxReservationsPerService: 5,
		MaxSegmentsPerReservation: 10000,
		MaxLeaseDuration:          10 * time.Minute,
	}
}

// Reservation represents an atomic, lease-bound hold on a set of ringbuffer segments.
type Reservation struct {
	ID         string       `json:"id"`
	OwnerID    string       `json:"owner_id"`
	ServiceRef string       `json:"service_ref"`
	SegmentIDs []SegmentID  `json:"segment_ids"`
	Start      time.Time    `json:"start"`
	End        time.Time    `json:"end"`
	Status     Completeness `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
	ExpiresAt  time.Time    `json:"expires_at"`
	TotalBytes int64        `json:"total_bytes"`
}

// ReservationManager defines the authoritative interface for probing, reserving, and renewing segments.
type ReservationManager interface {
	ProbeRange(serviceRef string, start, end time.Time) (RangeProbe, error)
	ReserveRange(serviceRef string, start, end time.Time, ownerID string, leaseDuration time.Duration) (Reservation, error)
	GetReservation(reservationID string) (Reservation, error)
	ListReservedSegments(reservationID string) ([]SegmentHandle, error)
	RenewReservation(reservationID string, leaseDuration time.Duration) error
	ReleaseReservation(reservationID string) error
}
