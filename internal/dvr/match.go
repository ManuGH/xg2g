// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MatchResult contains the outcome of a rule match against an EPG item.
type MatchResult struct {
	Matched bool
	Reasons []string
}

// Matches evaluates if a rule applies to a given EPG item.
// It checks Context (Channel), Content (Title), and Time (Day/Window).
// epgStart and epgEnd should already be converted to Local time if the rule is expected to work in local time.
// However, EPG usually comes with UTC or Local. The engine must ensure consistency.
// Here we assume `epgStart` is the time we want to check against the rule (e.g. the show's start time).
func (r *SeriesRule) Matches(epgTitle string, epgChannelRef string, epgStart time.Time) MatchResult {
	res := MatchResult{Matched: true}

	// 1. Channel Match
	if r.ChannelRef != "" {
		// Simple containment or strict match? Usually strict for recording rules.
		// Ignoring case is safer.
		if !strings.EqualFold(r.ChannelRef, epgChannelRef) {
			return MatchResult{Matched: false, Reasons: []string{"channel mismatch"}}
		}
		res.Reasons = append(res.Reasons, "channel match")
	}

	// 2. Title Match (Keyword)
	if r.Keyword != "" {
		if !strings.Contains(strings.ToLower(epgTitle), strings.ToLower(r.Keyword)) {
			return MatchResult{Matched: false, Reasons: []string{"keyword mismatch"}}
		}
		res.Reasons = append(res.Reasons, "keyword match")
	}

	// 3. Day Match
	if len(r.Days) > 0 {
		day := int(epgStart.Weekday()) // 0=Sunday
		found := false
		for _, d := range r.Days {
			if d == day {
				found = true
				break
			}
		}
		if !found {
			return MatchResult{Matched: false, Reasons: []string{"day mismatch"}}
		}
		res.Reasons = append(res.Reasons, "day match")
	}

	// 4. Time Window Match
	if r.StartWindow != "" {
		inWindow, err := IsTimeInWindow(epgStart, r.StartWindow)
		if err != nil {
			// Invalid window config -> fail safe? or ignore?
			// Fail matches to prevent unwanted recordings.
			return MatchResult{Matched: false, Reasons: []string{fmt.Sprintf("window config error: %v", err)}}
		}
		if !inWindow {
			return MatchResult{Matched: false, Reasons: []string{"startWindow mismatch"}}
		}
		res.Reasons = append(res.Reasons, "startWindow match")
	}

	return res
}

// IsTimeInWindow checks if time 't' (HH:MM) falls within the window "HHMM-HHMM".
// Supports midnight crossing (e.g., "2200-0200").
// Format expected: "HHMM-HHMM" or "HH:MM-HH:MM".
func IsTimeInWindow(t time.Time, windowStr string) (bool, error) {
	// 1. Clean format (remove colons)
	clean := strings.ReplaceAll(windowStr, ":", "")
	parts := strings.Split(clean, "-")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid format")
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return false, err
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return false, err
	}

	// Convert times to minutes from midnight
	startMins := (start/100)*60 + (start % 100)
	endMins := (end/100)*60 + (end % 100)

	tMins := t.Hour()*60 + t.Minute()

	if startMins < endMins {
		// Standard window (e.g. 1800-2000)
		// Inclusive start, exclusive end
		return tMins >= startMins && tMins < endMins, nil
	} else if startMins > endMins {
		// Midnight crossing (e.g. 2200-0200)
		// t is valid if it is >= start (22:00..23:59) OR < end (00:00..01:59)
		return tMins >= startMins || tMins < endMins, nil
	} else {
		// Empty window? or entire day? Treating 0000-0000 as "never" for safety.
		return false, nil
	}
}
