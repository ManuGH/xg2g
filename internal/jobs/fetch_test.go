// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
)

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
	if srefMap["1:0:19:1234:3EF:1:C00000:0:0:0:"] != "ch1" {
		t.Error("expected sRef for ch1 to map correctly")
	}
	if srefMap["1:0:19:5678:3EF:1:C00000:0:0:0:"] != "ch2" {
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
