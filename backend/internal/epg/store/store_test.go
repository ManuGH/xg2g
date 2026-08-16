// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func runSharedStoreTestSuite(t *testing.T, factory func(t *testing.T) EnrichmentStore) {
	ctx := context.Background()

	t.Run("PutAndGet_FoundMatch", func(t *testing.T) {
		s := factory(t)
		defer s.Close()

		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:    "breaking bad",
			Season:             2,
			Episode:            5,
			FingerprintVersion: epg.CurrentFingerprintVersion,
		}

		data := &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusFound,
			Identity: epg.ProviderIdentity{
				Provider: "tvmaze",
				Type:     "episode",
				ID:       "12345",
			},
			Rating: &epg.RatingScore{
				Score:  8.8,
				Scale:  10.0,
				Source: "tvmaze",
			},
			ProviderAgeRatings: []epg.AgeRating{
				{Value: 16, Scheme: "FSK", Country: "DE", Source: epg.RatingSourceProvider, Confidence: epg.RatingConfidenceMatched},
			},
			PosterURL: "https://example.com/poster.jpg",
			Summary:   "Walter and Jesse face new challenges.",
			FetchedAt: time.Now().Add(-1 * time.Hour),
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		}

		err := s.Put(ctx, data)
		require.NoError(t, err)

		got, found, err := s.Get(ctx, fp)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, got)

		assert.Equal(t, fp.Key(), got.FingerprintKey)
		assert.Equal(t, epg.MatchStatusFound, got.Status)
		assert.Equal(t, "tvmaze", got.Identity.Provider)
		assert.Equal(t, "12345", got.Identity.ID)
		require.NotNil(t, got.Rating)
		assert.Equal(t, 8.8, got.Rating.Score)
		assert.Len(t, got.ProviderAgeRatings, 1)
		assert.Equal(t, 16, got.ProviderAgeRatings[0].Value)
	})

	t.Run("PutAndGet_NoMatchNegativeCache", func(t *testing.T) {
		s := factory(t)
		defer s.Close()

		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:    "local community news",
			FingerprintVersion: epg.CurrentFingerprintVersion,
		}

		data := &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusNoMatch,
			FetchedAt:          time.Now(),
			ExpiresAt:          time.Now().Add(24 * time.Hour),
		}

		err := s.Put(ctx, data)
		require.NoError(t, err)

		got, found, err := s.Get(ctx, fp)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, got)
		assert.Equal(t, epg.MatchStatusNoMatch, got.Status)
	})

	t.Run("Get_ExpiredRecordTreatedAsMiss", func(t *testing.T) {
		s := factory(t)
		defer s.Close()

		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:    "expired show",
			FingerprintVersion: epg.CurrentFingerprintVersion,
		}

		data := &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusFound,
			FetchedAt:          time.Now().Add(-48 * time.Hour),
			ExpiresAt:          time.Now().Add(-1 * time.Hour), // Already expired
		}

		err := s.Put(ctx, data)
		require.NoError(t, err)

		got, found, err := s.Get(ctx, fp)
		require.NoError(t, err)
		assert.False(t, found, "Expired record must be returned as not found (miss)")
		assert.Nil(t, got)
	})

	t.Run("Get_MatcherVersionMismatchTreatedAsMiss", func(t *testing.T) {
		s := factory(t)
		defer s.Close()

		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:    "stale matcher show",
			FingerprintVersion: epg.CurrentFingerprintVersion,
		}

		data := &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion - 1, // Outdated matcher version
			Status:             epg.MatchStatusFound,
			FetchedAt:          time.Now(),
			ExpiresAt:          time.Now().Add(24 * time.Hour),
		}

		err := s.Put(ctx, data)
		require.NoError(t, err)

		got, found, err := s.Get(ctx, fp)
		require.NoError(t, err)
		assert.False(t, found, "Matcher version mismatch must be treated as miss")
		assert.Nil(t, got)
	})

	t.Run("PruneExpired", func(t *testing.T) {
		s := factory(t)
		defer s.Close()

		fpExpired := epg.ProgrammeFingerprint{NormalizedTitle: "to prune", FingerprintVersion: epg.CurrentFingerprintVersion}
		fpValid := epg.ProgrammeFingerprint{NormalizedTitle: "to keep", FingerprintVersion: epg.CurrentFingerprintVersion}

		err := s.Put(ctx, &epg.EnrichmentData{
			FingerprintKey:     fpExpired.Key(),
			FingerprintVersion: fpExpired.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusFound,
			FetchedAt:          time.Now().Add(-10 * time.Hour),
			ExpiresAt:          time.Now().Add(-2 * time.Hour),
		})
		require.NoError(t, err)

		err = s.Put(ctx, &epg.EnrichmentData{
			FingerprintKey:     fpValid.Key(),
			FingerprintVersion: fpValid.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusFound,
			FetchedAt:          time.Now(),
			ExpiresAt:          time.Now().Add(10 * time.Hour),
		})
		require.NoError(t, err)

		pruned, err := s.PruneExpired(ctx, time.Now())
		require.NoError(t, err)
		assert.Equal(t, int64(1), pruned)

		// Verify valid still exists
		gotValid, found, err := s.Get(ctx, fpValid)
		require.NoError(t, err)
		assert.True(t, found)
		assert.NotNil(t, gotValid)
	})
}

func TestMemoryEnrichmentStore(t *testing.T) {
	runSharedStoreTestSuite(t, func(t *testing.T) EnrichmentStore {
		return NewMemoryEnrichmentStore()
	})
}

func TestSQLiteEnrichmentStore(t *testing.T) {
	runSharedStoreTestSuite(t, func(t *testing.T) EnrichmentStore {
		db, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		s, err := NewSQLiteEnrichmentStore(db)
		require.NoError(t, err)
		return s
	})
}

func TestSQLiteEnrichmentStore_PersistenceAcrossReopen(t *testing.T) {
	// Test on file-based SQLite database to verify persistence across store instances
	dbFile := t.TempDir() + "/enrichment_test.db"
	db, err := sql.Open("sqlite", dbFile)
	require.NoError(t, err)
	defer db.Close()

	s1, err := NewSQLiteEnrichmentStore(db)
	require.NoError(t, err)

	ctx := context.Background()
	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    "dark",
		Season:             1,
		Episode:            1,
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	data := &epg.EnrichmentData{
		FingerprintKey:     fp.Key(),
		FingerprintVersion: fp.FingerprintVersion,
		MatcherVersion:     epg.CurrentMatcherVersion,
		Status:             epg.MatchStatusFound,
		Identity: epg.ProviderIdentity{
			Provider: "tvmaze",
			ID:       "999",
		},
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	err = s1.Put(ctx, data)
	require.NoError(t, err)
	s1.Close()

	// Reopen with new instance against the same database file
	s2, err := NewSQLiteEnrichmentStore(db)
	require.NoError(t, err)
	defer s2.Close()

	got, found, err := s2.Get(ctx, fp)
	require.NoError(t, err)
	require.True(t, found, "Persisted SQLite data must survive store instance recreation")
	assert.Equal(t, "999", got.Identity.ID)
}
