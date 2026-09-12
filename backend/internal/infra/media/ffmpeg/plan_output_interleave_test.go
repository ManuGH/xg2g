// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// TestAppendLiveHLSArgs_BoundsMuxerInterleaveDelta pins the muxer hold that a
// starved rendition may impose on the others. Without the bound FFmpeg waits
// its default 10s (staging 2026-09-12, audio PID dropped from the PMT: an 11s
// hole in the video and surviving-audio playlists). The option is an output
// option, so it must land after the last -i and before the playlist path.
func TestAppendLiveHLSArgs_BoundsMuxerInterleaveDelta(t *testing.T) {
	a := &LocalAdapter{HLSRoot: t.TempDir()}
	layout := liveSegmentLayout{segmentDurationSec: 2, listSize: 30}
	for _, multi := range []bool{false, true} {
		spec := ports.StreamSpec{
			SessionID: "sess",
			Mode:      ports.ModeLive,
			Profile:   ports.ProfileSpec{TranscodeVideo: true, Container: "fmp4"},
		}
		sel := liveAudioSelection{IsMultiAudio: multi}
		if multi {
			sel.VarStreamMap = "v:0,agroup:audio,default:yes a:0,agroup:audio,language:deu,default:yes"
		}
		args := a.appendLiveHLSArgs([]string{"-i", "pipe:0"}, spec, layout, sel)
		argStr := strings.Join(args, " ")
		if !strings.Contains(argStr, "-max_interleave_delta 2000000") {
			t.Fatalf("multi=%v: live hls args do not bound the muxer interleave delta: %s", multi, argStr)
		}
		if strings.Index(argStr, "-max_interleave_delta") < strings.Index(argStr, "-i pipe:0") {
			t.Fatalf("multi=%v: -max_interleave_delta placed before the input: %s", multi, argStr)
		}
	}
}
