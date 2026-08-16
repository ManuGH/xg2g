// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTVMazeClient_Lookup_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, DefaultTVMazeUserAgent, r.Header.Get("User-Agent"))

		if r.URL.Path == "/search/shows" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"score": 0.95,
					"show": {
						"id": 169,
						"name": "Breaking Bad",
						"premiered": "2008-01-20",
						"rating": {"average": 9.2},
						"image": {"medium": "https://example.com/breakingbad.jpg"},
						"summary": "<p>A high school chemistry teacher turns to drug manufacturing.</p>"
					}
				}
			]`))
			return
		}

		if r.URL.Path == "/shows/169/episodebynumber" {
			assert.Equal(t, "2", r.URL.Query().Get("season"))
			assert.Equal(t, "5", r.URL.Query().Get("number"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": 12345,
				"name": "Breakage",
				"season": 2,
				"number": 5,
				"rating": {"average": 8.8},
				"image": {"medium": "https://example.com/bb_s2e5.jpg"},
				"summary": "<p>Walt and Jesse encounter difficulties with distribution.</p>"
			}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := TVMazeConfig{
		BaseURL:           server.URL,
		UserAgent:         DefaultTVMazeUserAgent,
		RequestsPerSecond: 100,
		Burst:             10,
		Timeout:           2 * time.Second,
	}
	client := NewTVMazeClient(cfg)

	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    "breaking bad",
		Year:               2008,
		Season:             2,
		Episode:            5,
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	data, err := client.Lookup(context.Background(), fp)
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, epg.MatchStatusFound, data.Status)
	assert.Equal(t, "tvmaze", data.Identity.Provider)
	assert.Equal(t, "episode", data.Identity.Type)
	assert.Equal(t, "12345", data.Identity.ID)
	require.NotNil(t, data.Rating)
	assert.Equal(t, 8.8, data.Rating.Score)
	assert.Equal(t, "Walt and Jesse encounter difficulties with distribution.", data.Summary)
	assert.Equal(t, "https://example.com/bb_s2e5.jpg", data.PosterURL)
}

func TestTVMazeClient_Lookup_EmptyResultsReturnsNoMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`)) // Empty candidate list
	}))
	defer server.Close()

	cfg := TVMazeConfig{
		BaseURL:           server.URL,
		RequestsPerSecond: 100,
		Burst:             10,
		Timeout:           2 * time.Second,
	}
	client := NewTVMazeClient(cfg)

	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    "completely unknown local broadcast",
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	data, err := client.Lookup(context.Background(), fp)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, epg.MatchStatusNoMatch, data.Status, "Empty result must be classified as deterministic NoMatch")
}

func TestTVMazeClient_Lookup_HTTP429_HonorsRetryAfter(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Retry-After", "2") // 2 seconds backoff
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := TVMazeConfig{
		BaseURL:           server.URL,
		RequestsPerSecond: 100,
		Burst:             10,
		Timeout:           2 * time.Second,
	}
	client := NewTVMazeClient(cfg)

	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    "rate limited show",
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	// 1. First call encounters 429
	data1, err1 := client.Lookup(context.Background(), fp)
	require.Error(t, err1)
	require.NotNil(t, data1)
	assert.Equal(t, epg.MatchStatusTransientFailure, data1.Status)
	assert.Equal(t, 1, requestCount)

	// 2. Second immediate call is blocked locally by backoff without hitting the network
	data2, err2 := client.Lookup(context.Background(), fp)
	require.Error(t, err2)
	require.NotNil(t, data2)
	assert.Equal(t, epg.MatchStatusTransientFailure, data2.Status)
	assert.Equal(t, 1, requestCount, "Subsequent call during backoff window must not touch the network")
}

func TestTVMazeClient_Lookup_ServerErrors_TripCircuitBreaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := TVMazeConfig{
		BaseURL:           server.URL,
		RequestsPerSecond: 100,
		Burst:             10,
		Timeout:           2 * time.Second,
	}
	client := NewTVMazeClient(cfg)

	fp := epg.ProgrammeFingerprint{NormalizedTitle: "error show", FingerprintVersion: epg.CurrentFingerprintVersion}

	// 3 consecutive 500 errors trip the breaker
	for i := 0; i < 3; i++ {
		data, err := client.Lookup(context.Background(), fp)
		assert.Error(t, err)
		assert.Equal(t, epg.MatchStatusTransientFailure, data.Status)
	}

	// Next call should be blocked by circuit breaker
	dataCB, errCB := client.Lookup(context.Background(), fp)
	assert.Error(t, errCB)
	assert.Equal(t, epg.MatchStatusTransientFailure, dataCB.Status)
}
