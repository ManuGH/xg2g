// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Current versions for fingerprint generation and provider matcher logic.
const (
	CurrentFingerprintVersion = 1
	CurrentMatcherVersion     = 1
)

// MatchStatus represents the outcome of an external provider enrichment attempt.
type MatchStatus string

const (
	// MatchStatusFound indicates a confirmed match from a provider.
	MatchStatusFound MatchStatus = "found"
	// MatchStatusNoMatch indicates a deterministic no-match (e.g. 404 / empty search result from provider).
	MatchStatusNoMatch MatchStatus = "no_match"
	// MatchStatusTransientFailure indicates a temporary network/rate-limit error (never cached as negative match).
	MatchStatusTransientFailure MatchStatus = "transient_failure"
	// MatchStatusPermanentFailure indicates an invalid request or unrecoverable error.
	MatchStatusPermanentFailure MatchStatus = "permanent_failure"
)

// ProgrammeFingerprint represents the canonical, normalized local identity of a programme
// before any external provider matching occurs.
type ProgrammeFingerprint struct {
	NormalizedTitle    string `json:"normalizedTitle"`
	Year               int    `json:"year,omitempty"`
	Season             int    `json:"season,omitempty"`
	Episode            int    `json:"episode,omitempty"`
	EventGenre         string `json:"eventGenre,omitempty"`
	FingerprintVersion int    `json:"fingerprintVersion"`
}

// Key computes a stable, deterministic canonical string key and hash for storage lookups.
func (f ProgrammeFingerprint) Key() string {
	var sb strings.Builder
	sb.WriteString("v")
	sb.WriteString(strconv.Itoa(f.FingerprintVersion))
	sb.WriteString(":")
	sb.WriteString(f.NormalizedTitle)
	if f.Year > 0 {
		sb.WriteString(":y")
		sb.WriteString(strconv.Itoa(f.Year))
	}
	if f.Season > 0 || f.Episode > 0 {
		sb.WriteString(fmt.Sprintf(":s%de%d", f.Season, f.Episode))
	}
	if f.EventGenre != "" {
		sb.WriteString(":g")
		sb.WriteString(f.EventGenre)
	}

	raw := sb.String()
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:16])
}

// ProviderIdentity uniquely identifies an external entity from a metadata provider.
type ProviderIdentity struct {
	Provider string `json:"provider"` // e.g. "tvmaze", "tmdb"
	Type     string `json:"type"`     // e.g. "show", "movie", "episode"
	ID       string `json:"id"`       // e.g. "12345"
}

// RatingScore represents a standardized rating score from a provider.
type RatingScore struct {
	Score  float64 `json:"score"`  // e.g. 8.2
	Scale  float64 `json:"scale"`  // e.g. 10.0
	Source string  `json:"source"` // e.g. "tvmaze", "tmdb", "imdb"
}

// EnrichmentData holds external provider metadata associated with a ProgrammeFingerprint.
// Invariant: Provider metadata NEVER overwrites observed DVB canonical metadata.
type EnrichmentData struct {
	FingerprintKey     string           `json:"fingerprintKey"`
	FingerprintVersion int              `json:"fingerprintVersion"`
	MatcherVersion     int              `json:"matcherVersion"`
	Status             MatchStatus      `json:"status"`
	Identity           ProviderIdentity `json:"identity,omitempty"`
	Rating             *RatingScore     `json:"rating,omitempty"`
	ProviderAgeRatings []AgeRating      `json:"providerAgeRatings,omitempty"`
	PosterURL          string           `json:"posterUrl,omitempty"`
	BackdropURL        string           `json:"backdropUrl,omitempty"`
	Summary            string           `json:"summary,omitempty"`
	FetchedAt          time.Time        `json:"fetchedAt"`
	ExpiresAt          time.Time        `json:"expiresAt"`
}

// IsExpired checks if the enrichment record has passed its TTL.
func (e *EnrichmentData) IsExpired(now time.Time) bool {
	if e == nil || e.ExpiresAt.IsZero() {
		return false
	}
	return now.After(e.ExpiresAt)
}

// MetadataProvider is the abstract interface for external metadata lookup providers.
// Concrete implementations (TVMaze, TMDB) are plugged in without modifying the core queue or store.
type MetadataProvider interface {
	Name() string
	Lookup(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error)
}
