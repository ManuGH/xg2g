// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy_test

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResourceClassAllocation_NegativeMatrix verifies that all three core resource classes
// (Tuner/LiveTV, FFmpeg/Transcoder, and DVR/Recordings) strictly enforce valid, consumed, and bound
// AdmissionTickets, failing closed on missing, unconsumed, wrong-context, or released tickets.
func TestResourceClassAllocation_NegativeMatrix(t *testing.T) {
	adm := policy.NewHouseholdResourceAdmission()

	resPolicy := &identity.HouseholdResourcePolicy{
		MaxConcurrentLiveServices: 1, // 1 Tuner
		MaxConcurrentViewers:      5,
		MaxParallelRecordings:     1, // 1 DVR Worker
		MaxParallelTranscodes:     1, // 1 FFmpeg Transcode Session
		PreemptionEnabled:         true,
	}

	// =========================================================================
	// CLASS 1: TUNER / LIVE TV RESOURCE ALLOCATION
	// =========================================================================
	t.Run("Class 1: Tuner/LiveTV Strict Ticket Binding", func(t *testing.T) {
		reqTuner := policy.AdmissionRequest{
			SessionID:    "sess_tuner_1",
			UserID:       "usr_papa",
			Role:         identity.RoleAdmin,
			ProfileID:    "prof_papa",
			ServiceRef:   "1:0:19:132F:3EF:1:C00000:0:0:0:",
			RequestType:  policy.AdmissionRequestLiveTV,
			IsTranscoded: false,
		}

		// 1a. Attempt allocation with NIL ticket -> FAIL
		err := policy.ValidateBoundTicket(nil, reqTuner.SessionID, reqTuner.UserID, reqTuner.ProfileID, string(reqTuner.RequestType))
		assert.Equal(t, policy.ErrNilTicket, err, "NIL ticket for Tuner MUST fail closed")

		// 1b. Issue ticket but do NOT consume -> FAIL (ErrTicketNotConsumed)
		tkt, dec := adm.IssueAdmissionTicket(reqTuner, resPolicy)
		require.True(t, dec.Allowed)
		require.NotNil(t, tkt)

		err = policy.ValidateBoundTicket(tkt, reqTuner.SessionID, reqTuner.UserID, reqTuner.ProfileID, string(reqTuner.RequestType))
		assert.Equal(t, policy.ErrTicketNotConsumed, err, "Unconsumed ticket for Tuner MUST fail closed")

		// 1c. Consume ticket once -> VALID
		consumedTkt, err := adm.ConsumeTicketOnce(tkt.TicketID)
		require.NoError(t, err)
		err = policy.ValidateBoundTicket(consumedTkt, reqTuner.SessionID, reqTuner.UserID, reqTuner.ProfileID, string(reqTuner.RequestType))
		assert.NoError(t, err, "Consumed and bound Tuner ticket MUST pass validation")

		// 1d. Release ticket and attempt allocation -> FAIL (ErrTicketReleased)
		adm.ReleaseAdmissionTicket(tkt.TicketID)
		err = policy.ValidateBoundTicket(consumedTkt, reqTuner.SessionID, reqTuner.UserID, reqTuner.ProfileID, string(reqTuner.RequestType))
		assert.Equal(t, policy.ErrTicketReleased, err, "Released Tuner ticket MUST fail closed")
	})

	// =========================================================================
	// CLASS 2: FFMPEG / TRANSCODER RESOURCE ALLOCATION
	// =========================================================================
	t.Run("Class 2: FFmpeg/Transcoder Strict Capacity & Ticket Binding", func(t *testing.T) {
		reqFFmpeg1 := policy.AdmissionRequest{
			SessionID:    "sess_ffmpeg_1",
			UserID:       "usr_mama",
			Role:         identity.RoleAdmin,
			ProfileID:    "prof_mama",
			ServiceRef:   "1:0:19:283D:3FB:1:C00000:0:0:0:",
			RequestType:  policy.AdmissionRequestLiveTV,
			IsTranscoded: true, // FFmpeg Transcoder active
		}

		// 2a. Issue 1st FFmpeg ticket -> ALLOWED (1/1 transcode capacity)
		tkt1, dec1 := adm.IssueAdmissionTicket(reqFFmpeg1, resPolicy)
		require.True(t, dec1.Allowed)
		require.NotNil(t, tkt1)
		consumedTkt1, err := adm.ConsumeTicketOnce(tkt1.TicketID)
		require.NoError(t, err)

		err = policy.ValidateBoundTicket(consumedTkt1, reqFFmpeg1.SessionID, reqFFmpeg1.UserID, reqFFmpeg1.ProfileID, string(reqFFmpeg1.RequestType))
		assert.NoError(t, err, "Valid FFmpeg transcode ticket MUST pass validation")

		// 2b. Attempt 2nd FFmpeg transcode session when MaxParallelTranscodes = 1 -> REJECTED
		reqFFmpeg2 := policy.AdmissionRequest{
			SessionID:    "sess_ffmpeg_2",
			UserID:       "usr_kind",
			Role:         identity.RoleGuest,
			ProfileID:    "prof_kind",
			ServiceRef:   "1:0:19:2E9B:411:1:C00000:0:0:0:",
			RequestType:  policy.AdmissionRequestLiveTV,
			IsTranscoded: true, // FFmpeg Transcoder active
		}
		tkt2, dec2 := adm.IssueAdmissionTicket(reqFFmpeg2, resPolicy)
		assert.False(t, dec2.Allowed, "Exceeding FFmpeg transcode capacity MUST be rejected")
		assert.Nil(t, tkt2, "Rejected FFmpeg allocation MUST NOT yield an AdmissionTicket")

		// 2c. Release 1st FFmpeg transcode session -> 2nd attempt now succeeds
		adm.ReleaseAdmissionTicket(tkt1.TicketID)

		tkt2Retry, dec2Retry := adm.IssueAdmissionTicket(reqFFmpeg2, resPolicy)
		require.True(t, dec2Retry.Allowed, "After releasing 1st FFmpeg session, 2nd transcode allocation MUST succeed")
		require.NotNil(t, tkt2Retry)

		// 2d. Attempt using FFmpeg ticket on wrong session ID -> FAIL (ErrSessionMismatch)
		consumedTkt2, err := adm.ConsumeTicketOnce(tkt2Retry.TicketID)
		require.NoError(t, err)
		err = policy.ValidateBoundTicket(consumedTkt2, "wrong_sess_id", reqFFmpeg2.UserID, reqFFmpeg2.ProfileID, string(reqFFmpeg2.RequestType))
		assert.Equal(t, policy.ErrSessionMismatch, err, "FFmpeg ticket with session mismatch MUST fail closed")

		adm.ReleaseAdmissionTicket(tkt2Retry.TicketID)
	})

	// =========================================================================
	// CLASS 3: DVR / RECORDING WORKER RESOURCE ALLOCATION
	// =========================================================================
	t.Run("Class 3: DVR/Recording Worker Strict Ticket Binding", func(t *testing.T) {
		reqDVR1 := policy.AdmissionRequest{
			SessionID:    "rec_dvr_worker_100",
			UserID:       "usr_system",
			Role:         identity.RoleAdmin,
			ProfileID:    "prof_dvr",
			ServiceRef:   "1:0:19:132F:3EF:1:C00000:0:0:0:",
			RequestType:  policy.AdmissionRequestRecord,
			IsTranscoded: false,
		}

		// 3a. Issue 1st DVR recording ticket -> ALLOWED
		tktDVR1, decDVR1 := adm.IssueAdmissionTicket(reqDVR1, resPolicy)
		require.True(t, decDVR1.Allowed)
		require.NotNil(t, tktDVR1)

		// 3b. Attempt DVR worker execution with unconsumed ticket -> FAIL
		err := policy.ValidateBoundTicket(tktDVR1, reqDVR1.SessionID, reqDVR1.UserID, reqDVR1.ProfileID, string(reqDVR1.RequestType))
		assert.Equal(t, policy.ErrTicketNotConsumed, err, "Unconsumed DVR ticket MUST fail closed")

		// 3c. Consume ticket -> VALID for DVR Worker
		consumedDVR1, err := adm.ConsumeTicketOnce(tktDVR1.TicketID)
		require.NoError(t, err)
		err = policy.ValidateBoundTicket(consumedDVR1, reqDVR1.SessionID, reqDVR1.UserID, reqDVR1.ProfileID, string(reqDVR1.RequestType))
		assert.NoError(t, err, "Valid consumed DVR ticket MUST allow recording worker execution")

		// 3d. Attempt using DVR ticket for LiveTV resource class -> FAIL (ErrResourceClassMismatch)
		err = policy.ValidateBoundTicket(consumedDVR1, reqDVR1.SessionID, reqDVR1.UserID, reqDVR1.ProfileID, "live")
		assert.Equal(t, policy.ErrResourceClassMismatch, err, "DVR ticket attempted for LiveTV resource class MUST fail closed")

		// 3e. Attempt 2nd parallel DVR recording when MaxParallelRecordings = 1 -> REJECTED
		reqDVR2 := policy.AdmissionRequest{
			SessionID:    "rec_dvr_worker_200",
			UserID:       "usr_system",
			Role:         identity.RoleAdmin,
			ProfileID:    "prof_dvr",
			ServiceRef:   "1:0:19:283D:3FB:1:C00000:0:0:0:",
			RequestType:  policy.AdmissionRequestManualRecord,
			IsTranscoded: false,
		}
		tktDVR2, decDVR2 := adm.IssueAdmissionTicket(reqDVR2, resPolicy)
		assert.False(t, decDVR2.Allowed, "Exceeding DVR recording capacity MUST be rejected")
		assert.Nil(t, tktDVR2, "Rejected DVR allocation MUST NOT yield an AdmissionTicket")

		adm.ReleaseAdmissionTicket(tktDVR1.TicketID)
	})
}
