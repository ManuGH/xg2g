// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"sync"
	"time"
)

// DefaultReservationWindow defines how far in advance of a scheduled timer recording RF capacity is locked (default: 5 minutes).
const DefaultReservationWindow = 5 * time.Minute

// RecordingReservation represents a scheduled DVR timer recording requiring future receiver front-end capacity.
type RecordingReservation struct {
	ID          string      `json:"id"`
	ServiceRef  string      `json:"serviceRef"`
	MultiplexID MultiplexID `json:"multiplexId"`
	StartTime   time.Time   `json:"startTime"`
	EndTime     time.Time   `json:"endTime"`
	Title       string      `json:"title"`
	Priority    Priority    `json:"priority"`
}

// ReservationPlanner tracks upcoming timer recordings to prevent newly started low-priority live streams
// from preempting or starving scheduled recordings.
type ReservationPlanner struct {
	mu           sync.RWMutex
	window       time.Duration
	reservations map[string]RecordingReservation
}

// NewReservationPlanner initializes a recording reservation planner with the specified lookahead window.
func NewReservationPlanner(window time.Duration) *ReservationPlanner {
	if window <= 0 {
		window = DefaultReservationWindow
	}
	return &ReservationPlanner{
		window:       window,
		reservations: make(map[string]RecordingReservation),
	}
}

// Window returns the lookahead reservation window duration.
func (p *ReservationPlanner) Window() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.window
}

// SetWindow updates the lookahead window.
func (p *ReservationPlanner) SetWindow(w time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if w > 0 {
		p.window = w
	}
}

// SyncTimers updates the active recording reservations list from Enigma2 timer metadata.
func (p *ReservationPlanner) SyncTimers(timers []RecordingReservation) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.reservations = make(map[string]RecordingReservation, len(timers))
	for _, t := range timers {
		if t.Priority == 0 {
			t.Priority = PriorityUpcomingRecording
		}
		p.reservations[t.ID] = t
	}
}

// UpcomingReservations returns all scheduled recordings that are either currently active
// or starting within the reservation window relative to reference time 'now'.
func (p *ReservationPlanner) UpcomingReservations(now time.Time) []RecordingReservation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cutoff := now.Add(p.window)
	var active []RecordingReservation

	for _, r := range p.reservations {
		// Include if currently running: startTime <= now < endTime
		// OR starting within lookahead window: now <= startTime <= cutoff
		if (r.StartTime.Before(now) && r.EndTime.After(now)) ||
			(!r.StartTime.Before(now) && !r.StartTime.After(cutoff)) {
			active = append(active, r)
		}
	}

	return active
}
