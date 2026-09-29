// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestSeriesEngine_Singleflight_CoalescesSameRule(t *testing.T) {
	tmpDir := t.TempDir()
	rm := NewManager(tmpDir)

	ruleID, err := rm.AddRule(SeriesRule{
		Enabled:    true,
		Keyword:    "News",
		ChannelRef: "1:0:1:TEST",
		Priority:   5,
	})
	assert.NoError(t, err)

	mockClient := new(MockClient)

	// Block GetTimers briefly so concurrent callers arrive during the flight
	var getTimersCalls int32
	mockClient.On("GetTimers", mock.Anything).Run(func(args mock.Arguments) {
		atomic.AddInt32(&getTimersCalls, 1)
		time.Sleep(50 * time.Millisecond)
	}).Return([]openwebif.Timer{}, nil)

	mockClient.On("GetEPG", mock.Anything, "1:0:1:TEST", 7).Return([]openwebif.EPGEvent{
		{
			SRef:     "1:0:1:TEST",
			Title:    "News at Six",
			Begin:    time.Now().Add(1 * time.Hour).Unix(),
			Duration: 1800,
		},
	}, nil)

	mockClient.On("AddTimer", mock.Anything, "1:0:1:TEST", mock.Anything, mock.Anything, "News at Six", mock.Anything).Return(nil)

	engine := NewSeriesEngine(config.AppConfig{}, rm, func() OWIClient { return mockClient })

	// Run 3 concurrent RunOnce for the SAME ruleID
	const concurrency = 3
	var wg sync.WaitGroup
	wg.Add(concurrency)

	type runResult struct {
		reports []SeriesRuleRunReport
		err     error
	}
	results := make([]runResult, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		go func() {
			defer wg.Done()
			reps, err := engine.RunOnce(context.Background(), "manual", ruleID)
			results[idx] = runResult{reports: reps, err: err}
		}()
	}

	wg.Wait()

	// All 3 callers must get identical successful result
	for i := 0; i < concurrency; i++ {
		assert.NoError(t, results[i].err)
		assert.Len(t, results[i].reports, 1)
		assert.Equal(t, "success", results[i].reports[0].Status)
	}

	// Singleflight must have coalesced them into a single underlying execution!
	assert.Equal(t, int32(1), atomic.LoadInt32(&getTimersCalls), "concurrent calls for the same rule must be coalesced by singleflight")
}

func TestSeriesEngine_Singleflight_DistinctKeysForDifferentRules(t *testing.T) {
	tmpDir := t.TempDir()
	rm := NewManager(tmpDir)

	rule1, err := rm.AddRule(SeriesRule{
		Enabled:    true,
		Keyword:    "News",
		ChannelRef: "1:0:1:TEST1",
		Priority:   5,
	})
	assert.NoError(t, err)

	rule2, err := rm.AddRule(SeriesRule{
		Enabled:    true,
		Keyword:    "Sports",
		ChannelRef: "1:0:1:TEST2",
		Priority:   5,
	})
	assert.NoError(t, err)

	mockClient := new(MockClient)

	var getTimersCalls int32
	mockClient.On("GetTimers", mock.Anything).Run(func(args mock.Arguments) {
		atomic.AddInt32(&getTimersCalls, 1)
		time.Sleep(50 * time.Millisecond)
	}).Return([]openwebif.Timer{}, nil)

	mockClient.On("GetEPG", mock.Anything, "1:0:1:TEST1", 7).Return([]openwebif.EPGEvent{
		{SRef: "1:0:1:TEST1", Title: "News at Six", Begin: time.Now().Add(1 * time.Hour).Unix(), Duration: 1800},
	}, nil)

	mockClient.On("GetEPG", mock.Anything, "1:0:1:TEST2", 7).Return([]openwebif.EPGEvent{
		{SRef: "1:0:1:TEST2", Title: "Sports Center", Begin: time.Now().Add(2 * time.Hour).Unix(), Duration: 3600},
	}, nil)

	mockClient.On("AddTimer", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	engine := NewSeriesEngine(config.AppConfig{}, rm, func() OWIClient { return mockClient })

	// Run concurrent RunOnce for DIFFERENT ruleIDs
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = engine.RunOnce(context.Background(), "manual", rule1)
	}()

	go func() {
		defer wg.Done()
		_, _ = engine.RunOnce(context.Background(), "manual", rule2)
	}()

	wg.Wait()

	// Because rule1 and rule2 have distinct singleflight keys ("rule:rule1" vs "rule:rule2"),
	// both runs executed independently!
	assert.Equal(t, int32(2), atomic.LoadInt32(&getTimersCalls), "distinct rules must have distinct singleflight keys and run independently")
}
