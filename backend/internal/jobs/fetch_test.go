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
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/epg/store"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestAggregateEvents_PopulatesCanonicalMetadataOnIngest(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:", Name: "Das Erste HD"},
	}

	agg := newEPGAggregator(ctx, items)
	srefMap := agg.buildSRefMap()

	events := []openwebif.EPGEvent{
		{
			ID:          1001,
			Title:       "Tatort: Das Team S01E02",
			Description: "Krimi. FSK 12. Regie: Jan Georg Schütte",
			LongDesc:    "Krimi. FSK 12. Regie: Jan Georg Schütte. Darsteller: Charly Hübner",
			Begin:       1609459200,
			Duration:    5400,
			SRef:        "1:0:19:283D:3FB:1:C00000:0:0:0:",
		},
	}

	programmes := agg.aggregateEvents(events, srefMap)

	if len(programmes) != 1 {
		t.Fatalf("expected 1 programme, got %d", len(programmes))
	}

	prog := programmes[0]
	if prog.Canonical == nil {
		t.Fatal("expected prog.Canonical to be populated on ingest")
	}

	if prog.Canonical.AgeRating == nil {
		t.Fatal("expected AgeRating to be extracted")
	}
	if prog.Canonical.AgeRating.Value != 12 || prog.Canonical.AgeRating.Scheme != "FSK" || prog.Canonical.AgeRating.Country != "DE" {
		t.Errorf("unexpected AgeRating: %+v", prog.Canonical.AgeRating)
	}

	if prog.Canonical.EpisodeInfo == nil {
		t.Fatal("expected EpisodeInfo to be extracted")
	}
	if prog.Canonical.EpisodeInfo.SeasonNumber != 1 || prog.Canonical.EpisodeInfo.EpisodeNumber != 2 {
		t.Errorf("unexpected EpisodeInfo: %+v", prog.Canonical.EpisodeInfo)
	}
}

type mockJobsMetadataProvider struct {
	lookupFn func(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, error)
}

func (m *mockJobsMetadataProvider) Name() string { return "mock_jobs" }
func (m *mockJobsMetadataProvider) Lookup(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, error) {
	if m.lookupFn != nil {
		return m.lookupFn(ctx, fp)
	}
	return nil, nil
}

func TestAggregateEvents_AsyncEnrichmentPipelineIntegration(t *testing.T) {
	ctx := context.Background()
	items := []playlist.Item{
		{ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:", Name: "Das Erste HD"},
	}

	memStore := store.NewMemoryEnrichmentStore()
	defer memStore.Close()

	provider := &mockJobsMetadataProvider{
		lookupFn: func(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, error) {
			return &epg.EnrichmentData{
				FingerprintKey:     fp.Key(),
				FingerprintVersion: fp.FingerprintVersion,
				MatcherVersion:     epg.CurrentMatcherVersion,
				Status:             epg.MatchStatusFound,
				Identity: epg.ProviderIdentity{
					Provider: "tvmaze",
					Type:     "episode",
					ID:       "998877",
				},
				Rating: &epg.RatingScore{
					Score:  8.9,
					Scale:  10.0,
					Source: "tvmaze",
				},
				PosterURL: "https://example.com/poster99.jpg",
				Summary:   "Enriched summary from provider.",
				FetchedAt: time.Now(),
				ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			}, nil
		},
	}

	queue := epg.NewEnrichmentQueue(epg.DefaultQueueConfig(), memStore, provider)
	require.NoError(t, queue.Start(ctx))
	defer queue.Stop()

	events := []openwebif.EPGEvent{
		{
			ID:          1001,
			Title:       "Breaking Bad S02E05",
			Description: "FSK 16. Krimiserie",
			LongDesc:    "FSK 16. Krimiserie",
			Begin:       time.Now().Unix(),
			Duration:    3600,
			SRef:        "1:0:19:283D:3FB:1:C00000:0:0:0:",
			Genre:       "Serie",
		},
	}

	agg := newEPGAggregator(ctx, items).withEnrichment(memStore, queue)
	srefMap := agg.buildSRefMap()

	// 1. First Pass: Cache is empty. Programme gets E1 canonical metadata, and fp is enqueued to queue
	progsFirstPass := agg.aggregateEvents(events, srefMap)
	require.Len(t, progsFirstPass, 1)
	require.NotNil(t, progsFirstPass[0].Canonical)
	// E1 observed rating is preserved
	require.NotNil(t, progsFirstPass[0].Canonical.AgeRating)
	assert.Equal(t, 16, progsFirstPass[0].Canonical.AgeRating.Value)
	assert.Equal(t, "FSK", progsFirstPass[0].Canonical.AgeRating.Scheme)
	// Provider rating not yet attached on first pass
	assert.Nil(t, progsFirstPass[0].Canonical.RatingScore)

	// 2. Wait for background worker to process the job and write to memStore
	require.Eventually(t, func() bool {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:    "breaking bad",
			Season:             2,
			Episode:            5,
			EventGenre:         "series",
			FingerprintVersion: epg.CurrentFingerprintVersion,
		}
		data, found, err := memStore.Get(ctx, fp)
		return err == nil && found && data != nil
	}, 2*time.Second, 20*time.Millisecond)

	// 3. Second Ingest Pass: Store now has cached enrichment. Programme gets both E1 DVB data AND E2 provider data attached
	progsSecondPass := agg.aggregateEvents(events, srefMap)
	require.Len(t, progsSecondPass, 1)
	require.NotNil(t, progsSecondPass[0].Canonical)

	// Invariant Check: E1 DVB observed rating remains 100% intact and unmutated
	require.NotNil(t, progsSecondPass[0].Canonical.AgeRating)
	assert.Equal(t, 16, progsSecondPass[0].Canonical.AgeRating.Value)
	assert.Equal(t, "FSK", progsSecondPass[0].Canonical.AgeRating.Scheme)
	assert.Equal(t, "DE", progsSecondPass[0].Canonical.AgeRating.Country)

	// Provider Enrichment data is now attached
	require.NotNil(t, progsSecondPass[0].Canonical.RatingScore)
	assert.Equal(t, 8.9, progsSecondPass[0].Canonical.RatingScore.Score)
	assert.Equal(t, "tvmaze", progsSecondPass[0].Canonical.RatingScore.Source)
	assert.Equal(t, "https://example.com/poster99.jpg", progsSecondPass[0].Canonical.PosterURL)
	assert.Equal(t, "Enriched summary from provider.", progsSecondPass[0].Canonical.ProviderSummary)
}

