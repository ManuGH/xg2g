// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type mockEPGFetchClient struct {
	bouquets        map[string]string
	bouquetEvents   map[string][]openwebif.EPGEvent
	perServiceEPG   map[string][]openwebif.EPGEvent
	perServiceErr   error
	bouquetCalls    int
	perServiceCalls int
}

func (m *mockEPGFetchClient) Bouquets(_ context.Context) (map[string]string, error) {
	return m.bouquets, nil
}

func (m *mockEPGFetchClient) GetBouquetEPG(_ context.Context, bouquetRef string, _ int) ([]openwebif.EPGEvent, error) {
	m.bouquetCalls++
	return m.bouquetEvents[bouquetRef], nil
}

func (m *mockEPGFetchClient) GetEPG(_ context.Context, sRef string, _ int) ([]openwebif.EPGEvent, error) {
	m.perServiceCalls++
	if m.perServiceErr != nil {
		return nil, m.perServiceErr
	}
	return m.perServiceEPG[sRef], nil
}

func TestFetchEPGWithRetry_ClassifiesTimeoutAsRetryable(t *testing.T) {
	client := &mockEPGFetchClient{perServiceErr: context.DeadlineExceeded}
	cfg := config.AppConfig{EPGRetries: 2}

	_, err := fetchEPGWithRetry(context.Background(), client, "1:0:1:ABC", cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if client.perServiceCalls != 3 {
		t.Fatalf("GetEPG() calls = %d, want 3", client.perServiceCalls)
	}
	if got := JobErrorCode(err); got != problemcode.CodeJobEPGFetchTimeout {
		t.Fatalf("JobErrorCode() = %q, want %q", got, problemcode.CodeJobEPGFetchTimeout)
	}
	if !JobErrorRetryable(err) {
		t.Fatal("timeout error must be retryable")
	}
}

func TestFetchEPGWithRetry_RejectsEmptyServiceRef(t *testing.T) {
	_, err := fetchEPGWithRetry(context.Background(), &mockEPGFetchClient{}, "", config.AppConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := JobErrorCode(err); got != problemcode.CodeJobEPGFetchInvalidInput {
		t.Fatalf("JobErrorCode() = %q, want %q", got, problemcode.CodeJobEPGFetchInvalidInput)
	}
	if JobErrorRetryable(err) {
		t.Fatal("invalid input must not be retryable")
	}
}

func TestFetchEPGWithRetry_PropagatesFinalGenericFailure(t *testing.T) {
	client := &mockEPGFetchClient{perServiceErr: errors.New("receiver returned malformed EPG")}

	_, err := fetchEPGWithRetry(context.Background(), client, "1:0:1:ABC", config.AppConfig{EPGRetries: 0})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := JobErrorCode(err); got != problemcode.CodeJobEPGFetchFailed {
		t.Fatalf("JobErrorCode() = %q, want %q", got, problemcode.CodeJobEPGFetchFailed)
	}
	if JobErrorRetryable(err) {
		t.Fatal("generic fetch failure must not be retryable")
	}
}

// TestExtractSRefFromStreamURL tests service reference extraction from various URL formats.
func TestExtractSRefFromStreamURL(t *testing.T) {
	tests := []struct {
		name      string
		streamURL string
		want      string
	}{
		{
			name:      "new format - direct service reference",
			streamURL: "http://192.168.1.100:8001/1:0:19:132F:3EF:1:C00000:0:0:0:",
			want:      "1:0:19:132F:3EF:1:C00000:0:0:0:",
		},
		{
			name:      "old format - query parameter encoded",
			streamURL: "http://192.168.1.100:8001/web/stream.m3u?ref=1%3A0%3A19%3A132F%3A3EF%3A1%3AC00000%3A0%3A0%3A0%3A",
			want:      "1:0:19:132F:3EF:1:C00000:0:0:0:",
		},
		{
			name:      "old format - query parameter not encoded",
			streamURL: "http://192.168.1.100:8001/web/stream.m3u?ref=1:0:1:3ABD:514:13E:820000:0:0:0:",
			want:      "1:0:1:3ABD:514:13E:820000:0:0:0:",
		},
		{
			name:      "empty URL",
			streamURL: "",
			want:      "",
		},
		{
			name:      "invalid URL",
			streamURL: "://invalid",
			want:      "",
		},
		{
			name:      "no service reference",
			streamURL: "http://192.168.1.100:8001/",
			want:      "",
		},
		{
			name:      "path without colons",
			streamURL: "http://192.168.1.100:8001/plain/path",
			want:      "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSRefFromStreamURL(tc.streamURL)
			if got != tc.want {
				t.Errorf("extractSRefFromStreamURL(%q) = %q, want %q", tc.streamURL, got, tc.want)
			}
		})
	}
}

// TestNewEPGAggregator tests EPG aggregator construction.
func TestNewEPGAggregator(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{Name: "Test Channel", TvgID: "test1"},
	}

	agg := newEPGAggregator(ctx, items)

	if agg == nil {
		t.Fatal("expected non-nil aggregator")
	}
	if agg.ctx != ctx {
		t.Error("aggregator context mismatch")
	}
	if len(agg.items) != len(items) {
		t.Errorf("expected %d items, got %d", len(items), len(agg.items))
	}
}

