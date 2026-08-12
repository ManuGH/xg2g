// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy

import (
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type GuardianState string

const (
	GuardianStatusOK      GuardianState = "ok"
	GuardianStatusWarning GuardianState = "warning"
	GuardianStatusBlock   GuardianState = "block"
)

type ActiveStreamWatch struct {
	SessionID         string
	UserID            string
	Role              identity.Role
	ProfileID         string
	ServiceRef        string
	CurrentEventID    string
	CurrentEventEnd   time.Time
	MaxParentalRating int
	UnknownPolicy     string
	DailyCutoffEnd    string // e.g. "19:00"
	Timezone          string // e.g. "Europe/Vienna"
}

type GuardianDecision struct {
	State            GuardianState `json:"state"`
	Reason           string        `json:"reason,omitempty"`
	SecondsRemaining int           `json:"secondsRemaining,omitempty"`
}

// EvaluateStreamStatus evaluates if an active playback stream is clean, approaching boundary (warning), or violating policy (block).
func EvaluateStreamStatus(watch *ActiveStreamWatch, now time.Time, currentEvent *openwebif.EPGEvent, overrideRating *int) GuardianDecision {
	// 1. Evaluate Daily Cutoff Window
	if watch.DailyCutoffEnd != "" {
		loc := time.UTC
		if watch.Timezone != "" {
			if l, err := time.LoadLocation(watch.Timezone); err == nil {
				loc = l
			}
		}
		nowInTZ := now.In(loc)
		currentHM := nowInTZ.Format("15:04")

		// If current time reaches or exceeds cutoff
		if currentHM >= watch.DailyCutoffEnd {
			return GuardianDecision{
				State:  GuardianStatusBlock,
				Reason: fmt.Sprintf("Daily access time window ended at %s", watch.DailyCutoffEnd),
			}
		}

		// Check if within 30 seconds of cutoff
		cutoffToday, err := time.ParseInLocation("15:04", watch.DailyCutoffEnd, loc)
		if err == nil {
			cutoffTime := time.Date(nowInTZ.Year(), nowInTZ.Month(), nowInTZ.Day(), cutoffToday.Hour(), cutoffToday.Minute(), 0, 0, loc)
			diff := cutoffTime.Sub(nowInTZ)
			if diff > 0 && diff <= 30*time.Second {
				return GuardianDecision{
					State:            GuardianStatusWarning,
					Reason:           fmt.Sprintf("Daily access window ends at %s", watch.DailyCutoffEnd),
					SecondsRemaining: int(diff.Seconds()),
				}
			}
		}
	}

	// 2. Evaluate Event Rating Shift
	if currentEvent != nil {
		eventIDStr := fmt.Sprintf("%d", currentEvent.ID)
		var manualMap map[string]int
		if overrideRating != nil {
			manualMap = map[string]int{eventIDStr: *overrideRating}
		}

		desc := currentEvent.Description
		if desc == "" {
			desc = currentEvent.LongDesc
		}

		res := epg.ResolveEPGRating("", currentEvent.Title, desc, manualMap, eventIDStr)
		allowed, _ := epg.IsRatingAllowed(res.Rating, res.IsUnknown, watch.MaxParentalRating, watch.UnknownPolicy)

		// Test if current event rating is allowed
		if !allowed {
			startTime := time.Unix(currentEvent.Begin, 0).UTC()
			if currentEvent.Begin > 0 && now.Before(startTime) {
				diff := startTime.Sub(now)
				if diff <= 30*time.Second {
					return GuardianDecision{
						State:            GuardianStatusWarning,
						Reason:           fmt.Sprintf("Upcoming program '%s' requires parental rating %d+", currentEvent.Title, res.Rating),
						SecondsRemaining: int(diff.Seconds()),
					}
				}
			} else {
				return GuardianDecision{
					State:  GuardianStatusBlock,
					Reason: fmt.Sprintf("Program '%s' exceeds profile parental rating limit (%d+)", currentEvent.Title, watch.MaxParentalRating),
				}
			}
		}
	}

	return GuardianDecision{
		State: GuardianStatusOK,
	}
}

// ReconcileStream recalibrates stream boundary timers against updated EPG event schedules.
func ReconcileStream(watch *ActiveStreamWatch, updatedEPG *openwebif.EPGEvent, now time.Time) GuardianDecision {
	if watch == nil {
		return GuardianDecision{State: GuardianStatusOK}
	}

	if updatedEPG != nil {
		watch.CurrentEventID = fmt.Sprintf("%d", updatedEPG.ID)
		if updatedEPG.Begin > 0 && updatedEPG.Duration > 0 {
			watch.CurrentEventEnd = time.Unix(updatedEPG.Begin+updatedEPG.Duration, 0).UTC()
		}
	}

	return EvaluateStreamStatus(watch, now, updatedEPG, nil)
}
