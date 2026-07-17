// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

// TestUseCMAFSegmenter pins the LL pipe-mode condition: transcode + fmp4 +
// LowLatencyHLS. Copy sources keep the hls muxer (unknown GOP would break
// rotation), as does mpegts.
func TestUseCMAFSegmenter(t *testing.T) {
	cases := []struct {
		name       string
		lowLatency bool
		transcode  bool
		container  string
		want       bool
	}{
		{"ll transcode fmp4", true, true, "fmp4", false},
		{"ll copy fmp4", true, false, "fmp4", false},
		{"ll transcode ts", true, true, "ts", false},
		{"standard transcode fmp4", false, true, "fmp4", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &LocalAdapter{LowLatencyHLS: tc.lowLatency}
			spec := ports.StreamSpec{Profile: ports.ProfileSpec{
				TranscodeVideo: tc.transcode,
				Container:      tc.container,
			}}
			if got := a.useCMAFSegmenter(spec); got != tc.want {
				t.Errorf("useCMAFSegmenter = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAppendLiveCMAFStreamArgs pins the stream contract the segmenter
// depends on: fragments cut on keyframes and the part grid, no AVIO
// buffering, output on stdout.
func TestAppendLiveCMAFStreamArgs(t *testing.T) {
	argStr := strings.Join(appendLiveCMAFStreamArgs(nil), " ")
	for _, want := range []string{
		"-f mp4",
		"frag_keyframe",
		"empty_moov",
		"default_base_moof",
		"-frag_duration 500000",
		"-flush_packets 1",
		"pipe:1",
	} {
		if !strings.Contains(argStr, want) {
			t.Errorf("cmaf args missing %q: %s", want, argStr)
		}
	}
	if strings.Contains(argStr, "hls") {
		t.Errorf("cmaf args must not contain hls muxer options: %s", argStr)
	}
}

// The LL-HLS packager (internal/hls/llhls) scans the segment FFmpeg is
// currently writing to cut EXT-X-PART entries. With temp_file the open
// segment only exists as seg_N.m4s.tmp until it completes, so the packager
// never sees parts and Safari degrades to full-segment blocking reloads.
// temp_file must therefore stay off exactly when the LL path is active.
func TestAppendLiveHLSArgs_TempFileFlag(t *testing.T) {
	cases := []struct {
		name         string
		lowLatency   bool
		container    string
		wantTempFile bool
	}{
		{name: "llhls fmp4 drops temp_file", lowLatency: true, container: "fmp4", wantTempFile: true}, // Changed: temp_file is now always true to fix fmp4 partial reads on Safari
		{name: "standard fmp4 keeps temp_file", lowLatency: false, container: "fmp4", wantTempFile: true},
		{name: "llhls mpegts keeps temp_file", lowLatency: true, container: "ts", wantTempFile: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &LocalAdapter{
				HLSRoot:       t.TempDir(),
				LowLatencyHLS: tc.lowLatency,
				Logger:        zerolog.Nop(),
			}
			spec := ports.StreamSpec{
				SessionID: "test-sess-llhls",
				Profile:   ports.ProfileSpec{Container: tc.container},
			}
			layout := liveSegmentLayout{segmentDurationSec: 2, listSize: 6}

			args := adapter.appendLiveHLSArgs(nil, spec, layout)

			var flags string
			for i, a := range args {
				if a == "-hls_flags" && i+1 < len(args) {
					flags = args[i+1]
					break
				}
			}
			if flags == "" {
				t.Fatalf("no -hls_flags in args: %v", args)
			}

			hasTempFile := strings.Contains(flags, "temp_file")
			if hasTempFile != tc.wantTempFile {
				t.Errorf("temp_file in hls_flags = %v, want %v (flags: %q)", hasTempFile, tc.wantTempFile, flags)
			}
			for _, required := range []string{"delete_segments", "append_list", "program_date_time"} {
				if !strings.Contains(flags, required) {
					t.Errorf("hls_flags missing %q (flags: %q)", required, flags)
				}
			}
			if strings.Contains(flags, "independent_segments") {
				t.Errorf("hls_flags unexpectedly contains independent_segments in copy mode (flags: %q)", flags)
			}

			argStr := strings.Join(args, " ")
			wantFrag := tc.lowLatency && tc.container == "fmp4"
			if hasFrag := strings.Contains(argStr, "frag_duration"); hasFrag != wantFrag {
				t.Errorf("frag_duration present = %v, want %v (args: %s)", hasFrag, wantFrag, argStr)
			}
		})
	}
}

func TestAppendLiveHLSArgs_IndependentSegmentsFlag(t *testing.T) {
	adapter := &LocalAdapter{
		HLSRoot: t.TempDir(),
		Logger:  zerolog.Nop(),
	}
	cases := []struct {
		name           string
		transcodeVideo bool
		wantFlag       bool
	}{
		{"copy mode omits independent_segments", false, false},
		{"transcode mode includes independent_segments", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := ports.StreamSpec{
				SessionID: "test-sess-indep",
				Profile: ports.ProfileSpec{
					TranscodeVideo: tc.transcodeVideo,
					Container:      "ts",
				},
			}
			args := adapter.appendLiveHLSArgs(nil, spec, liveSegmentLayout{segmentDurationSec: 2, listSize: 6})
			var flags string
			for i, a := range args {
				if a == "-hls_flags" && i+1 < len(args) {
					flags = args[i+1]
					break
				}
			}
			gotFlag := strings.Contains(flags, "independent_segments")
			if gotFlag != tc.wantFlag {
				t.Errorf("independent_segments present = %v, want %v (flags: %q)", gotFlag, tc.wantFlag, flags)
			}
		})
	}
}
