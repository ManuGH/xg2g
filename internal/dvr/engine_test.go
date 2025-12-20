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

// MockClient
type MockClient struct {
	mock.Mock
}

func (m *MockClient) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]openwebif.Timer), args.Error(1)
}

func (m *MockClient) AddTimer(ctx context.Context, sRef string, begin, end int64, name, description string) error {
	args := m.Called(ctx, sRef, begin, end, name, description)
	return args.Error(0)
}

// MockEpg
type MockEpg struct {
	mock.Mock
}

func (m *MockEpg) GetEvents(from, to time.Time) ([]openwebif.EPGEvent, error) {
	args := m.Called(from, to)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]openwebif.EPGEvent), args.Error(1)
}

func (m *MockClient) GetEPG(ctx context.Context, ref string, limit int) ([]openwebif.EPGEvent, error) {
	args := m.Called(ctx, ref, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]openwebif.EPGEvent), args.Error(1)
}

func (m *MockClient) DeleteTimer(ctx context.Context, sRef string, begin, end int64) error {
	args := m.Called(ctx, sRef, begin, end)
	return args.Error(0)
}

func TestSeriesEngine_RunOnce(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	rm := NewManager(tmpDir)

	// Create Rule
	ruleID := rm.AddRule(SeriesRule{
		Enabled:     true,
		Keyword:     "News",
		ChannelRef:  "1:0:1:TEST",
		Priority:    10,
		StartWindow: "00:00-23:59",
	})

	// Create another rule that shouldn't match
	rm.AddRule(SeriesRule{
		Enabled:    true,
		Keyword:    "Sports",
		ChannelRef: "1:0:1:SPORTS",
	})

	mockClient := new(MockClient)

	// Mock Config
	mockCfg := config.AppConfig{}

	// Factory returns the SAME mock instance for testing
	factory := func() OWIClient {
		return mockClient
	}

	engine := NewSeriesEngine(mockCfg, rm, factory)

	// Mock Data
	now := time.Now()
	// EPG Return
	events := []openwebif.EPGEvent{
		{Title: "News at Six", SRef: "1:0:1:TEST", Begin: now.Add(1 * time.Hour).Unix(), Duration: 1800},
		{Title: "Comedy Show", SRef: "1:0:1:TEST", Begin: now.Add(2 * time.Hour).Unix(), Duration: 1800},
		{Title: "Sports Live", SRef: "1:0:1:SPORTS", Begin: now.Add(3 * time.Hour).Unix(), Duration: 3600},
	}
	// Note: We use generic matchers because strict time matching is flaky in tests
	// Note: We use generic matchers because strict time matching is flaky in tests
	mockClient.On("GetEPG", mock.Anything, mock.Anything, mock.Anything).Return(events, nil)

	// Client Return (GetTimers empty first time)
	mockClient.On("GetTimers", mock.Anything).Return([]openwebif.Timer{}, nil)

	// Client Expectation (AddTimer for News) - Only expectation for News!
	mockClient.On("AddTimer", mock.Anything, "1:0:1:TEST", mock.Anything, mock.Anything, "News at Six", mock.Anything).Return(nil)

	// Execute RunOnce for specific rule (to test ID filtering)
	reports, err := engine.RunOnce(context.Background(), "manual", ruleID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, reports, 1)
	assert.Equal(t, "success", string(reports[0].Status))
	assert.Equal(t, 1, reports[0].Summary.TimersCreated)
	assert.Equal(t, 1, reports[0].Summary.EpgItemsMatched) // Only News matched
	assert.Equal(t, "created", reports[0].Decisions[0].Action)

	mockClient.AssertExpectations(t)

	// TEST 2: Idempotency
	// Run again, but this time GetTimers will return the timer we just created (simulated)
	// mockClient must be reset or configured for second call

	mockClient2 := new(MockClient)
	engine2 := NewSeriesEngine(mockCfg, rm, func() OWIClient { return mockClient2 })

	mockClient2.On("GetEPG", mock.Anything, mock.Anything, mock.Anything).Return(events, nil)

	// Simulate existing timer
	existingTimer := openwebif.Timer{
		ServiceRef: "1:0:1:TEST",
		Name:       "News at Six",
		Begin:      now.Add(1 * time.Hour).Unix(), // Same start
		End:        now.Add(1*time.Hour).Unix() + 1800,
	}
	mockClient2.On("GetTimers", mock.Anything).Return([]openwebif.Timer{existingTimer}, nil)

	// Expect NO AddTimer calls

	reports2, err2 := engine2.RunOnce(context.Background(), "manual", ruleID)

	assert.NoError(t, err2)
	assert.Equal(t, 0, reports2[0].Summary.TimersCreated)
	assert.Equal(t, 1, reports2[0].Summary.TimersSkipped) // Should skip duplicate
	assert.Equal(t, "skipped", reports2[0].Decisions[0].Action)

	mockClient2.AssertExpectations(t)
}
