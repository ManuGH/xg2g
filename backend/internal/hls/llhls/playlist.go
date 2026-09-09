// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package llhls

import (
	"fmt"
	"strconv"
	"strings"
)

type segmentBlock struct {
	lines    []string
	duration float64
	uri      string
}

// parseExtinfDuration extracts the segment duration in seconds from an #EXTINF line.
func parseExtinfDuration(line string) float64 {
	val := strings.TrimPrefix(line, "#EXTINF:")
	if idx := strings.IndexByte(val, ','); idx >= 0 {
		val = val[:idx]
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// parsePlaylistSegments splits the raw playlist lines into header lines (before
// the first segment), individual segment blocks (preserving tags like
// #EXT-X-PROGRAM-DATE-TIME, #EXTINF, and the segment URI), and trailer lines.
func parsePlaylistSegments(lines []string) ([]string, []segmentBlock, []string) {
	var headers []string
	var segments []segmentBlock
	var trailers []string

	var curSegLines []string
	var curDur float64
	seenFirstSegment := false
	inTrailers := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if inTrailers {
			trailers = append(trailers, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#EXT-X-ENDLIST") {
			inTrailers = true
			trailers = append(trailers, line)
			continue
		}

		if !seenFirstSegment {
			if strings.HasPrefix(trimmed, "#EXTINF:") || strings.HasPrefix(trimmed, "#EXT-X-PROGRAM-DATE-TIME:") || strings.HasPrefix(trimmed, "#EXT-X-DISCONTINUITY") {
				seenFirstSegment = true
				curSegLines = append(curSegLines, line)
				if strings.HasPrefix(trimmed, "#EXTINF:") {
					curDur = parseExtinfDuration(trimmed)
				}
				continue
			}
			headers = append(headers, line)
			continue
		}

		curSegLines = append(curSegLines, line)
		if strings.HasPrefix(trimmed, "#EXTINF:") {
			curDur = parseExtinfDuration(trimmed)
		} else if !strings.HasPrefix(trimmed, "#") {
			segments = append(segments, segmentBlock{
				lines:    curSegLines,
				duration: curDur,
				uri:      trimmed,
			})
			curSegLines = nil
			curDur = 0
		}
	}

	if len(curSegLines) > 0 {
		if !seenFirstSegment {
			headers = append(headers, curSegLines...)
		} else {
			trailers = append(trailers, curSegLines...)
		}
	}

	return headers, segments, trailers
}

// renderLLPlaylist augments FFmpeg's media playlist with the Low-Latency
// HLS tags: server-control (blocking reload and delta skip boundary), the part
// target, the parts of the segment currently being written, and a preload hint
// for the next part.
//
// When skipParam is "YES" or "v2", and the playlist duration is at least
// CAN-SKIP-UNTIL, older segments are replaced with #EXT-X-SKIP:SKIPPED-SEGMENTS=N
// according to RFC 8216bis §4.4.5.2.
//
// Part durations are advertised as the nominal part target; FFmpeg cuts
// fragments on the frag_duration grid so real durations stay at or under
// the target, which is what PART-TARGET requires.
func renderLLPlaylist(base basePlaylist, cur openSegment, partTargetMs int, skipParam string) string {
	partTarget := float64(partTargetMs) / 1000.0
	// Apple requires PART-HOLD-BACK >= 2x part target; 3x is the
	// interoperable default that hls.js and Safari both accept.
	holdBack := 3 * partTarget

	lines := strings.Split(strings.TrimRight(base.raw, "\n"), "\n")

	// Determine target duration (spec requires CAN-SKIP-UNTIL >= 6 * TARGETDURATION)
	targetDur := base.targetDur
	if targetDur <= 0 {
		for _, l := range lines {
			if strings.HasPrefix(l, "#EXT-X-TARGETDURATION:") {
				if d, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l, "#EXT-X-TARGETDURATION:"))); err == nil && d > 0 {
					targetDur = d
					break
				}
			}
		}
		if targetDur <= 0 {
			targetDur = 2
		}
	}
	skipUntil := float64(targetDur * 6)

	headers, segments, trailers := parsePlaylistSegments(lines)

	var b strings.Builder
	for _, line := range headers {
		b.WriteString(line)
		b.WriteByte('\n')
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			fmt.Fprintf(&b, "#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK=%.3f,CAN-SKIP-UNTIL=%.3f\n", holdBack, skipUntil)
			fmt.Fprintf(&b, "#EXT-X-PART-INF:PART-TARGET=%.3f\n", partTarget)
		}
	}

	shouldSkip := (skipParam == "YES" || skipParam == "v2") && len(segments) > 0
	skippedCount := 0
	keepFromIdx := 0

	if shouldSkip {
		accum := 0.0
		for i := len(segments) - 1; i >= 0; i-- {
			d := segments[i].duration
			if d <= 0 {
				d = float64(targetDur)
			}
			accum += d
			if accum >= skipUntil {
				keepFromIdx = i
				break
			}
		}
		// Only produce a delta playlist if total duration >= CAN-SKIP-UNTIL and at least one segment is skipped
		if accum >= skipUntil && keepFromIdx > 0 {
			skippedCount = keepFromIdx
		}
	}

	if skippedCount > 0 {
		fmt.Fprintf(&b, "#EXT-X-SKIP:SKIPPED-SEGMENTS=%d\n", skippedCount)
		for _, seg := range segments[keepFromIdx:] {
			for _, sl := range seg.lines {
				b.WriteString(sl)
				b.WriteByte('\n')
			}
		}
	} else {
		for _, seg := range segments {
			for _, sl := range seg.lines {
				b.WriteString(sl)
				b.WriteByte('\n')
			}
		}
	}

	if cur.name != "" {
		var nextOffset int64
		for _, p := range cur.parts {
			attrs := fmt.Sprintf(`#EXT-X-PART:DURATION=%.3f,URI="%s",BYTERANGE="%d@%d"`, partTarget, cur.name, p.Size, p.Offset)
			if p.Independent {
				attrs += ",INDEPENDENT=YES"
			}
			b.WriteString(attrs)
			b.WriteByte('\n')
			nextOffset = p.Offset + p.Size
		}
		fmt.Fprintf(&b, "#EXT-X-PRELOAD-HINT:TYPE=PART,URI=\"%s\",BYTERANGE-START=%d\n", cur.name, nextOffset)
	}

	for _, line := range trailers {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}
