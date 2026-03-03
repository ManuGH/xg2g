package hls

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SegmentTruth represents authoritative metadata derived from the playlist
type SegmentTruth struct {
	HasPDT        bool
	FirstPDT      time.Time
	LastPDT       time.Time
	LastDuration  time.Duration
	TotalDuration time.Duration
	IsVOD         bool // Derived from #EXT-X-PLAYLIST-TYPE:VOD or #EXT-X-ENDLIST
}

// ExtractSegmentTruth parses a playlist to extract authoritative timeline metadata.
// It implements critical guards:
// 1. Playlist Type: Detects VOD vs EVENT/LIVE
// 2. Monotonicity: Ensures PDT never jumps backwards (Live/Event)
// 3. Durations: Sums EXTINF for total duration
func ExtractSegmentTruth(playlist string) (*SegmentTruth, error) {
	scanner := bufio.NewScanner(strings.NewReader(playlist))
	truth := &SegmentTruth{}

	var (
		nextDuration       time.Duration
		nextPDT            time.Time
		hasEndList         bool
		hasPlaylistTypeVOD bool

		segmentCount    int
		lastPDT         time.Time
		segmentsWithPDT int
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:VOD") {
			hasPlaylistTypeVOD = true
			continue
		}
		if line == "#EXT-X-ENDLIST" {
			hasEndList = true
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:") {
			pdtStr := strings.TrimPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:")
			t, err := time.Parse(time.RFC3339Nano, pdtStr)
			if err != nil {
				// Retry without Nano if needed
				t, err = time.Parse(time.RFC3339, pdtStr)
				if err != nil {
					// Guard: strict fail on corrupt PDT
					return nil, fmt.Errorf("invalid PDT format: %s", pdtStr)
				}
			}

			// Monotonicity Check
			if !lastPDT.IsZero() {
				if t.Before(lastPDT) {
					return nil, fmt.Errorf("PDT non-monotonic: %v < %v", t, lastPDT)
				}
			}
			nextPDT = t
			lastPDT = t
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			// Format: #EXTINF:10.000,
			durPart := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.Index(durPart, ","); idx != -1 {
				durPart = durPart[:idx]
			}
			secs, err := strconv.ParseFloat(durPart, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid EXTINF duration: %s", durPart)
			}
			nextDuration = time.Duration(secs * float64(time.Second))
			continue
		}

		// URI Line (Start of a Segment)
		if !strings.HasPrefix(line, "#") {
			segmentCount++

			// Apply Duration
			truth.TotalDuration += nextDuration
			truth.LastDuration = nextDuration

			// Apply PDT
			if !nextPDT.IsZero() {
				segmentsWithPDT++
				if truth.FirstPDT.IsZero() {
					truth.FirstPDT = nextPDT
				}
				truth.LastPDT = nextPDT // Current segment start
			}

			// Reset transient state
			nextDuration = 0
			nextPDT = time.Time{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Determine Type
	truth.IsVOD = hasPlaylistTypeVOD || hasEndList
	truth.HasPDT = segmentsWithPDT > 0

	// Guard: Live/Event Partial Coverage Check
	// If NOT VOD, and we have SOME PDT labels but not ALL, that is a broken live stream.
	if !truth.IsVOD && truth.HasPDT && segmentsWithPDT != segmentCount {
		return nil, fmt.Errorf("partial PDT coverage in live playlist (found %d/%d)", segmentsWithPDT, segmentCount)
	}

	return truth, nil
}
