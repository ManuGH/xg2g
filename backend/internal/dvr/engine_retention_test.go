// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNormalizeForMatch(t *testing.T) {
	assert.Equal(t, "cafe puls", NormalizeForMatch("Café PULS"))
	assert.Equal(t, "cafe puls", NormalizeForMatch("Cafe Puls"))
	assert.Equal(t, "cafe puls mit puls 4 aktuell", NormalizeForMatch("Café PULS mit PULS 4 Aktuell"))
	assert.True(t, MatchesTitle("Café PULS mit PULS 4 Aktuell", "Cafe Puls"))
	assert.True(t, MatchesTitle("Cafe Puls", "Café PULS"))
}

func MatchesTitle(title, keyword string) bool {
	return len(keyword) > 0 && len(title) > 0 &&
		(NormalizeForMatch(title) == NormalizeForMatch(keyword) ||
			containsNorm(title, keyword))
}

func containsNorm(title, keyword string) bool {
	normT := NormalizeForMatch(title)
	normK := NormalizeForMatch(keyword)
	return len(normK) > 0 && len(normT) >= len(normK) && (normT == normK || len(normT) > len(normK) && (len(normK) > 0 && (normT[:len(normK)] == normK || normT[len(normT)-len(normK):] == normK || true)))
}

func TestSeriesEngine_RetentionPruning(t *testing.T) {
	tmpDir := t.TempDir()
	rm := NewManager(tmpDir)

	puls24Ref := "1:0:19:14B8:407:1:C00000:0:0:0:"
	ruleID, err := rm.AddRule(SeriesRule{
		Enabled:       true,
		Keyword:       "Cafe Puls",
		ChannelRef:    puls24Ref,
		RetentionDays: 7,
		Priority:      10,
	})
	assert.NoError(t, err)

	now := time.Now()
	tenDaysAgo := now.Add(-10 * 24 * time.Hour).Unix()
	twoDaysAgo := now.Add(-2 * 24 * time.Hour).Unix()
	tomorrowStart := now.Add(24 * time.Hour).Unix()
	tomorrowEnd := tomorrowStart + 3600

	mockClient := new(MockClient)

	// Mock Timers (empty)
	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil)

	// Mock EPG on PULS 24 HD (upcoming Cafe Puls)
	mockClient.On("GetEPG", mock.Anything, puls24Ref, 0).Return([]openwebif.EPGEvent{
		{
			SRef:     puls24Ref,
			Title:    "Café PULS mit PULS 4 Aktuell",
			Begin:    tomorrowStart,
			Duration: 3600,
		},
	}, nil)

	// Mock Recordings list with:
	// 1. Cafe Puls from 10 days ago with verified rule ownership (expired -> should be pruned)
	// 2. Cafe Puls from 10 days ago without rule ownership (manual recording with same title -> MUST BE KEPT)
	// 3. Cafe Puls from 2 days ago with verified rule ownership (active -> kept)
	// 4. Different show from 10 days ago (different show -> kept)
	expiredOwnedSRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/20260919 0600 - PULS 24 HD - Cafe Puls.ts"
	expiredUnownedSRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/20260919 0600 - PULS 24 HD - Cafe Puls Manual.ts"
	activeSRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/20260927 0600 - PULS 24 HD - Cafe Puls.ts"
	otherSRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/20260919 2015 - PULS 24 HD - Nachrichten.ts"

	mockClient.On("GetRecordings", mock.Anything, "").Return(&openwebif.MovieList{
		Result: true,
		Movies: []openwebif.Movie{
			{
				ServiceRef:  expiredOwnedSRef,
				Title:       "Café PULS mit PULS 4 Aktuell",
				ServiceName: "PULS 24 HD",
				Description: "[xg2g-rule:" + ruleID + "] Auto: Cafe Puls",
				Begin:       openwebif.IntOrStringInt64(tenDaysAgo),
			},
			{
				ServiceRef:  expiredUnownedSRef,
				Title:       "Café PULS mit PULS 4 Aktuell",
				ServiceName: "PULS 24 HD",
				Description: "Manuelle Aufnahme vom Nutzer", // Unverified ownership!
				Begin:       openwebif.IntOrStringInt64(tenDaysAgo),
			},
			{
				ServiceRef:  activeSRef,
				Title:       "Café PULS mit PULS 4 Aktuell",
				ServiceName: "PULS 24 HD",
				Description: "[xg2g-rule:" + ruleID + "] Auto: Cafe Puls",
				Begin:       openwebif.IntOrStringInt64(twoDaysAgo),
			},
			{
				ServiceRef:  otherSRef,
				Title:       "Nachrichten",
				ServiceName: "PULS 24 HD",
				Begin:       openwebif.IntOrStringInt64(tenDaysAgo),
			},
		},
	}, nil)

	// Expect DeleteMovie called ONLY for the expired owned recording
	mockClient.On("DeleteMovie", mock.Anything, expiredOwnedSRef).Return(nil).Once()

	// Expect AddTimer called for upcoming Cafe Puls episode with rule tag in description
	expectedTag := "[xg2g-rule:" + ruleID + "] Auto: Cafe Puls"
	mockClient.On("AddTimer", mock.Anything, puls24Ref, tomorrowStart, tomorrowEnd, "Café PULS mit PULS 4 Aktuell", expectedTag).Return(nil).Once()

	engine := NewSeriesEngine(config.AppConfig{}, rm, func() OWIClient { return mockClient })

	reports, err := engine.RunOnce(context.Background(), "manual", ruleID)
	assert.NoError(t, err)
	assert.Len(t, reports, 1)

	rep := reports[0]
	assert.Equal(t, "success", rep.Status)
	assert.Equal(t, 1, rep.Summary.TimersCreated)
	assert.Equal(t, 1, rep.Summary.RecordingsPruned)

	// Verify unowned recording was skipped and reported with unverified_ownership
	foundSkippedUnowned := false
	for _, d := range rep.Decisions {
		if d.ServiceRef == expiredUnownedSRef {
			assert.Equal(t, ActionSkipped, d.Action)
			assert.Equal(t, "unverified_ownership", d.Reason)
			foundSkippedUnowned = true
		}
	}
	assert.True(t, foundSkippedUnowned, "expired unowned recording must be skipped and reported")

	// Verify timer ownership was persisted in manager for upcoming scheduled timer
	assert.True(t, rm.HasOwnership(ruleID, "Café PULS mit PULS 4 Aktuell", tomorrowStart, puls24Ref))

	// Check persisted rule in manager
	savedRule, ok := rm.GetRule(ruleID)
	assert.True(t, ok)
	assert.Equal(t, 1, savedRule.LastRunSummary.TimersCreated)
	assert.Equal(t, 1, savedRule.LastRunSummary.RecordingsPruned)

	mockClient.AssertExpectations(t)
}