// TestBuildSRefMap tests service reference map construction.
func TestBuildSRefMap(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{
			Name:  "Channel 1",
			TvgID: "ch1",
			URL:   "http://host:8001/1:0:19:1234:3EF:1:C00000:0:0:0:",
		},
		{
			Name:  "Channel 2",
			TvgID: "ch2",
			URL:   "http://host:8001/1:0:19:5678:3EF:1:C00000:0:0:0:",
		},
		{
			Name:  "Channel 3 (no sRef)",
			TvgID: "ch3",
			URL:   "http://host:8001/invalid",
		},
	}

	agg := newEPGAggregator(ctx, items)
	srefMap := agg.buildSRefMap()

	// Verify correct mappings
	if srefMap["1:0:19:1234:3EF:1:C00000:0:0:0:"] != "1:0:19:1234:3EF:1:C00000:0:0:0:" {
		t.Error("expected sRef for ch1 to map correctly")
	}
	if srefMap["1:0:19:5678:3EF:1:C00000:0:0:0:"] != "1:0:19:5678:3EF:1:C00000:0:0:0:" {
		t.Error("expected sRef for ch2 to map correctly")
	}

	// Verify invalid URL doesn't create mapping
	if len(srefMap) != 2 {
		t.Errorf("expected 2 mappings, got %d", len(srefMap))
	}
}

// TestAggregateEvents tests EPG event aggregation and conversion to programmes.
func TestAggregateEvents(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{TvgID: "ch1", Name: "Channel 1"},
		{TvgID: "ch2", Name: "Channel 2"},
	}

	agg := newEPGAggregator(ctx, items)

	// Create sRef map
	srefMap := map[string]string{
		"sref1": "ch1",
		"sref2": "ch2",
	}

	// Create test events
	events := []openwebif.EPGEvent{
		{
			ID:          1,
			Title:       "Programme 1",
			Description: "Description 1",
			Begin:       1609459200, // 2021-01-01 00:00:00 UTC
			Duration:    3600,
			SRef:        "sref1",
		},
		{
			ID:          2,
			Title:       "Programme 2",
			Description: "Description 2",
			Begin:       1609462800, // 2021-01-01 01:00:00 UTC
			Duration:    1800,
			SRef:        "sref1",
		},
		{
			ID:          3,
			Title:       "Programme 3",
			Description: "Description 3",
			Begin:       1609466400, // 2021-01-01 02:00:00 UTC
			Duration:    7200,
			SRef:        "sref2",
		},
		{
			ID:          4,
			Title:       "Unmapped Event",
			Description: "No channel",
			Begin:       1609470000,
			Duration:    600,
			SRef:        "unknown-sref",
		},
	}

	programmes := agg.aggregateEvents(events, srefMap)

	// Verify we got 3 programmes (4th event has unknown sRef)
	if len(programmes) != 3 {
		t.Fatalf("expected 3 programmes, got %d", len(programmes))
	}

	// Verify channel mappings
	ch1Count := 0
	ch2Count := 0
	for _, prog := range programmes {
		switch prog.Channel {
		case "ch1":
			ch1Count++
		case "ch2":
			ch2Count++
		}
	}

	if ch1Count != 2 {
		t.Errorf("expected 2 programmes for ch1, got %d", ch1Count)
	}
	if ch2Count != 1 {
		t.Errorf("expected 1 programme for ch2, got %d", ch2Count)
	}
}

// TestAggregateEvents_EmptyEvents tests aggregation with no events.
func TestAggregateEvents_EmptyEvents(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{TvgID: "ch1", Name: "Channel 1"},
	}

	agg := newEPGAggregator(ctx, items)
	srefMap := map[string]string{"sref1": "ch1"}

	programmes := agg.aggregateEvents([]openwebif.EPGEvent{}, srefMap)

	if len(programmes) != 0 {
		t.Errorf("expected 0 programmes, got %d", len(programmes))
	}
}

// TestAggregateEvents_NoMatchingChannels tests aggregation when no events match channels.
func TestAggregateEvents_NoMatchingChannels(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{TvgID: "ch1", Name: "Channel 1"},
	}

	agg := newEPGAggregator(ctx, items)
	srefMap := map[string]string{"sref1": "ch1"}

	events := []openwebif.EPGEvent{
		{
			ID:       1,
			Title:    "Orphan Programme",
			Begin:    1609459200,
			Duration: 3600,
			SRef:     "unknown-sref", // Doesn't match any channel
		},
	}

	programmes := agg.aggregateEvents(events, srefMap)

	if len(programmes) != 0 {
		t.Errorf("expected 0 programmes (no matching channels), got %d", len(programmes))
	}
}

