package model

import (
	"fmt"
	"time"
)

// TimerTruth represents the essential truth signals from a timer (OpenWebIf/Enigma2).
type TimerTruth struct {
	Begin      time.Time
	End        time.Time
	ServiceRef string
	Name       string
	JustPlay   bool // If true, this is a zap timer, not a recording
	Disabled   bool // If true, timer is deactivated
	Running    bool // Explicit flag if the receiver says it's recording
}

const (
	// MinBytesThreshold defines the minimum size for a file to be considered a valid recording "content"
	// and not just an empty placeholder or failed start.
	MinBytesThreshold = 10 * 1024 * 1024 // 10MB

	// PostRollGracePeriod defines the time after timer end where we tolerate "finishing up".
	// If timer is past end + grace, and file is missing/small, it's failed.
	PostRollGracePeriod = 120 * time.Second
)

// DeriveRecordingStatus calculates the authoritative status of a recording
// based on strict truth signals from the file system and the timer schedule.
//
// Priority Order (Stop-the-line):
// 1. FILTER: Ignored timers (justplay, disabled) are disregarded (timer = nil).
// 2. RECORDING: Active timer + (Running OR File Exists).
// 3. SCHEDULED: Future timer.
// 4. FAILED: Past timer + (File Missing/Small/Partial).
// 5. COMPLETED: File OK + No active timer.
// 6. UNKNOWN: Fallback.
func DeriveRecordingStatus(now time.Time, file FilePresenceClass, timer *TimerTruth) (RecordingStatus, string) {
	// 1. Pre-Filter: Validate Timer Relevance
	if timer != nil {
		if timer.JustPlay {
			// This is not a recording timer. Treat as if no timer exists for this context.
			timer = nil
		} else if timer.Disabled {
			// Policy: Disabled timers are ignored (or could be Scheduled-Disabled in future).
			// For P3-3, we ignore them.
			timer = nil
		}
	}

	// 2. Evaluate Timer-Based States (Recording / Scheduled / Failed)
	if timer != nil {
		windowStart := timer.Begin
		windowEnd := timer.End
		graceEnd := windowEnd.Add(PostRollGracePeriod)

		isNowInWindow := (now.After(windowStart) || now.Equal(windowStart)) && now.Before(windowEnd)
		isFuture := now.Before(windowStart)
		isPastGrace := now.After(graceEnd)

		// A. RECORDING
		// If we are in the time window
		if isNowInWindow {
			// Truth: It IS recording if the receiver says so (Running) OR if we simply see the file present (any size/partial).
			// Note: Even a "Missing" file might be "Starting", but strict truth requires a signal.
			// If Running=true, we trust it even if file is missing (yet).
			if timer.Running {
				return RecordingStatusRecording, "timer_active_running"
			}
			// Fallback: If not explicitly "running" (legacy), but file exists (even small/partial), assume recording.
			if file != FilePresenceMissing {
				return RecordingStatusRecording, "timer_active_file_present"
			}
			// Edge case: Timer active, but file missing and running=false/unknown.
			// Could be "starting" or "failed start".
			// Truth: If it's early in the window, maybe starting.
			// But strictly: If we have no signal, it's technically Scheduled or Unknown.
			// We map to RECORDING if we are deep in window? No, avoid heuristics.
			// Let's stick to: Timer Active + No Proof = UNKNOWN (or SCHEDULED if we treat it as pending execution).
			// User Logic says: "Timer active + file ok => recording".
			// "Timer active + file missing but running=true => recording".
			// Implementation decision: Return UNKNOWN if we have absolutely no physical proof.
			return RecordingStatusUnknown, "timer_active_no_signal"
		}

		// B. SCHEDULED
		if isFuture {
			return RecordingStatusScheduled, "timer_future"
		}

		// C. FAILED (Critical: Must be checked before assuming Completed)
		// If timer is definitively over (past grace) AND we don't have a valid file.
		if isPastGrace {
			if file == FilePresenceMissing {
				return RecordingStatusFailed, "timer_past_file_missing"
			}
			if file == FilePresenceSmall {
				return RecordingStatusFailed, fmt.Sprintf("timer_past_file_small_<%dMB", MinBytesThreshold/1024/1024)
			}
			// Optional: Treat Partial as failed? Usually yes if timer is over.
			if file == FilePresencePartial {
				return RecordingStatusFailed, "timer_past_file_partial"
			}
		}
	}

	// 3. Evaluate File-Based States (Completed)
	// If we are here, either:
	// - No timer matched
	// - Timer matched but was past grace and file was NOT missing/small/partial (i.e., File is OK).

	// If File is OK (>=10MB), and we are not in a recording state (checked above), it's Completed.
	if file == FilePresenceOK {
		return RecordingStatusCompleted, "file_ok_no_active_timer"
	}

	// 4. Residual States
	// File is Present but Small/Partial, and no active timer to justify it?
	// Likely a failed leftover or debris.
	if file == FilePresenceSmall || file == FilePresencePartial {
		return RecordingStatusFailed, "file_artifact_no_timer"
	}

	return RecordingStatusUnknown, "no_signal"
}
