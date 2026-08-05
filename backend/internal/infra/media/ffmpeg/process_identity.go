// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"time"
)

// TranscodeProcessIdentity is an immutable value object representing the operational
// identity of a single execution attempt (generation) of an FFmpeg process.
type TranscodeProcessIdentity struct {
	JobID      string    `json:"job_id"`
	Generation uint64    `json:"generation"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
}

// IsZero reports whether the process identity is uninitialized.
func (t TranscodeProcessIdentity) IsZero() bool {
	return t.JobID == "" || t.Generation == 0
}

// NewProcessIdentity constructs a TranscodeProcessIdentity instance.
func NewProcessIdentity(jobID string, generation uint64, pid int, startedAt time.Time) TranscodeProcessIdentity {
	return TranscodeProcessIdentity{
		JobID:      jobID,
		Generation: generation,
		PID:        pid,
		StartedAt:  startedAt,
	}
}