func TestCollectEPGProgrammes_FallsBackWhenBouquetCoverageIsTooShort(t *testing.T) {
	now := time.Now().UTC()
	serviceRef := "1:0:19:8F:4:85:C00000:0:0:0"

	client := &mockEPGFetchClient{
		bouquets: map[string]string{
			"Premium": "bref-premium",
		},
		bouquetEvents: map[string][]openwebif.EPGEvent{
			"bref-premium": {
				{
					Title:    "Expired bouquet event",
					Begin:    now.Add(-2 * time.Hour).Unix(),
					Duration: 1800,
					SRef:     serviceRef,
				},
			},
		},
		perServiceEPG: map[string][]openwebif.EPGEvent{
			serviceRef: {
				{
					Title:    "Future per-service event",
					Begin:    now.Add(2 * time.Hour).Unix(),
					Duration: 7200,
					SRef:     serviceRef,
				},
			},
		},
	}

	items := []playlist.Item{
		{
			Name:       "Sky Sport Austria 1",
			Group:      "Premium",
			ServiceRef: serviceRef,
		},
	}

	cfg := config.AppConfig{
		EPGSource:         "bouquet",
		EPGDays:           14,
		EPGTimeoutMS:      5000,
		EPGMaxConcurrency: 1,
		EPGRetries:        0,
	}

	programmes := collectEPGProgrammes(context.Background(), client, items, cfg)

	if client.bouquetCalls != 1 {
		t.Fatalf("expected 1 bouquet EPG call, got %d", client.bouquetCalls)
	}
	if client.perServiceCalls != 1 {
		t.Fatalf("expected fallback to make 1 per-service EPG call, got %d", client.perServiceCalls)
	}
	if len(programmes) != 1 {
		t.Fatalf("expected 1 programme after per-service fallback, got %d", len(programmes))
	}
	if programmes[0].Title.Text != "Future per-service event" {
		t.Fatalf("expected per-service programme to win after fallback, got %q", programmes[0].Title.Text)
	}
}

func TestCollectEPGProgrammes_UsesBouquetWhenCoverageIsSufficient(t *testing.T) {
	now := time.Now().UTC()
	serviceRef := "1:0:19:11:6:85:C00000:0:0:0"

	client := &mockEPGFetchClient{
		bouquets: map[string]string{
			"Premium": "bref-premium",
		},
		bouquetEvents: map[string][]openwebif.EPGEvent{
			"bref-premium": {
				{
					Title:    "Future bouquet event",
					Begin:    now.Add(30 * time.Minute).Unix(),
					Duration: int64((7 * time.Hour).Seconds()),
					SRef:     serviceRef,
				},
			},
		},
	}

	items := []playlist.Item{
		{
			Name:       "Sky Sport F1",
			Group:      "Premium",
			ServiceRef: serviceRef,
		},
	}

	cfg := config.AppConfig{
		EPGSource:         "bouquet",
		EPGDays:           14,
		EPGTimeoutMS:      5000,
		EPGMaxConcurrency: 1,
		EPGRetries:        0,
	}

	programmes := collectEPGProgrammes(context.Background(), client, items, cfg)

	if client.bouquetCalls != 1 {
		t.Fatalf("expected 1 bouquet EPG call, got %d", client.bouquetCalls)
	}
	if client.perServiceCalls != 0 {
		t.Fatalf("expected no per-service fallback when bouquet coverage is sufficient, got %d calls", client.perServiceCalls)
	}
	if len(programmes) != 1 {
		t.Fatalf("expected 1 bouquet programme, got %d", len(programmes))
	}
	if programmes[0].Title.Text != "Future bouquet event" {
		t.Fatalf("expected bouquet programme to be used directly, got %q", programmes[0].Title.Text)
	}
}

func TestBouquetEPGCoversFutureWindow(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name   string
		events []openwebif.EPGEvent
		wantOK bool
	}{
		{
			name:   "no events",
			events: nil,
			wantOK: false,
		},
		{
			name: "latest end already in the past",
			events: []openwebif.EPGEvent{
				{
					Begin:    now.Add(-2 * time.Hour).Unix(),
					Duration: 1800,
				},
			},
			wantOK: false,
		},
		{
			name: "future coverage shorter than minimum window",
			events: []openwebif.EPGEvent{
				{
					Begin:    now.Add(10 * time.Minute).Unix(),
					Duration: int64((2 * time.Hour).Seconds()),
				},
			},
			wantOK: false,
		},
		{
			name: "future coverage reaches minimum window",
			events: []openwebif.EPGEvent{
				{
					Begin:    now.Add(15 * time.Minute).Unix(),
					Duration: int64((7 * time.Hour).Seconds()),
				},
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOK, _ := bouquetEPGCoversFutureWindow(tt.events, now, minBouquetFutureCoverage)
			if gotOK != tt.wantOK {
				t.Fatalf("bouquetEPGCoversFutureWindow() = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}
