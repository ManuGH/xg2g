// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"regexp"
	"strconv"
	"strings"
)

// Metadata holds extracted legacy EPG metadata (kept for backwards compatibility).
type Metadata struct {
	Year      string
	Country   string
	Directors []string
	Actors    []string
	Genre     string
}

var (
	// Regex patterns for common EPG text structures
	yearCountryRegex = regexp.MustCompile(`\b([A-Z][a-zA-Z\.]+)\s+((?:19|20)\d{2})\b`)
	directorRegex    = regexp.MustCompile(`(?i)(?:Regie|Director|Directed by)[:\s]+([^,.\n]+)`)
	castRegex        = regexp.MustCompile(`(?i)(?:Darsteller|Cast|Actors|Mit)[:\s]+([^.\n]+)`)

	// Explicit FSK patterns: ONLY text explicitly mentioning FSK produces Scheme: "FSK", Country: "DE".
	reFSKZeroText   = regexp.MustCompile(`(?i)\bFSK\s*(?:o\.?A\.?|0|ohne\s+Altersbeschränkung)\b`)
	reFSKStandard   = regexp.MustCompile(`(?i)(?:\[FSK\s*(?:ab\s*)?|\(FSK\s*(?:ab\s*)?|\bFSK\s*(?:[:\-]|ab)?\s*|\bFreigabe:\s*FSK\s*)(0|6|12|16|18)\b`)
	reFSKGemaessFSK = regexp.MustCompile(`(?i)\bFreigegeben\s+ab\s+(0|6|12|16|18)\s+(?:Jahren\s+)?gemäß\s+FSK\b`)

	// Generic Broadcaster Age (Non-FSK: Austrian, Swiss, foreign, or unbranded broadcaster text)
	reBroadcasterAge = regexp.MustCompile(`(?i)\b(?:ab\s+(6|12|16|18)\s+Jahren|Altersfreigabe\s*:\s*(6|12|16|18)|geeignet\s+ab\s+(6|12|16|18)|ab\s+(6|12|16|18)\s+J\.)\b`)
	rePlusAge        = regexp.MustCompile(`\b(18|16|12|6)\s*\+`)

	// Episode / Season Regex Patterns (Strict Precision)
	reSxxExx        = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*E(\d{1,3})\b`)
	reNxNN          = regexp.MustCompile(`\b(\d{1,2})x(\d{1,3})\b`)
	reStaffelFolge  = regexp.MustCompile(`(?i)\bStaffel\s+(\d{1,2})\s*,\s*(?:Folge|Episode)\s+(\d{1,3})\b`)
	reFolgeIsolated = regexp.MustCompile(`(?i)\b(?:Folge|Episode)\s+(\d{1,3})\b`)
)

// ExtractAgeRating performs deterministic extraction of age ratings with explicit scheme & provenance.
func ExtractAgeRating(title, desc, broadcasterRating string) *AgeRating {
	// 1. Broadcaster DVB Rating (if explicitly provided via DVB descriptor or API field)
	if broadcasterRating != "" {
		if rating, ok := parseBroadcasterDescriptorRating(broadcasterRating); ok {
			return &AgeRating{
				Value:      rating,
				Scheme:     "dvb_descriptor",
				Country:    "unknown",
				Source:     RatingSourceDVBDescriptor,
				Confidence: RatingConfidenceObserved,
			}
		}
	}

	combined := title + " " + desc
	if strings.TrimSpace(combined) == "" {
		return nil
	}

	// 2. Explicit FSK Parsing (DE)
	if reFSKZeroText.MatchString(combined) {
		return &AgeRating{
			Value:      0,
			Scheme:     "FSK",
			Country:    "DE",
			Source:     RatingSourceDVBText,
			Confidence: RatingConfidenceObserved,
		}
	}

	if match := reFSKStandard.FindStringSubmatch(combined); len(match) == 2 {
		if val, err := strconv.Atoi(match[1]); err == nil && isValidAgeRating(val) {
			return &AgeRating{
				Value:      val,
				Scheme:     "FSK",
				Country:    "DE",
				Source:     RatingSourceDVBText,
				Confidence: RatingConfidenceObserved,
			}
		}
	}

	if match := reFSKGemaessFSK.FindStringSubmatch(combined); len(match) == 2 {
		if val, err := strconv.Atoi(match[1]); err == nil && isValidAgeRating(val) {
			return &AgeRating{
				Value:      val,
				Scheme:     "FSK",
				Country:    "DE",
				Source:     RatingSourceDVBText,
				Confidence: RatingConfidenceObserved,
			}
		}
	}

	// 3. Generic Broadcaster Text (broadcaster_age, country=unknown)
	if match := reBroadcasterAge.FindStringSubmatch(combined); len(match) > 0 {
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				if val, err := strconv.Atoi(match[i]); err == nil && isValidAgeRating(val) {
					return &AgeRating{
						Value:      val,
						Scheme:     "broadcaster_age",
						Country:    "unknown",
						Source:     RatingSourceDVBText,
						Confidence: RatingConfidenceObserved,
					}
				}
			}
		}
	}

	if match := rePlusAge.FindStringSubmatch(combined); len(match) > 1 {
		if val, err := strconv.Atoi(match[1]); err == nil && isValidAgeRating(val) {
			return &AgeRating{
				Value:      val,
				Scheme:     "broadcaster_age",
				Country:    "unknown",
				Source:     RatingSourceDVBText,
				Confidence: RatingConfidenceObserved,
			}
		}
	}

	return nil
}

// ExtractEpisodeInfo extracts season and episode numbers with strict precision.
// Ambiguous patterns return nil.
func ExtractEpisodeInfo(title, desc string) *EpisodeInfo {
	combined := title + " " + desc
	if strings.TrimSpace(combined) == "" {
		return nil
	}

	// 1. SxxExx (e.g. S02E05)
	if match := reSxxExx.FindStringSubmatch(combined); len(match) == 3 {
		s, _ := strconv.Atoi(match[1])
		e, _ := strconv.Atoi(match[2])
		if s > 0 && e > 0 {
			return &EpisodeInfo{
				SeasonNumber:  s,
				EpisodeNumber: e,
				SourcePattern: "SxxExx",
			}
		}
	}

	// 2. Staffel X, Folge Y
	if match := reStaffelFolge.FindStringSubmatch(combined); len(match) == 3 {
		s, _ := strconv.Atoi(match[1])
		e, _ := strconv.Atoi(match[2])
		if s > 0 && e > 0 {
			return &EpisodeInfo{
				SeasonNumber:  s,
				EpisodeNumber: e,
				SourcePattern: "Staffel N, Folge M",
			}
		}
	}

	// 3. 2x05 (NxNN)
	if match := reNxNN.FindStringSubmatch(combined); len(match) == 3 {
		s, _ := strconv.Atoi(match[1])
		e, _ := strconv.Atoi(match[2])
		if s > 0 && e > 0 {
			return &EpisodeInfo{
				SeasonNumber:  s,
				EpisodeNumber: e,
				SourcePattern: "NxNN",
			}
		}
	}

	// 4. Folge N / Episode N (isolated episode without season)
	if match := reFolgeIsolated.FindStringSubmatch(combined); len(match) == 2 {
		e, _ := strconv.Atoi(match[1])
		if e > 0 {
			return &EpisodeInfo{
				SeasonNumber:  0,
				EpisodeNumber: e,
				SourcePattern: "Folge N",
			}
		}
	}

	return nil
}

// ExtractDVBGenre maps DVB EN 300 468 content descriptors or category strings to canonical genres.
func ExtractDVBGenre(contentDescriptor int, category string) (genre string, source string) {
	// DVB EN 300 468 Table 28 (Content nibble level 1)
	if contentDescriptor > 0 {
		nibble := (contentDescriptor >> 4) & 0x0F
		switch nibble {
		case 0x01:
			return "movie", "dvb_descriptor"
		case 0x02:
			return "news", "dvb_descriptor"
		case 0x03:
			return "show", "dvb_descriptor"
		case 0x04:
			return "sport", "dvb_descriptor"
		case 0x05:
			return "kids", "dvb_descriptor"
		case 0x06:
			return "music", "dvb_descriptor"
		case 0x07:
			return "arts", "dvb_descriptor"
		case 0x08:
			return "social", "dvb_descriptor"
		case 0x09:
			return "docu", "dvb_descriptor"
		}
	}

	if category != "" {
		catLower := strings.ToLower(category)
		switch {
		case strings.Contains(catLower, "film") || strings.Contains(catLower, "movie") || strings.Contains(catLower, "kino"):
			return "movie", "dvb_category"
		case strings.Contains(catLower, "serie"):
			return "series", "dvb_category"
		case strings.Contains(catLower, "sport"):
			return "sport", "dvb_category"
		case strings.Contains(catLower, "nachrichten") || strings.Contains(catLower, "news") || strings.Contains(catLower, "magazin"):
			return "news", "dvb_category"
		case strings.Contains(catLower, "doku") || strings.Contains(catLower, "dokumentation") || strings.Contains(catLower, "reportage"):
			return "docu", "dvb_category"
		case strings.Contains(catLower, "kinder") || strings.Contains(catLower, "kids") || strings.Contains(catLower, "animation") || strings.Contains(catLower, "zeichentrick"):
			return "kids", "dvb_category"
		case strings.Contains(catLower, "show") || strings.Contains(catLower, "unterhaltung") || strings.Contains(catLower, "comedy"):
			return "show", "dvb_category"
		}
	}

	return "", ""
}

// EnrichProgramme parses canonical metadata once upon programme ingest.
func EnrichProgramme(prog *Programme) {
	if prog == nil {
		return
	}

	descText := ""
	if prog.Desc != nil {
		descText = prog.Desc.Text
	}

	cat := ""
	if len(prog.Category) > 0 {
		cat = prog.Category[0]
	}

	genre, genreSource := ExtractDVBGenre(0, cat)
	meta := ParseDescription(descText)

	prog.Canonical = &CanonicalMetadata{
		AgeRating:   ExtractAgeRating(prog.Title.Text, descText, ""),
		EpisodeInfo: ExtractEpisodeInfo(prog.Title.Text, descText),
		Genre:       genre,
		GenreSource: genreSource,
		Year:        meta.Year,
		Country:     meta.Country,
		Directors:   meta.Directors,
		Actors:      meta.Actors,
	}
}

// ParseDescription extracts legacy director, actor, and year metadata.
func ParseDescription(desc string) Metadata {
	meta := Metadata{}

	if match := yearCountryRegex.FindStringSubmatch(desc); len(match) == 3 {
		meta.Country = strings.TrimSpace(match[1])
		meta.Year = match[2]
	}

	if match := directorRegex.FindStringSubmatch(desc); len(match) > 1 {
		dirs := strings.Split(match[1], ",")
		for _, d := range dirs {
			if trimmed := strings.TrimSpace(d); trimmed != "" {
				meta.Directors = append(meta.Directors, trimmed)
			}
		}
	}

	if match := castRegex.FindStringSubmatch(desc); len(match) > 1 {
		actors := strings.Split(match[1], ",")
		for _, a := range actors {
			name := strings.Split(a, "(")[0]
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				meta.Actors = append(meta.Actors, trimmed)
			}
		}
	}

	return meta
}

func isValidAgeRating(r int) bool {
	return r == 0 || r == 6 || r == 12 || r == 16 || r == 18
}

func parseBroadcasterDescriptorRating(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil && isValidAgeRating(n) {
		return n, true
	}
	return 0, false
}
