// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

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

// M8: a malformed StartWindow must fail the rule run loudly, not silently match with
// parseHHMM's -1 sentinel (which recorded the wrong programs while reporting success).
func TestSeriesEngine_RunOnce_InvalidStartWindowFailsRule(t *testing.T) {
	rm := NewManager(t.TempDir())
	ruleID, err := rm.AddRule(SeriesRule{
		Enabled:     true,
		Keyword:     "News",
		ChannelRef:  "1:0:1:TEST",
		StartWindow: "garbage-1200", // malformed
	})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	mockClient := new(MockClient)
	engine := NewSeriesEngine(config.AppConfig{}, rm, func() OWIClient { return mockClient })

	now := time.Now()
	events := []openwebif.EPGEvent{
		{Title: "News at Six", SRef: "1:0:1:TEST", Begin: now.Add(1 * time.Hour).Unix(), Duration: 1800},
	}
	mockClient.On("GetEPG", mock.Anything, mock.Anything, mock.Anything).Return(events, nil)
	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil)
	// No AddTimer expectation: a malformed window must NOT create timers.

	reports, err := engine.RunOnce(context.Background(), "manual", ruleID)
	assert.NoError(t, err)
	assert.Len(t, reports, 1)
	// The discriminator: the old code silently used winStart=-1 and reported success.
	assert.Equal(t, "failed", string(reports[0].Status), "malformed StartWindow must fail the rule")
	assert.Equal(t, 0, reports[0].Summary.TimersCreated, "malformed window must not create timers")
	mockClient.AssertNotCalled(t, "AddTimer", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
