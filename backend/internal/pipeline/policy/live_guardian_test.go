// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy_test

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
	"github.com/stretchr/testify/assert"
)

func TestLiveGuardian_PreViolationWarningAndImmediateBlock(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Vienna")
	now := time.Date(2026, 8, 12, 18, 59, 45, 0, loc) // 18:59:45 (15 seconds before 19:00 cutoff)

	watch := &policy.ActiveStreamWatch{
		SessionID:         "sess_max",
		UserID:            "usr_max",
		Role:              identity.RoleMember,
		ProfileID:         "prof_max",
		MaxParentalRating: 12,
		UnknownPolicy:     epg.UnknownPolicyRequestApproval,
		DailyCutoffEnd:    "19:00",
		Timezone:          "Europe/Vienna",
	}

	// 1. 18:59:45 -> Warning (15 seconds remaining before 19:00 cutoff)
	dec1 := policy.EvaluateStreamStatus(watch, now, nil, nil)
	assert.Equal(t, policy.GuardianStatusWarning, dec1.State)
	assert.Equal(t, 15, dec1.SecondsRemaining)

	// 2. 19:00:00 -> Immediate Block
	cutoffTime := time.Date(2026, 8, 12, 19, 0, 0, 0, loc)
	dec2 := policy.EvaluateStreamStatus(watch, cutoffTime, nil, nil)
	assert.Equal(t, policy.GuardianStatusBlock, dec2.State)

	// 3. Impending FSK 18 event starting in 20 seconds -> Warning BEFORE event starts
	eventStart := time.Date(2026, 8, 12, 20, 0, 0, 0, time.UTC)
	impendingEvent := &openwebif.EPGEvent{
		ID:          9001,
		Title:       "Horror Movie (FSK 18)",
		Description: "Ab 18 Jahren freigegeben",
		Begin:       eventStart.Unix(),
		Duration:    7200,
	}
	preEventTime := eventStart.Add(-20 * time.Second) // 20s before start

	watchNoCutoff := &policy.ActiveStreamWatch{
		SessionID:         "sess_child",
		MaxParentalRating: 12,
		UnknownPolicy:     epg.UnknownPolicyRequestApproval,
	}

	dec3 := policy.EvaluateStreamStatus(watchNoCutoff, preEventTime, impendingEvent, nil)
	assert.Equal(t, policy.GuardianStatusWarning, dec3.State)
	assert.Equal(t, 20, dec3.SecondsRemaining)

	// 4. FSK 18 event active NOW -> Immediate Block (0s of FSK 18 content shown)
	dec4 := policy.EvaluateStreamStatus(watchNoCutoff, eventStart, impendingEvent, nil)
	assert.Equal(t, policy.GuardianStatusBlock, dec4.State)
}
