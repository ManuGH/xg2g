// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/receiverusage"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

var fixedTime = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

func TestAdmissionAdapter_BuildUsageRequest_VerifiedProtected(t *testing.T) {
	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	req := BuildUsageRequest(sCtx, "rec-1", "owner-1", true, true, fixedTime)

	if req.ReceiverID != "rec-1" {
		t.Fatalf("expected rec-1, got %s", req.ReceiverID)
	}
	if req.Intent != receiverusage.IntentLive {
		t.Fatalf("expected IntentLive, got %v", req.Intent)
	}
	if req.Access.Class != receiverusage.AccessCapacityRestricted {
		t.Fatalf("expected AccessCapacityRestricted, got %v", req.Access.Class)
	}
}

func TestAdmissionAdapter_BuildUsageRequest_VerifiedFTA(t *testing.T) {
	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	req := BuildUsageRequest(sCtx, "rec-1", "owner-1", true, false, fixedTime)

	if req.Access.Class != receiverusage.AccessCapacityNone {
		t.Fatalf("expected AccessCapacityNone, got %v", req.Access.Class)
	}
}

func TestAdmissionAdapter_BuildUsageRequest_Unknown(t *testing.T) {
	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	req := BuildUsageRequest(sCtx, "rec-1", "owner-1", false, false, fixedTime)

	if req.Access.Class != receiverusage.AccessCapacityUnknown {
		t.Fatalf("expected AccessCapacityUnknown, got %v", req.Access.Class)
	}
}

func TestAdmissionAdapter_BuildSystemSnapshot_MergesXg2gAndOpenWebif(t *testing.T) {
	activeXg2g := []*model.SessionRecord{
		{
			SessionID:     "sess-xg2g-1",
			ServiceRef:    "1:0:19:1111:3FB:1:C00000:0:0:0:",
			CreatedAtUnix: fixedTime.Unix(),
			ContextData: map[string]string{
				"receiver_id":   "rec-1",
				"is_restricted": "true",
				"owner":         "user-1",
				"mode":          model.ModeLive,
			},
		},
	}

	openwebifActs := []OpenWebifActivity{
		{
			ActivityID:       "ow-act-1",
			ServiceReference: "1:0:19:2222:3FB:1:C00000:0:0:0:",
			IsProtected:      true,
			IsRecording:      false,
			StartedAt:        fixedTime.Add(-5 * time.Minute),
		},
	}

	snap := BuildSystemSnapshot("rec-1", activeXg2g, openwebifActs)

	if len(snap.ActiveSessions) != 2 {
		t.Fatalf("expected 2 active sessions in snapshot, got %d", len(snap.ActiveSessions))
	}
	if snap.ActiveSessions[0].SessionID != "sess-xg2g-1" {
		t.Fatalf("expected first session sess-xg2g-1, got %s", snap.ActiveSessions[0].SessionID)
	}
	if snap.ActiveSessions[1].SessionID != "openwebif-ow-act-1" {
		t.Fatalf("expected second session openwebif-ow-act-1, got %s", snap.ActiveSessions[1].SessionID)
	}
}

func TestAdmissionAdapter_BuildSystemSnapshot_FiltersByReceiverID(t *testing.T) {
	activeXg2g := []*model.SessionRecord{
		{
			SessionID:     "sess-rec1",
			ServiceRef:    "1:0:19:1111:3FB:1:C00000:0:0:0:",
			CreatedAtUnix: fixedTime.Unix(),
			ContextData:   map[string]string{"receiver_id": "rec-1", "owner": "user-1"},
		},
		{
			SessionID:     "sess-rec2",
			ServiceRef:    "1:0:19:2222:3FB:1:C00000:0:0:0:",
			CreatedAtUnix: fixedTime.Unix(),
			ContextData:   map[string]string{"receiver_id": "rec-2", "owner": "user-2"}, // Foreign receiver
		},
	}

	snap := BuildSystemSnapshot("rec-1", activeXg2g, nil)

	if len(snap.ActiveSessions) != 1 {
		t.Fatalf("expected 1 active session for rec-1, got %d", len(snap.ActiveSessions))
	}
	if snap.ActiveSessions[0].SessionID != "sess-rec1" {
		t.Fatalf("expected sess-rec1, got %s", snap.ActiveSessions[0].SessionID)
	}
}
