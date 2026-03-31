package decision

import (
	"testing"

	mediacodec "github.com/ManuGH/xg2g/internal/media/codec"
)

type videoDivergenceCase struct {
	name                 string
	category             string
	input                DecisionInput
	sourceOverride       *mediacodec.VideoCapability
	clientOverride       *mediacodec.VideoCapability
	wantLegacyCompatible bool
	wantNewCompatible    bool
	wantDivergence       bool
	wantNewStricter      bool
	wantNewReasons       []mediacodec.CompatibilityReason
	wantNewPathCorrect   bool
}

func TestVideoCompatibilityDivergenceMatrix(t *testing.T) {
	trueVal := true

	cases := []videoDivergenceCase{
		{
			name:     "baseline h264 within limits",
			category: "baseline",
			input: DecisionInput{
				Source: Source{
					Container:  "mp4",
					VideoCodec: "h264",
					AudioCodec: "aac",
					Width:      1920,
					Height:     1080,
					FPS:        25,
				},
				Capabilities: Capabilities{
					Containers:    []string{"mp4"},
					VideoCodecs:   []string{"h264"},
					AudioCodecs:   []string{"aac"},
					SupportsHLS:   true,
					SupportsRange: &trueVal,
					MaxVideo: &MaxVideoDimensions{
						Width:  1920,
						Height: 1080,
						FPS:    50,
					},
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: true,
			wantNewCompatible:    true,
			wantDivergence:       false,
			wantNewPathCorrect:   true,
		},
		{
			name:     "baseline unknown source dimensions with no client limit",
			category: "baseline",
			input: DecisionInput{
				Source: Source{
					Container:  "mpegts",
					VideoCodec: "h264",
					AudioCodec: "aac",
				},
				Capabilities: Capabilities{
					Containers:  []string{"mpegts"},
					VideoCodecs: []string{"h264"},
					AudioCodecs: []string{"aac"},
					SupportsHLS: true,
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: true,
			wantNewCompatible:    true,
			wantDivergence:       false,
			wantNewPathCorrect:   true,
		},
		{
			name:     "baseline source resolution exceeds client limit",
			category: "baseline",
			input: DecisionInput{
				Source: Source{
					Container:  "mp4",
					VideoCodec: "h264",
					AudioCodec: "aac",
					Width:      3840,
					Height:     2160,
					FPS:        25,
				},
				Capabilities: Capabilities{
					Containers:    []string{"mp4"},
					VideoCodecs:   []string{"h264"},
					AudioCodecs:   []string{"aac"},
					SupportsHLS:   true,
					SupportsRange: &trueVal,
					MaxVideo: &MaxVideoDimensions{
						Width:  1920,
						Height: 1080,
						FPS:    50,
					},
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: false,
			wantNewCompatible:    false,
			wantDivergence:       false,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonResolutionExceeded,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "baseline source frame rate exceeds client limit",
			category: "baseline",
			input: DecisionInput{
				Source: Source{
					Container:  "mp4",
					VideoCodec: "h264",
					AudioCodec: "aac",
					Width:      1920,
					Height:     1080,
					FPS:        50,
				},
				Capabilities: Capabilities{
					Containers:    []string{"mp4"},
					VideoCodecs:   []string{"h264"},
					AudioCodecs:   []string{"aac"},
					SupportsHLS:   true,
					SupportsRange: &trueVal,
					MaxVideo: &MaxVideoDimensions{
						Width:  1920,
						Height: 1080,
						FPS:    30,
					},
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: false,
			wantNewCompatible:    false,
			wantDivergence:       false,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonFrameRateExceeded,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "baseline mpeg2 source mismatches modern client codecs",
			category: "baseline",
			input: DecisionInput{
				Source: Source{
					Container:  "mpegts",
					VideoCodec: "mpeg2",
					AudioCodec: "mp2",
					Width:      720,
					Height:     576,
					FPS:        25,
				},
				Capabilities: Capabilities{
					Containers:  []string{"mpegts"},
					VideoCodecs: []string{"h264", "hevc"},
					AudioCodecs: []string{"aac", "ac3"},
					SupportsHLS: true,
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: false,
			wantNewCompatible:    false,
			wantDivergence:       false,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonCodecMismatch,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "stricter interlaced source",
			category: "stricter",
			input: DecisionInput{
				Source: Source{
					Container:  "mpegts",
					VideoCodec: "h264",
					AudioCodec: "aac",
					Width:      1920,
					Height:     1080,
					FPS:        50,
					Interlaced: true,
				},
				Capabilities: Capabilities{
					Containers:  []string{"mpegts"},
					VideoCodecs: []string{"h264"},
					AudioCodecs: []string{"aac"},
					SupportsHLS: true,
					MaxVideo: &MaxVideoDimensions{
						Width:  1920,
						Height: 1080,
						FPS:    50,
					},
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: false,
			wantNewCompatible:    false,
			wantDivergence:       false,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonInterlacedSource,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "baseline legacy fallback without dimensions or fps against bounded client",
			category: "baseline",
			input: DecisionInput{
				Source: Source{
					Container:  "mpegts",
					VideoCodec: "h264",
					AudioCodec: "aac",
				},
				Capabilities: Capabilities{
					Containers:  []string{"mpegts"},
					VideoCodecs: []string{"h264"},
					AudioCodecs: []string{"aac"},
					SupportsHLS: true,
					MaxVideo: &MaxVideoDimensions{
						Width:  1920,
						Height: 1080,
						FPS:    50,
					},
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: false,
			wantNewCompatible:    false,
			wantDivergence:       false,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonResolutionUnknown,
				mediacodec.ReasonFrameRateUnknown,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "stricter unknown codec string no longer matches by accident",
			category: "stricter",
			input: DecisionInput{
				Source: Source{
					Container:  "mpegts",
					VideoCodec: "mysterycodec",
					AudioCodec: "aac",
					Width:      1280,
					Height:     720,
					FPS:        25,
				},
				Capabilities: Capabilities{
					Containers:  []string{"mpegts"},
					VideoCodecs: []string{"mysterycodec"},
					AudioCodecs: []string{"aac"},
					SupportsHLS: true,
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: true,
			wantNewCompatible:    false,
			wantDivergence:       true,
			wantNewStricter:      true,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonCodecMismatch,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "stricter hevc main10 source against main client",
			category: "stricter",
			input: DecisionInput{
				Source: Source{
					Container:  "fmp4",
					VideoCodec: "hevc",
					AudioCodec: "aac",
					Width:      1920,
					Height:     1080,
					FPS:        25,
				},
				Capabilities: Capabilities{
					Containers:  []string{"fmp4"},
					VideoCodecs: []string{"hevc"},
					AudioCodecs: []string{"aac"},
					SupportsHLS: true,
				},
				Policy: Policy{AllowTranscode: true},
			},
			sourceOverride: &mediacodec.VideoCapability{
				Codec:        mediacodec.IDHEVC,
				BitDepth:     10,
				MaxRes:       mediacodec.Resolution{Width: 1920, Height: 1080},
				MaxFrameRate: mediacodec.FrameRate{Numerator: 25, Denominator: 1},
			},
			clientOverride: &mediacodec.VideoCapability{
				Codec:        mediacodec.IDHEVC,
				BitDepth:     8,
				MaxRes:       mediacodec.Resolution{Width: 1920, Height: 1080},
				MaxFrameRate: mediacodec.FrameRate{Numerator: 50, Denominator: 1},
			},
			wantLegacyCompatible: true,
			wantNewCompatible:    false,
			wantDivergence:       true,
			wantNewStricter:      true,
			wantNewReasons: []mediacodec.CompatibilityReason{
				mediacodec.ReasonBitDepthExceeded,
			},
			wantNewPathCorrect: true,
		},
		{
			name:     "looser completely unknown bit depth remains neutral",
			category: "looser",
			input: DecisionInput{
				Source: Source{
					Container:  "mpegts",
					VideoCodec: "hevc",
					AudioCodec: "aac",
					Width:      1920,
					Height:     1080,
					FPS:        25,
				},
				Capabilities: Capabilities{
					Containers:  []string{"mpegts"},
					VideoCodecs: []string{"hevc"},
					AudioCodecs: []string{"aac"},
					SupportsHLS: true,
				},
				Policy: Policy{AllowTranscode: true},
			},
			wantLegacyCompatible: true,
			wantNewCompatible:    true,
			wantDivergence:       false,
			wantNewPathCorrect:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			normalized := NormalizeInput(tc.input)
			legacy := legacyVideoCompatibleWithoutRepair(normalized)
			sourceCapability := sourceToVideoCapability(normalized.Source)
			if tc.sourceOverride != nil {
				sourceCapability = *tc.sourceOverride
			}
			clientCapability := clientToVideoCapabilityForSource(normalized.Capabilities, normalized.Source)
			if tc.clientOverride != nil {
				clientCapability = *tc.clientOverride
			}
			newResult := EvaluateVideoCompatibility(sourceCapability, clientCapability)
			newCompatible := newResult.Compatible()
			divergence := legacy != newCompatible

			if legacy != tc.wantLegacyCompatible {
				t.Fatalf("legacy compatibility mismatch: got=%v want=%v", legacy, tc.wantLegacyCompatible)
			}
			if newCompatible != tc.wantNewCompatible {
				t.Fatalf("new compatibility mismatch: got=%v want=%v reasons=%+v", newCompatible, tc.wantNewCompatible, newResult.Reasons)
			}
			if divergence != tc.wantDivergence {
				t.Fatalf("divergence mismatch: got=%v want=%v reasons=%+v", divergence, tc.wantDivergence, newResult.Reasons)
			}
			if divergence && tc.wantNewStricter && !(legacy && !newCompatible) {
				t.Fatalf("expected new path to be stricter: legacy=%v new=%v reasons=%+v", legacy, newCompatible, newResult.Reasons)
			}
			if !tc.wantNewPathCorrect {
				t.Fatalf("test case must explicitly assert new path correctness")
			}
			for _, reason := range tc.wantNewReasons {
				if !newResult.Has(reason) {
					t.Fatalf("expected reason %q, got %+v", reason, newResult.Reasons)
				}
			}
		})
	}
}

// legacyVideoCompatibleWithoutRepair mirrors the current string-based predicate
// behavior for divergence testing. Keep this in sync with computePredicates'
// video logic until the typed path becomes authoritative.
func legacyVideoCompatibleWithoutRepair(input DecisionInput) bool {
	canVideo := contains(input.Capabilities.VideoCodecs, input.Source.VideoCodec) && withinMaxVideo(input.Source, input.Capabilities.MaxVideo)
	videoRepairRequired := sourceRequiresVideoRepair(input.Source, input.Capabilities.MaxVideo)
	return canVideo && !videoRepairRequired
}
