// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidTemplateToken = regexp.MustCompile(`\{[^{}]*\}`)
)

// TemplateMetadata holds fields available for token replacement in media filenames.
type TemplateMetadata struct {
	Title        string    `json:"title"`
	ChannelName  string    `json:"channel_name"`
	StartTime    time.Time `json:"start_time"`
	Year         int       `json:"year,omitempty"`
	Season       int       `json:"season,omitempty"`
	Episode      int       `json:"episode,omitempty"`
	EpisodeTitle string    `json:"episode_title,omitempty"`
	AssetID      string    `json:"asset_id"`
}

// FormatMediaFilename formats filenames using safe token replacement based on NamingPreset.
func FormatMediaFilename(preset NamingPreset, templateStr string, meta TemplateMetadata, format ContainerFormat) string {
	ext := "." + string(format)
	cleanTitle := sanitizeFilenameSegment(meta.Title)
	cleanChannel := sanitizeFilenameSegment(meta.ChannelName)
	cleanEpTitle := sanitizeFilenameSegment(meta.EpisodeTitle)

	if meta.StartTime.IsZero() {
		meta.StartTime = time.Now()
	}
	dateStr := meta.StartTime.Format("2006-01-02")
	timeStr := meta.StartTime.Format("15-04")
	yearVal := meta.Year
	if yearVal == 0 {
		yearVal = meta.StartTime.Year()
	}

	var baseName string

	switch preset {
	case NamingPresetMovies:
		baseName = fmt.Sprintf("%s (%d)/%s (%d)", cleanTitle, yearVal, cleanTitle, yearVal)
	case NamingPresetSeries:
		if meta.Season > 0 && meta.Episode > 0 {
			if cleanEpTitle != "" {
				baseName = fmt.Sprintf("%s/Season %02d/%s - S%02dE%02d - %s", cleanTitle, meta.Season, cleanTitle, meta.Season, meta.Episode, cleanEpTitle)
			} else {
				baseName = fmt.Sprintf("%s/Season %02d/%s - S%02dE%02d", cleanTitle, meta.Season, cleanTitle, meta.Season, meta.Episode)
			}
		} else {
			// Fallback if season/episode missing
			baseName = fmt.Sprintf("%s/%s/%s - %s %s", cleanTitle, dateStr, cleanTitle, dateStr, timeStr)
		}
	case NamingPresetGenericTV:
		baseName = fmt.Sprintf("%s/%s/%s - %s %s", cleanChannel, dateStr, cleanTitle, dateStr, timeStr)
	case NamingPresetCustom:
		if templateStr == "" {
			baseName = fmt.Sprintf("%s/%s/%s - %s %s", cleanChannel, dateStr, cleanTitle, dateStr, timeStr)
		} else {
			replancer := strings.NewReplacer(
				"{title}", cleanTitle,
				"{channel}", cleanChannel,
				"{date}", dateStr,
				"{time}", timeStr,
				"{year}", fmt.Sprintf("%d", yearVal),
				"{season}", fmt.Sprintf("%02d", meta.Season),
				"{episode}", fmt.Sprintf("%02d", meta.Episode),
				"{episode_title}", cleanEpTitle,
			)
			baseName = replancer.Replace(templateStr)
			// Strip any unsupported residual tokens safely
			baseName = ErrInvalidTemplateToken.ReplaceAllString(baseName, "")
		}
	}

	cleanBase := filepath.Clean(baseName)
	return cleanBase + ext
}

// AppendCollisionSuffix appends a human-readable timestamp or asset short ID to resolve filename collisions safely.
func AppendCollisionSuffix(originalPath string, assetID string) string {
	ext := filepath.Ext(originalPath)
	base := strings.TrimSuffix(originalPath, ext)
	shortID := assetID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("%s - %s%s", base, shortID, ext)
}

func sanitizeFilenameSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return "Untitled"
	}
	// Replace illegal characters with dash
	for _, ch := range illegalChars {
		segment = strings.ReplaceAll(segment, ch, "-")
	}
	// Replace slash with dash for filename safety
	segment = strings.ReplaceAll(segment, "/", "-")
	return filepath.Clean(segment)
}
