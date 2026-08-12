// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package epg

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	RatingUnknown = -1
	Rating0       = 0
	Rating6       = 6
	Rating12      = 12
	Rating16      = 16
	Rating18      = 18
)

const (
	UnknownPolicyBlock           = "block"
	UnknownPolicyRequestApproval = "request_approval"
	UnknownPolicyAllow           = "allow"
)

type RatingResolutionResult struct {
	Rating    int    `json:"rating"`
	IsUnknown bool   `json:"isUnknown"`
	Source    string `json:"source"` // "override", "broadcaster", "text_analysis", "fallback"
}

var (
	reFSK        = regexp.MustCompile(`(?i)(?:FSK|ab|Altersfreigabe|ab\s+den\s+Jahren|Freigegeben\s+ab)\s*[:\-]?\s*(\d{1,2})`)
	rePlusRating = regexp.MustCompile(`(?i)\b(18|16|12|6)\s*\+`)
	reFSKZero    = regexp.MustCompile(`(?i)\b(FSK\s*o\.?A\.?|ohne\s+Altersbeschränkung|ohne\s+Altersbegrenzung)\b`)
)

// ResolveEPGRating determines the age rating for an EPG entry using a 4-stage deterministic lookup:
// 1. Manual Override check
// 2. Broadcaster / DVB rating string parsing
// 3. Regex text analysis on title & description
// 4. Fallback to RatingUnknown (-1)
func ResolveEPGRating(broadcasterRating string, title string, description string, manualOverrides map[string]int, itemID string) RatingResolutionResult {
	if itemID != "" && manualOverrides != nil {
		if r, ok := manualOverrides[itemID]; ok {
			return RatingResolutionResult{
				Rating:    r,
				IsUnknown: r < 0,
				Source:    "override",
			}
		}
	}

	if broadcasterRating != "" {
		if r, ok := parseBroadcasterRating(broadcasterRating); ok {
			return RatingResolutionResult{
				Rating:    r,
				IsUnknown: false,
				Source:    "broadcaster",
			}
		}
	}

	text := title + " " + description
	if r, ok := parseRatingFromText(text); ok {
		return RatingResolutionResult{
			Rating:    r,
			IsUnknown: false,
			Source:    "text_analysis",
		}
	}

	return RatingResolutionResult{
		Rating:    RatingUnknown,
		IsUnknown: true,
		Source:    "fallback",
	}
}

func parseBroadcasterRating(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RatingUnknown, false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return normalizeRating(n)
	}
	return parseRatingFromText(raw)
}

func parseRatingFromText(text string) (int, bool) {
	if text == "" {
		return RatingUnknown, false
	}

	if reFSKZero.MatchString(text) {
		return 0, true
	}

	if matches := reFSK.FindStringSubmatch(text); len(matches) > 1 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			return normalizeRating(val)
		}
	}

	if matches := rePlusRating.FindStringSubmatch(text); len(matches) > 1 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			return normalizeRating(val)
		}
	}

	return RatingUnknown, false
}

func normalizeRating(raw int) (int, bool) {
	switch {
	case raw <= 0:
		return 0, true
	case raw <= 6:
		return 6, true
	case raw <= 12:
		return 12, true
	case raw <= 16:
		return 16, true
	case raw >= 18:
		return 18, true
	default:
		return RatingUnknown, false
	}
}

// IsRatingAllowed evaluates a content rating against a profile's max parental rating and unknown rating policy.
// Returns (allowed, requiresApproval).
func IsRatingAllowed(contentRating int, isUnknown bool, maxRating int, unknownPolicy string) (bool, bool) {
	if isUnknown || contentRating < 0 {
		switch unknownPolicy {
		case UnknownPolicyAllow:
			return true, false
		case UnknownPolicyRequestApproval:
			return false, true
		case UnknownPolicyBlock:
			fallthrough
		default:
			return false, false
		}
	}

	if contentRating <= maxRating {
		return true, false
	}

	// Exceeds max rating threshold
	return false, true
}
