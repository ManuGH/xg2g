package decision

import "testing"

func TestDecide_CodecNegotiationContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		in         Input
		wantPath   Path
		wantCodec  string
		wantReason Reason
		wantUseHW  bool
	}{
		{
			name: "direct when source codec/container are client compatible",
			in: Input{
				SourceCodec:      "h264",
				SourceContainer:  "mp4",
				ClientCodecs:     []string{"h264", "hevc"},
				ClientContainers: []string{"mp4", "fmp4"},
			},
			wantPath:   PathDirectPlay,
			wantCodec:  "h264",
			wantReason: ReasonDirectPlaySupported,
		},
		{
			name: "remux when codec is supported but container is not",
			in: Input{
				SourceCodec:      "h264",
				SourceContainer:  "mkv",
				ClientCodecs:     []string{"h264"},
				ClientContainers: []string{"mp4"},
			},
			wantPath:   PathRemux,
			wantCodec:  "h264",
			wantReason: ReasonRemuxRequired,
		},
		{
			name: "av1_hw prefers av1 on hw but falls back to hevc hw when av1 hw unavailable",
			in: Input{
				SourceCodec:  "vp9",
				ClientCodecs: []string{"h264", "hevc", "av1"},
				Profile:      "av1_hw",
				Server:       ServerCapabilities{HWAccelAvailable: true, SupportedHWCodecs: []string{"hevc", "h264"}},
			},
			wantPath:   PathTranscodeHW,
			wantCodec:  "hevc",
			wantReason: ReasonProfilePreference,
			wantUseHW:  true,
		},
		{
			name: "av1_required rejects when av1 hw is unavailable",
			in: Input{
				SourceCodec:  "vp9",
				ClientCodecs: []string{"h264", "hevc", "av1"},
				Profile:      "av1_required",
				Server:       ServerCapabilities{HWAccelAvailable: true, SupportedHWCodecs: []string{"hevc", "h264"}},
			},
			wantPath:   PathReject,
			wantCodec:  "",
			wantReason: ReasonHWCodecUnavailable,
		},
		{
			name: "cpu-only chooses h264 over hevc on cost",
			in: Input{
				SourceCodec:  "vp9",
				ClientCodecs: []string{"h264", "hevc"},
				Server:       ServerCapabilities{HWAccelAvailable: false},
			},
			wantPath:   PathTranscodeCPU,
			wantCodec:  "h264",
			wantReason: ReasonCPUPreferred,
		},
		{
			name: "gpu hevc beats cpu h264 when hevc hw is available",
			in: Input{
				SourceCodec:  "vp9",
				ClientCodecs: []string{"h264", "hevc"},
				Server:       ServerCapabilities{HWAccelAvailable: true, SupportedHWCodecs: []string{"hevc"}},
			},
			wantPath:   PathTranscodeHW,
			wantCodec:  "hevc",
			wantReason: ReasonCodecSelected,
			wantUseHW:  true,
		},
		{
			name: "safari_hevc is hard constrained to hevc even on cpu",
			in: Input{
				SourceCodec:  "vp9",
				ClientCodecs: []string{"h264", "hevc"},
				Profile:      "safari_hevc",
				Server:       ServerCapabilities{HWAccelAvailable: false},
			},
			wantPath:   PathTranscodeCPU,
			wantCodec:  "hevc",
			wantReason: ReasonProfileConstraint,
		},
		{
			name: "safari prefers h264 on cpu without hard constraint reason",
			in: Input{
				SourceCodec:  "vp9",
				ClientCodecs: []string{"h264", "hevc"},
				Profile:      "safari",
				Server:       ServerCapabilities{HWAccelAvailable: false},
			},
			wantPath:   PathTranscodeCPU,
			wantCodec:  "h264",
			wantReason: ReasonCPUPreferred,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Decide(tt.in)
			if got.Path != tt.wantPath {
				t.Fatalf("path mismatch: got=%q want=%q", got.Path, tt.wantPath)
			}
			if got.OutputCodec != tt.wantCodec {
				t.Fatalf("output codec mismatch: got=%q want=%q", got.OutputCodec, tt.wantCodec)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason mismatch: got=%q want=%q", got.Reason, tt.wantReason)
			}
			if got.UseHWAccel != tt.wantUseHW {
				t.Fatalf("useHW mismatch: got=%v want=%v", got.UseHWAccel, tt.wantUseHW)
			}
		})
	}
}
