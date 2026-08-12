// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy_test

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
)

func TestAdmissionTicket_BypassAndValidationMatrix(t *testing.T) {
	adm := policy.NewHouseholdResourceAdmission()

	req := policy.AdmissionRequest{
		SessionID:    "sess_live_100",
		UserID:       "user_guest_1",
		Role:         identity.RoleGuest,
		ProfileID:    "prof_guest_1",
		ServiceRef:   "1:0:19:283D:3FB:1:C00000:0:0:0:",
		RequestType:  policy.AdmissionRequestLiveTV,
		IsTranscoded: false,
	}

	resPolicy := &identity.HouseholdResourcePolicy{
		MaxConcurrentLiveServices: 3,
		MaxConcurrentViewers:      5,
		MaxParallelRecordings:     4,
		MaxParallelTranscodes:     3,
		PreemptionEnabled:         true,
	}

	// 1. Issue Ticket
	ticket, dec := adm.IssueAdmissionTicket(req, resPolicy)
	if !dec.Allowed || ticket == nil {
		t.Fatalf("expected allowed ticket, got dec=%+v", dec)
	}

	if !strings.HasPrefix(ticket.TicketID, "tkt_") || len(ticket.TicketID) < 10 {
		t.Fatalf("expected CSPRNG ticket ID, got %s", ticket.TicketID)
	}

	// 2. Validate un-consumed ticket -> FAIL (ErrTicketNotConsumed)
	err := policy.ValidateBoundTicket(ticket, "sess_live_100", "user_guest_1", "prof_guest_1", "live")
	if err != policy.ErrTicketNotConsumed {
		t.Fatalf("expected ErrTicketNotConsumed, got %v", err)
	}

	// 3. Consume ticket once -> SUCCESS
	consumedTkt, err := adm.ConsumeTicketOnce(ticket.TicketID)
	if err != nil || consumedTkt.Status != policy.TicketStatusConsumed {
		t.Fatalf("expected consumed ticket, got err=%v, tkt=%+v", err, consumedTkt)
	}

	// 4. Double Consume -> FAIL (ErrTicketAlreadyConsumed)
	_, err = adm.ConsumeTicketOnce(ticket.TicketID)
	if err != policy.ErrTicketAlreadyConsumed {
		t.Fatalf("expected ErrTicketAlreadyConsumed, got %v", err)
	}

	// 5. Validate nil ticket -> FAIL (ErrNilTicket)
	if err := policy.ValidateBoundTicket(nil, "sess_live_100", "user_guest_1", "prof_guest_1", "live"); err != policy.ErrNilTicket {
		t.Fatalf("expected ErrNilTicket, got %v", err)
	}

	// 6. Validate session ID mismatch -> FAIL (ErrSessionMismatch)
	if err := policy.ValidateBoundTicket(consumedTkt, "wrong_sess", "user_guest_1", "prof_guest_1", "live"); err != policy.ErrSessionMismatch {
		t.Fatalf("expected ErrSessionMismatch, got %v", err)
	}

	// 7. Validate user ID mismatch -> FAIL (ErrUserMismatch)
	if err := policy.ValidateBoundTicket(consumedTkt, "sess_live_100", "wrong_user", "prof_guest_1", "live"); err != policy.ErrUserMismatch {
		t.Fatalf("expected ErrUserMismatch, got %v", err)
	}

	// 8. Validate profile ID mismatch -> FAIL (ErrProfileMismatch)
	if err := policy.ValidateBoundTicket(consumedTkt, "sess_live_100", "user_guest_1", "wrong_prof", "live"); err != policy.ErrProfileMismatch {
		t.Fatalf("expected ErrProfileMismatch, got %v", err)
	}

	// 9. Validate resource class mismatch (e.g. guest_live ticket trying to run DVR worker) -> FAIL (ErrResourceClassMismatch)
	if err := policy.ValidateBoundTicket(consumedTkt, "sess_live_100", "user_guest_1", "prof_guest_1", "dvr"); err != policy.ErrResourceClassMismatch {
		t.Fatalf("expected ErrResourceClassMismatch, got %v", err)
	}

	// 10. Valid bound ticket verification -> SUCCESS
	if err := policy.ValidateBoundTicket(consumedTkt, "sess_live_100", "user_guest_1", "prof_guest_1", "live"); err != nil {
		t.Fatalf("expected valid bound ticket check, got %v", err)
	}

	// 11. Idempotent Release Ticket (Multiple callers safe)
	adm.ReleaseAdmissionTicket(ticket.TicketID)
	adm.ReleaseAdmissionTicket(ticket.TicketID)

	// 12. Validate released ticket -> FAIL (ErrTicketReleased)
	if err := policy.ValidateBoundTicket(consumedTkt, "sess_live_100", "user_guest_1", "prof_guest_1", "live"); err != policy.ErrTicketReleased {
		t.Fatalf("expected ErrTicketReleased, got %v", err)
	}
}

func TestAdmissionTicket_ManualVsScheduledRecordingPreemption(t *testing.T) {
	adm := policy.NewHouseholdResourceAdmission()

	resPolicy := &identity.HouseholdResourcePolicy{
		MaxConcurrentLiveServices: 2,
		MaxConcurrentViewers:      5,
		MaxParallelRecordings:     5,
		MaxParallelTranscodes:     5,
		PreemptionEnabled:         true,
		PreemptionPriorityRanks:   []string{"scheduled_recording", "manual_recording", "admin_live", "member_live", "guest_live"},
	}

	// Fill live services: guest_live on srv1 and admin_live on srv2
	tktGuest, dec1 := adm.IssueAdmissionTicket(policy.AdmissionRequest{
		SessionID:   "sess_guest",
		UserID:      "guest1",
		Role:        identity.RoleGuest,
		ServiceRef:  "srv1",
		RequestType: policy.AdmissionRequestLiveTV,
	}, resPolicy)
	if !dec1.Allowed {
		t.Fatalf("guest live failed: %+v", dec1)
	}
	_, _ = adm.ConsumeTicketOnce(tktGuest.TicketID)

	tktAdmin, dec2 := adm.IssueAdmissionTicket(policy.AdmissionRequest{
		SessionID:   "sess_admin",
		UserID:      "admin1",
		Role:        identity.RoleAdmin,
		ServiceRef:  "srv2",
		RequestType: policy.AdmissionRequestLiveTV,
	}, resPolicy)
	if !dec2.Allowed {
		t.Fatalf("admin live failed: %+v", dec2)
	}
	_, _ = adm.ConsumeTicketOnce(tktAdmin.TicketID)

	// Now manual recording tries to acquire a 3rd live service (srv3) when max live services is 2.
	// manual_recording has higher priority than guest_live & admin_live -> should preempt guest_live (lowest priority rank)
	tktManual, dec3 := adm.IssueAdmissionTicket(policy.AdmissionRequest{
		SessionID:   "sess_manual_rec",
		UserID:      "admin1",
		Role:        identity.RoleAdmin,
		ServiceRef:  "srv3",
		RequestType: policy.AdmissionRequestManualRecord,
	}, resPolicy)

	if !dec3.Allowed || dec3.DisplacedSessionID != "sess_guest" {
		t.Fatalf("expected manual recording to preempt guest_live (sess_guest), got dec3=%+v", dec3)
	}
	_, _ = adm.ConsumeTicketOnce(tktManual.TicketID)
}
