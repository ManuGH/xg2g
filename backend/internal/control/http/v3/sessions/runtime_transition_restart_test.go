package sessions

import (
	"testing"
	"time"

	runtimepolicy "github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// A runtime policy step-down stops the session with R_CLIENT_STOP and restarts
// it immediately. The record it leaves behind must say so, otherwise the shared
// ingest would treat it as a user stop and tear the upstream down between the
// stop and the restart.
func TestApplySessionRuntimePolicyTransition_RestartMarksRestartPending(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rec, err := lifecycle.NewReadySessionRecord(now)
	if err != nil {
		t.Fatalf("NewReadySessionRecord: %v", err)
	}
	resolver := profiles.LoadResolver()
	rec.Profile = resolver.Resolve(profiles.ProfileCopy, "", 2700, nil, profiles.GPUBackendNone, profiles.HWAccelOff)

	res, err := ApplySessionRuntimePolicyTransition(rec, runtimepolicy.SessionTransition{
		Kind:     runtimepolicy.SessionTransitionScheduleStepDown,
		FromStep: runtimepolicy.PlaybackStepDirectCopy,
		ToStep:   runtimepolicy.PlaybackStepH2641080p,
	}, now, resolver)
	if err != nil {
		t.Fatalf("ApplySessionRuntimePolicyTransition: %v", err)
	}
	if !res.Restart {
		t.Fatalf("test premise broken: transition did not request a restart (blockers=%v)", res.Blockers)
	}
	if !rec.RestartPending() {
		t.Fatal("a runtime policy restart must mark the session restart-pending")
	}
}

// A transition that does not restart (commit probe) must not touch the marker.
func TestApplySessionRuntimePolicyTransition_NoRestartLeavesMarkerAlone(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rec, err := lifecycle.NewReadySessionRecord(now)
	if err != nil {
		t.Fatalf("NewReadySessionRecord: %v", err)
	}
	res, err := ApplySessionRuntimePolicyTransition(rec, runtimepolicy.SessionTransition{
		Kind: runtimepolicy.SessionTransitionCommitProbe,
	}, now, profiles.LoadResolver())
	if err != nil {
		t.Fatalf("ApplySessionRuntimePolicyTransition: %v", err)
	}
	if res.Restart {
		t.Fatal("commit probe must not restart")
	}
	if rec.ContextData[model.CtxKeyRestartPending] != "" {
		t.Fatal("a non-restart transition must not set the restart marker")
	}
}
