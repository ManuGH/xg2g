// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

// RatingSource identifies where an age rating was extracted or retrieved from.
type RatingSource string

const (
	RatingSourceDVBText       RatingSource = "dvb_text"
	RatingSourceDVBDescriptor RatingSource = "dvb_descriptor"
	RatingSourceProvider      RatingSource = "provider"
	RatingSourceManual        RatingSource = "override"
)

// RatingConfidence represents the certainty of extraction without implying legal broadcast guarantee.
type RatingConfidence string

const (
	// RatingConfidenceObserved means the value was extracted directly and unambiguously from source text/descriptors.
	RatingConfidenceObserved RatingConfidence = "observed"
	// RatingConfidenceMatched means the value was obtained from a high-confidence external provider match.
	RatingConfidenceMatched RatingConfidence = "matched"
	// RatingConfidenceInferred means the value was derived heuristically.
	RatingConfidenceInferred RatingConfidence = "inferred"
)

// AgeRating models an age certification with explicit scheme and provenance.
type AgeRating struct {
	Value      int              `json:"value"`      // 0, 6, 12, 16, 18
	Scheme     string           `json:"scheme"`     // "FSK" (only if explicitly stated), or "broadcaster_age"
	Country    string           `json:"country"`    // "DE" (for FSK), "unknown" for generic broadcaster text
	Source     RatingSource     `json:"source"`     // "dvb_text", "dvb_descriptor", etc.
	Confidence RatingConfidence `json:"confidence"` // "observed", "matched", "inferred"
}

// EpisodeInfo models season and episode numbers extracted with high precision.
type EpisodeInfo struct {
	SeasonNumber  int    `json:"seasonNumber,omitempty"`
	EpisodeNumber int    `json:"episodeNumber,omitempty"`
	EpisodeTitle  string `json:"episodeTitle,omitempty"`
	SourcePattern string `json:"sourcePattern,omitempty"`
}

// CanonicalMetadata contains pre-parsed, deterministic EPG metadata attached to a Programme on ingest.
type CanonicalMetadata struct {
	AgeRating          *AgeRating   `json:"ageRating,omitempty"` // Strictly from DVB source (E1)
	EpisodeInfo        *EpisodeInfo `json:"episodeInfo,omitempty"`
	Genre              string       `json:"genre,omitempty"`
	GenreSource        string       `json:"genreSource,omitempty"` // "dvb_descriptor", "dvb_category", "dvb_text"
	Year               string       `json:"year,omitempty"`
	Country            string       `json:"country,omitempty"`
	Directors          []string     `json:"directors,omitempty"`
	Actors             []string     `json:"actors,omitempty"`
	RatingScore        *RatingScore `json:"ratingScore,omitempty"`        // External provider rating (E2)
	ProviderAgeRatings []AgeRating  `json:"providerAgeRatings,omitempty"` // External provider ratings (E2)
	PosterURL          string       `json:"posterUrl,omitempty"`          // External provider poster (E2)
	ProviderSummary    string       `json:"providerSummary,omitempty"`    // External provider summary (E2)
}
