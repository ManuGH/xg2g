package lifecycle

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// The two internal restart paths (client-feedback fallback and runtime policy
// transition) both publish R_CLIENT_STOP and restart the session right away.
// ApplyFallbackRestart is the shared edge they go through, so it must record
// that the stop is not a user stop.
func TestApplyFallbackRestart_MarksRestartPending(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rec, err := NewReadySessionRecord(now)
	if err != nil {
		t.Fatalf("NewReadySessionRecord: %v", err)
	}
	if rec.RestartPending() {
		t.Fatal("a fresh ready session must not be restart-pending")
	}

	ApplyFallbackRestart(rec, now)

	if !rec.RestartPending() {
		t.Fatal("ApplyFallbackRestart must mark the session restart-pending")
	}
	if got := rec.ContextData[model.CtxKeyRestartPending]; got != "1" {
		t.Fatalf("restart marker = %q, want \"1\"", got)
	}
}

func TestApplyFallbackRestart_KeepsExistingContextData(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rec, err := NewReadySessionRecord(now)
	if err != nil {
		t.Fatalf("NewReadySessionRecord: %v", err)
	}
	rec.ContextData = map[string]string{model.CtxKeyClientPath: "hlsjs"}

	ApplyFallbackRestart(rec, now)

	if rec.ContextData[model.CtxKeyClientPath] != "hlsjs" {
		t.Fatal("existing context data must survive the restart marker")
	}
}

// The restarted session reuses the same record. If the marker stayed, a later
// REAL user stop of the restarted session would be treated as a restart and
// lose the immediate upstream release.
func TestResetForFallbackRestart_ClearsRestartPending(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rec, err := NewReadySessionRecord(now)
	if err != nil {
		t.Fatalf("NewReadySessionRecord: %v", err)
	}
	ApplyFallbackRestart(rec, now)
	if !rec.RestartPending() {
		t.Fatal("test premise broken: marker not set")
	}

	ResetForFallbackRestart(rec, now.Add(time.Second))

	if rec.RestartPending() {
		t.Fatal("ResetForFallbackRestart must clear the restart marker")
	}
}

func TestSessionRecord_RestartPending_NilSafe(t *testing.T) {
	var nilRec *model.SessionRecord
	if nilRec.RestartPending() {
		t.Fatal("nil record must not be restart-pending")
	}
	if (&model.SessionRecord{}).RestartPending() {
		t.Fatal("record without context data must not be restart-pending")
	}
	if (&model.SessionRecord{ContextData: map[string]string{model.CtxKeyRestartPending: "0"}}).RestartPending() {
		t.Fatal("marker value other than \"1\" must not count")
	}
}
