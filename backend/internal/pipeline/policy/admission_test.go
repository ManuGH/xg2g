// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy_test

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
	"github.com/stretchr/testify/assert"
)

func TestHouseholdResourceAdmission_LimitsAndTunerSharing(t *testing.T) {
	adm := policy.NewHouseholdResourceAdmission()

	pol := &identity.HouseholdResourcePolicy{
		MaxConcurrentLiveServices: 2,
		MaxConcurrentViewers:      3,
		MaxParallelRecordings:     2,
		MaxParallelTranscodes:     1,
		PreemptionEnabled:         true,
	}

	// 1. First viewer tunes ORF 1
	req1 := policy.AdmissionRequest{
		SessionID:   "sess_1",
		UserID:      "usr_papa",
		Role:        identity.RoleAdmin,
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	dec1 := adm.EvaluateAndReserve(req1, pol)
	assert.True(t, dec1.Allowed)

	// 2. Second viewer tunes SAME ORF 1 service (Shared Tuner Exception)
	req2 := policy.AdmissionRequest{
		SessionID:   "sess_2",
		UserID:      "usr_frau",
		Role:        identity.RoleMember,
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	dec2 := adm.EvaluateAndReserve(req2, pol)
	assert.True(t, dec2.Allowed, "Shared tuner stream must be allowed")

	// 3. Third viewer tunes RTL (2nd distinct live service)
	req3 := policy.AdmissionRequest{
		SessionID:   "sess_3",
		UserID:      "usr_guest",
		Role:        identity.RoleGuest,
		ServiceRef:  "1:0:19:2E9B:411:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	dec3 := adm.EvaluateAndReserve(req3, pol)
	assert.True(t, dec3.Allowed)

	// 4. Fourth viewer tries to tune ZDF (3rd live service, max = 2) -> Guest (sess_3) should be preempted by Admin if Admin requests!
	req4 := policy.AdmissionRequest{
		SessionID:   "sess_4",
		UserID:      "usr_admin2",
		Role:        identity.RoleAdmin,
		ServiceRef:  "1:0:19:2B66:3F2:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	dec4 := adm.EvaluateAndReserve(req4, pol)
	assert.True(t, dec4.Allowed)
	assert.Equal(t, "sess_3", dec4.DisplacedSessionID, "Guest session sess_3 must be preempted by Admin req4")

	// 5. Release session
	adm.ReleaseSession("sess_1")
	adm.ReleaseSession("sess_2")
	adm.ReleaseSession("sess_4")
}

func TestIssueAdmissionTicket_AndPreemptionTieBreaker(t *testing.T) {
	adm := policy.NewHouseholdResourceAdmission()
	pol := &identity.HouseholdResourcePolicy{
		MaxConcurrentLiveServices: 1,
		MaxConcurrentViewers:      2,
		PreemptionEnabled:         true,
	}

	reqGuest1 := policy.AdmissionRequest{
		SessionID:   "sess_g1",
		UserID:      "usr_g1",
		Role:        identity.RoleGuest,
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	tkt1, dec1 := adm.IssueAdmissionTicket(reqGuest1, pol)
	assert.True(t, dec1.Allowed)
	assert.NotNil(t, tkt1)
	assert.Equal(t, policy.TicketStatusIssued, tkt1.Status)
	assert.True(t, strings.HasPrefix(tkt1.TicketID, "tkt_"))

	reqGuest2 := policy.AdmissionRequest{
		SessionID:   "sess_g2",
		UserID:      "usr_g2",
		Role:        identity.RoleGuest,
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	_, dec2 := adm.IssueAdmissionTicket(reqGuest2, pol)
	assert.True(t, dec2.Allowed)

	// Admin requests new live service -> Both g1 and g2 are guests (same priority rank).
	// Youngest session (sess_g2) must be preempted by tie-breaker!
	reqAdmin := policy.AdmissionRequest{
		SessionID:   "sess_admin",
		UserID:      "usr_admin",
		Role:        identity.RoleAdmin,
		ServiceRef:  "1:0:19:2E9B:411:1:C00000:0:0:0:",
		RequestType: policy.AdmissionRequestLiveTV,
	}
	tktAdmin, decAdmin := adm.IssueAdmissionTicket(reqAdmin, pol)
	assert.True(t, decAdmin.Allowed)
	assert.NotNil(t, tktAdmin)
	assert.Equal(t, "sess_g2", decAdmin.DisplacedSessionID, "Tie-breaker must preempt youngest guest session sess_g2")
}
