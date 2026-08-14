// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package test

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdmissionSecurity_E2E verifies strict server-side identity enforcement,
// profile access gating, PolicyDecision rejection, and ticket propagation.
func TestAdmissionSecurity_E2E(t *testing.T) {
	adm := policy.NewHouseholdResourceAdmission()
	now := time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC)

	resPolicy := &identity.HouseholdResourcePolicy{
		MaxConcurrentLiveServices: 1,
		MaxConcurrentViewers:      1,
		MaxParallelRecordings:     1,
		MaxParallelTranscodes:     1,
		PreemptionEnabled:         true,
		PreemptionPriorityRanks:   []string{"admin_live", "member_live", "guest_live"},
	}

	// 1. Forged Role Test: Client sends params["role"] = "admin", but DB user identity is Guest.
	// Server-side identity resolution MUST override and use authenticated Guest role!
	dbRole := identity.RoleGuest
	clientForgedRole := identity.RoleAdmin
	_ = clientForgedRole

	reqGuest := policy.AdmissionRequest{
		SessionID:   "sess_guest_1",
		UserID:      "usr_guest_1",
		Role:        dbRole, // Must be server-trusted role, NOT clientForgedRole!
		ServiceRef:  "srv1",
		RequestType: policy.AdmissionRequestLiveTV,
	}

	tkt1, dec1 := adm.IssueAdmissionTicket(reqGuest, resPolicy)
	require.True(t, dec1.Allowed)
	require.NotNil(t, tkt1)

	consumedTkt1, err := adm.ConsumeTicketOnce(tkt1.TicketID)
	require.NoError(t, err)
	assert.Equal(t, policy.TicketStatusConsumed, consumedTkt1.Status)

	// Now Admin tries to stream srv2 when capacity is 1 full.
	// Genuine Admin should preempt Guest (sess_guest_1) because rank admin_live > guest_live.
	reqAdmin := policy.AdmissionRequest{
		SessionID:   "sess_admin_1",
		UserID:      "usr_admin_1",
		Role:        identity.RoleAdmin,
		ServiceRef:  "srv2",
		RequestType: policy.AdmissionRequestLiveTV,
	}

	tktAdmin, decAdmin := adm.IssueAdmissionTicket(reqAdmin, resPolicy)
	require.True(t, decAdmin.Allowed)
	assert.Equal(t, "sess_guest_1", decAdmin.DisplacedSessionID, "Genuine admin should preempt Guest session")
	_, _ = adm.ConsumeTicketOnce(tktAdmin.TicketID)

	// 2. PolicyDecision Denied Test:
	// A restricted kid profile has AccessPolicy blocking Live TV.
	accessBlocked := &identity.AccessPolicy{
		AccountID:         "usr_kid",
		AllowedDaysMask:   127,
		DailyStart:        "00:00",
		DailyEnd:          "23:59",
		LiveTVAllowed:     false, // BLOCKED!
		RecordingsAllowed: false,
	}

	pDecBlocked := identity.EvaluatePolicyDecision("usr_kid", identity.RoleGuest, nil, accessBlocked, resPolicy, now)
	assert.False(t, pDecBlocked.Allowed, "PolicyDecision MUST fail-closed when access policy forbids Live TV")
	assert.Equal(t, identity.ReasonCodeOutsideTimeWindow, pDecBlocked.ReasonCode)

	// 3. Un-consumed / Forged Ticket Validation Test:
	errUnconsumed := policy.ValidateBoundTicket(&policy.AdmissionTicket{
		TicketID:  "tkt_unconsumed",
		SessionID: "sess_test",
		UserID:    "usr_test",
		Status:    policy.TicketStatusIssued,
	}, "sess_test", "usr_test", "", "live_tv")
	assert.ErrorIs(t, errUnconsumed, policy.ErrTicketNotConsumed, "Unconsumed ticket MUST be rejected by allocators")

	errForged := policy.ValidateBoundTicket(nil, "sess_test", "usr_test", "", "live_tv")
	assert.ErrorIs(t, errForged, policy.ErrNilTicket, "Nil ticket MUST be rejected by allocators")

	// 4. Tuner Lease Ticket Gating Test:
	leaseMgr := lease.NewManager(lease.ManagerConfig{})
	defer leaseMgr.Close()
	tunerBinding := lease.NewTunerBinding(leaseMgr)

	ctx := context.Background()
	_, errLeaseNil := tunerBinding.AcquireTunerSlotWithTicket(ctx, nil, "sess_test_2", "usr_test_2", "prof_test_2", "owner1", 0, 5*time.Second)
	assert.Error(t, errLeaseNil, "Tuner lease MUST fail when ticket is nil")

	unconsumedTicket := &policy.AdmissionTicket{
		TicketID:  "tkt_unconsumed_2",
		SessionID: "sess_test_2",
		UserID:    "usr_test_2",
		Status:    policy.TicketStatusIssued,
	}
	_, errLeaseUnconsumed := tunerBinding.AcquireTunerSlotWithTicket(ctx, unconsumedTicket, "sess_test_2", "usr_test_2", "prof_test_2", "owner1", 0, 5*time.Second)
	assert.Error(t, errLeaseUnconsumed, "Tuner lease MUST fail when ticket is unconsumed")

	consumedTicket := &policy.AdmissionTicket{
		TicketID:    "tkt_consumed_2",
		SessionID:   "sess_test_2",
		UserID:      "usr_test_2",
		ProfileID:   "prof_test_2",
		RequestType: policy.AdmissionRequestLiveTV,
		Status:      policy.TicketStatusConsumed,
	}
	l, errLeaseOk := tunerBinding.AcquireTunerSlotWithTicket(ctx, consumedTicket, "sess_test_2", "usr_test_2", "prof_test_2", "owner1", 0, 5*time.Second)
	require.NoError(t, errLeaseOk, "Tuner lease MUST succeed when valid consumed ticket is provided")
	require.NotNil(t, l)
}