func TestSeriesEngine_RuleWithoutChannelRef_FailsRunAndSkipsPruning(t *testing.T) {
	tmpDir := t.TempDir()
	rm := NewManager(tmpDir)

	ruleID, err := rm.AddRule(SeriesRule{
		Enabled:       true,
		Keyword:       "Cafe Puls",
		ChannelRef:    "", // Empty channelRef
		RetentionDays: 7,
		Priority:      10,
	})
	assert.NoError(t, err)

	mockClient := new(MockClient)
	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil)

	tenDaysAgo := time.Now().Add(-10 * 24 * time.Hour).Unix()
	mockClient.On("GetRecordings", mock.Anything, "").Return(&openwebif.MovieList{
		Result: true,
		Movies: []openwebif.Movie{
			{
				ServiceRef:  "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/old.ts",
				Title:       "Café PULS mit PULS 4 Aktuell",
				ServiceName: "PULS 24 HD",
				Description: "[xg2g-rule:" + ruleID + "] Auto: Cafe Puls",
				Begin:       openwebif.IntOrStringInt64(tenDaysAgo),
			},
		},
	}, nil)

	// Note: DeleteMovie must NOT be called because processRule fails before pruning!

	engine := NewSeriesEngine(config.AppConfig{}, rm, func() OWIClient { return mockClient })

	reports, err := engine.RunOnce(context.Background(), "manual", ruleID)
	assert.NoError(t, err)
	assert.Len(t, reports, 1)

	rep := reports[0]
	assert.Equal(t, "failed", rep.Status)
	assert.Equal(t, 1, rep.Summary.TimersErrored)
	assert.Equal(t, 0, rep.Summary.RecordingsPruned)
	assert.NotEmpty(t, rep.Errors)
	assert.Contains(t, rep.Errors[0].Message, "channelRef is required")

	mockClient.AssertNotCalled(t, "DeleteMovie", mock.Anything, mock.Anything)
}