func TestCollectEPGProgrammes_ProductionEntryPointWithEnrichment(t *testing.T) {
	ctx := context.Background()
	sref := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	items := []playlist.Item{
		{ServiceRef: sref, Name: "Das Erste HD", TvgID: sref},
	}

	events := []openwebif.EPGEvent{
		{
			ID:          2002,
			Title:       "Dark S01E03",
			Description: "FSK 16. Mysteryserie",
			LongDesc:    "FSK 16. Mysteryserie",
			Begin:       time.Now().Unix(),
			Duration:    3600,
			SRef:        sref,
			Genre:       "Serie",
		},
	}

	mockClient := &mockEPGFetchClient{
		perServiceEPG: map[string][]openwebif.EPGEvent{
			sref: events,
		},
	}

	memStore := store.NewMemoryEnrichmentStore()
	defer memStore.Close()

	provider := &mockJobsMetadataProvider{
		lookupFn: func(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, error) {
			return &epg.EnrichmentData{
				FingerprintKey:     fp.Key(),
				FingerprintVersion: fp.FingerprintVersion,
				MatcherVersion:     epg.CurrentMatcherVersion,
				Status:             epg.MatchStatusFound,
				Identity: epg.ProviderIdentity{
					Provider: "tvmaze",
					Type:     "episode",
					ID:       "178403",
				},
				Rating: &epg.RatingScore{
					Score:  8.7,
					Scale:  10.0,
					Source: "tvmaze",
				},
				PosterURL: "https://example.com/dark_s1e3.jpg",
				Summary:   "Past and Present.",
				FetchedAt: time.Now(),
				ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			}, nil
		},
	}

	queue := epg.NewEnrichmentQueue(epg.DefaultQueueConfig(), memStore, provider)
	require.NoError(t, queue.Start(ctx))
	defer queue.Stop()

	cfg := config.AppConfig{
		EPGMaxConcurrency: 2,
		EPGTimeoutMS:      1000,
		EPGRetries:        0,
		EPGDays:           1,
	}

	// 1. First Pass via production entrypoint collectEPGProgrammes: Miss in cache -> enqueued to queue
	progsFirstPass := collectEPGProgrammes(ctx, mockClient, items, cfg, memStore, queue)
	require.Len(t, progsFirstPass, 1)
	require.NotNil(t, progsFirstPass[0].Canonical)
	assert.Equal(t, 16, progsFirstPass[0].Canonical.AgeRating.Value)
	assert.Nil(t, progsFirstPass[0].Canonical.RatingScore)

	// 2. Wait for worker to finish processing and save to store
	require.Eventually(t, func() bool {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:    "dark",
			Season:             1,
			Episode:            3,
			EventGenre:         "series",
			FingerprintVersion: epg.CurrentFingerprintVersion,
		}
		data, found, err := memStore.Get(ctx, fp)
		return err == nil && found && data != nil
	}, 2*time.Second, 20*time.Millisecond)

	// 3. Second Pass via production entrypoint collectEPGProgrammes: Store hit -> Enriched!
	progsSecondPass := collectEPGProgrammes(ctx, mockClient, items, cfg, memStore, queue)
	require.Len(t, progsSecondPass, 1)
	require.NotNil(t, progsSecondPass[0].Canonical)

	// E1 rating intact
	require.NotNil(t, progsSecondPass[0].Canonical.AgeRating)
	assert.Equal(t, 16, progsSecondPass[0].Canonical.AgeRating.Value)
	assert.Equal(t, "FSK", progsSecondPass[0].Canonical.AgeRating.Scheme)

	// E2 provider rating & poster attached
	require.NotNil(t, progsSecondPass[0].Canonical.RatingScore)
	assert.Equal(t, 8.7, progsSecondPass[0].Canonical.RatingScore.Score)
	assert.Equal(t, "tvmaze", progsSecondPass[0].Canonical.RatingScore.Source)
	assert.Equal(t, "https://example.com/dark_s1e3.jpg", progsSecondPass[0].Canonical.PosterURL)
	assert.Equal(t, "Past and Present.", progsSecondPass[0].Canonical.ProviderSummary)
}
