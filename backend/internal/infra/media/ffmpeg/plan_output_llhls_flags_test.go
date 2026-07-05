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
		{name: "llhls fmp4 drops temp_file", lowLatency: true, container: "fmp4", wantTempFile: false},
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
			for _, required := range []string{"delete_segments", "append_list", "independent_segments", "program_date_time"} {
				if !strings.Contains(flags, required) {
					t.Errorf("hls_flags missing %q (flags: %q)", required, flags)
				}
			}

			argStr := strings.Join(args, " ")
			wantFrag := tc.lowLatency && tc.container == "fmp4"
			if hasFrag := strings.Contains(argStr, "frag_duration"); hasFrag != wantFrag {
				t.Errorf("frag_duration present = %v, want %v (args: %s)", hasFrag, wantFrag, argStr)
			}
		})
	}
}
