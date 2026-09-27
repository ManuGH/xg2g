package v3

import (
	"testing"
	"time"
)

func TestValidateSessionPlaybackTraceAcceptsBoundedOrderedWindow(t *testing.T) {
	now := time.Date(2026, time.September, 27, 10, 0, 0, 0, time.UTC)
	batch := ClientPlaybackTraceBatch{
		ObservedAt: now,
		Events: []ClientPlaybackTraceEvent{
			{Sequence: 1, ElapsedMs: 0, Stage: ClientTraceStageLifecycle, Event: ClientTracePlaybackStarted},
			{Sequence: 2, ElapsedMs: 2_500, Stage: ClientTraceStageRender, Event: ClientTraceFrameLate},
		},
	}
	if err := validateSessionPlaybackTrace(batch, now); err != nil {
		t.Fatalf("expected valid trace, got %v", err)
	}
}

func TestValidateSessionPlaybackTraceRejectsInvalidWindows(t *testing.T) {
	now := time.Date(2026, time.September, 27, 10, 0, 0, 0, time.UTC)
	valid := ClientPlaybackTraceBatch{
		ObservedAt: now,
		Events: []ClientPlaybackTraceEvent{
			{Sequence: 1, ElapsedMs: 0, Stage: ClientTraceStageLifecycle, Event: ClientTracePlaybackStarted},
			{Sequence: 2, ElapsedMs: 1_000, Stage: ClientTraceStageNetwork, Event: ClientTraceHttpResponse},
		},
	}
	tests := map[string]func(*ClientPlaybackTraceBatch){
		"empty":          func(batch *ClientPlaybackTraceBatch) { batch.Events = nil },
		"out of order":   func(batch *ClientPlaybackTraceBatch) { batch.Events[1].Sequence = 1 },
		"stage mismatch": func(batch *ClientPlaybackTraceBatch) { batch.Events[1].Stage = ClientTraceStageDecode },
		"older than window": func(batch *ClientPlaybackTraceBatch) {
			batch.Events[0].ElapsedMs = 0
			batch.Events[1].ElapsedMs = 30_001
		},
		"stale observed at":  func(batch *ClientPlaybackTraceBatch) { batch.ObservedAt = now.Add(-3 * time.Minute) },
		"future observed at": func(batch *ClientPlaybackTraceBatch) { batch.ObservedAt = now.Add(time.Minute) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			batch := valid
			batch.Events = append([]ClientPlaybackTraceEvent(nil), valid.Events...)
			mutate(&batch)
			if err := validateSessionPlaybackTrace(batch, now); err == nil {
				t.Fatal("expected invalid trace to be rejected")
			}
		})
	}
}
