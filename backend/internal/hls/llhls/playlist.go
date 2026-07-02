// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package llhls

import (
	"fmt"
	"strings"
)

// renderLLPlaylist augments FFmpeg's media playlist with the Low-Latency
// HLS tags: server-control (blocking reload), the part target, the parts of
// the segment currently being written, and a preload hint for the next part.
//
// Part durations are advertised as the nominal part target; FFmpeg cuts
// fragments on the frag_duration grid so real durations stay at or under
// the target, which is what PART-TARGET requires.
func renderLLPlaylist(base basePlaylist, cur openSegment, partTargetMs int) string {
	partTarget := float64(partTargetMs) / 1000.0
	// Apple requires PART-HOLD-BACK >= 2x part target; 3x is the
	// interoperable default that hls.js and Safari both accept.
	holdBack := 3 * partTarget

	var b strings.Builder
	lines := strings.Split(strings.TrimRight(base.raw, "\n"), "\n")
	injected := false
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
		if !injected && strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			fmt.Fprintf(&b, "#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK=%.3f\n", holdBack)
			fmt.Fprintf(&b, "#EXT-X-PART-INF:PART-TARGET=%.3f\n", partTarget)
			injected = true
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

	return b.String()
}
