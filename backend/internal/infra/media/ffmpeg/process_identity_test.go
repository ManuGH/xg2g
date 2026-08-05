// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"
	"time"
)

func TestTranscodeProcessIdentity(t *testing.T) {
	var zero TranscodeProcessIdentity
	if !zero.IsZero() {
		t.Errorf("expected zero struct to be IsZero()")
	}

	now := time.Now()
	ident := NewProcessIdentity("job-123", 1, 4567, now)

	if ident.IsZero() {
		t.Errorf("expected initialized struct not to be IsZero()")
	}
	if ident.JobID != "job-123" {
		t.Errorf("expected JobID job-123, got %s", ident.JobID)
	}
	if ident.Generation != 1 {
		t.Errorf("expected Generation 1, got %d", ident.Generation)
	}
	if ident.PID != 4567 {
		t.Errorf("expected PID 4567, got %d", ident.PID)
	}
	if !ident.StartedAt.Equal(now) {
		t.Errorf("expected StartedAt %v, got %v", now, ident.StartedAt)
	}
}
