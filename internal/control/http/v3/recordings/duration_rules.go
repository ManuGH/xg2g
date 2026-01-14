package recordings

import (
	"errors"
	"fmt"
	"time"
)

// DurationSource indicates where the duration came from.
// Keep this list stable and ordered by trustworthiness.
type DurationSource string

const (
	DurationUnknown   DurationSource = "UNKNOWN"
	DurationContainer DurationSource = "CONTAINER" // e.g., ffprobe on finalized mp4/mkv
	DurationIndex     DurationSource = "INDEX"     // e.g., derived from HLS EXTINF sum (weak unless final)
	DurationMetadata  DurationSource = "METADATA"  // e.g., Enigma2 metadata (often unreliable)
)

// DurationUpdate is an attempted update to duration, with provenance and “finalness”.
type DurationUpdate struct {
	Seconds int64
	Source  DurationSource

	// If true, this update claims finality. Only allowed if conditions are met.
	Final bool
}

// DurationPolicy contains strict rules; keep it config-lite.
// If you later expose knobs, that is “config as product surface”.
type DurationPolicy struct {
	// MinimumDeltaSeconds avoids flapping due to tiny probe differences.
	MinimumDeltaSeconds int64 // e.g., 1

	// AllowIndexFinal indicates whether index-derived durations may ever be final.
	// Default: false (strongly recommended).
	AllowIndexFinal bool
}

// ApplyDurationUpdate enforces monotonicity and finality lock.
// It atomically transitions to READY_FINAL if the update claims finality, ensuring validity.
// Returns (changed bool, err error).
func ApplyDurationUpdate(meta *RecordingMeta, pol DurationPolicy, upd DurationUpdate, now time.Time) (bool, error) {
	if meta == nil {
		return false, errors.New("duration: meta is nil")
	}
	if upd.Seconds < 0 {
		return false, errors.New("duration: negative seconds")
	}
	if upd.Source == DurationUnknown {
		return false, errors.New("duration: invalid source UNKNOWN")
	}

	// Finality lock: once READY_FINAL, duration must not change.
	if meta.State == StateReadyFinal {
		// Idempotent allow: same exact value + Final=true
		if meta.Facts.DurationSeconds != nil && *meta.Facts.DurationSeconds == upd.Seconds && upd.Final {
			return false, nil
		}
		return false, fmt.Errorf("duration: cannot update duration in READY_FINAL (have=%v upd=%d)", meta.Facts.DurationSeconds, upd.Seconds)
	}

	// Guard: “final” updates must satisfy stronger conditions.
	if upd.Final {
		if upd.Source != DurationContainer {
			// Strong default: only container duration can be final.
			// If you later want to allow other sources, do it explicitly (policy flag).
			if !pol.AllowIndexFinal || upd.Source != DurationIndex {
				return false, fmt.Errorf("duration: final update not allowed from source=%s", upd.Source)
			}
		}
	}

	// Monotonicity + minimum delta
	if meta.Facts.DurationSeconds != nil {
		prev := *meta.Facts.DurationSeconds

		// Do not allow shrinkage.
		if upd.Seconds < prev {
			return false, fmt.Errorf("duration: monotonicity violated prev=%d upd=%d", prev, upd.Seconds)
		}

		// Ignore tiny changes unless finalization requires it.
		delta := upd.Seconds - prev
		if delta < pol.MinimumDeltaSeconds && !upd.Final {
			return false, nil
		}

		// No-op
		if upd.Seconds == prev && meta.Facts.DurationFinal == upd.Final {
			return false, nil
		}
	}

	// Apply
	meta.Facts.DurationSeconds = ptr(upd.Seconds)
	meta.Facts.DurationFinal = upd.Final

	// If duration is marked final, state MUST become READY_FINAL to satisfy invariants.
	// We perform this transition atomically here to ensure the returned object is valid.
	if upd.Final && meta.State != StateReadyFinal {
		err := meta.ApplyTransition(TransitionEvent{
			From:   meta.State,
			To:     StateReadyFinal,
			Reason: "duration_finalized",
			AtUTC:  now,
		})
		if err != nil {
			return false, err
		}
	}

	return true, meta.Validate()
}

func ptr(v int64) *int64 { return &v }

// ShouldFinalizeState decides if current meta can transition to READY_FINAL,
// given the duration flags and other evidence (playlist present, final artifact present, etc.).
// Keep this conservative.
func ShouldFinalizeState(meta *RecordingMeta) (bool, error) {
	if meta == nil {
		return false, errors.New("duration: meta nil")
	}
	if meta.Facts.DurationSeconds == nil {
		return false, nil
	}
	if !meta.Facts.DurationFinal {
		return false, nil
	}
	// Optional: require PlaylistPath present or final artifact path present.
	// If you have a separate “FinalArtifactPath”, check it here.
	return true, nil
}
