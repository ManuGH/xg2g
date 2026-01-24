package decision

import (
	"reflect"
	"testing"
)

// INV-NORM-001: Normalization is idempotent.
// NormalizeInput(NormalizeInput(x)) == NormalizeInput(x)
func TestNormalizeInput_Idempotence_INV_NORM_001(t *testing.T) {
	t.Parallel()

	for _, tc := range normalizationVectors() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			once := NormalizeInput(tc.in)
			twice := NormalizeInput(once)

			if !reflect.DeepEqual(once, twice) {
				t.Fatalf("idempotence violated\nonce =%+v\ntwice=%+v\ninput=%+v",
					once, twice, tc.in)
			}
		})
	}
}

// INV-NORM-002: Normalization is deterministic.
// Same input -> same output (always).
func TestNormalizeInput_Determinism_INV_NORM_002(t *testing.T) {
	t.Parallel()

	const runs = 100

	for _, tc := range normalizationVectors() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			first := NormalizeInput(tc.in)
			for i := 0; i < runs; i++ {
				got := NormalizeInput(tc.in)
				if !reflect.DeepEqual(first, got) {
					t.Fatalf("determinism violated at run=%d\nfirst=%+v\ngot  =%+v\ninput=%+v",
						i, first, got, tc.in)
				}
			}
		})
	}
}

// INV-NORM-003: Unicode normalization produces canonical equivalence.
// Different unicode representations -> same normalized output.
//
// Tests robustNorm behavior with:
// - Whitespace variants (space, NBSP, tabs, newlines)
// - Zero-width characters (ZWSP, ZWNJ, ZWJ, BOM)
// - Case folding (ToLower)
func TestNormalizeInput_UnicodeCanonical_INV_NORM_003(t *testing.T) {
	t.Parallel()

	type pair struct {
		name string
		a    DecisionInput
		b    DecisionInput
	}

	pairs := []pair{
		{
			name: "whitespace_variants_container",
			a: DecisionInput{
				Source:       Source{Container: "mp4"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{Container: "  mp4  "},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "nbsp_edge_trim",
			a: DecisionInput{
				Source:       Source{VideoCodec: "h264"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{VideoCodec: "\u00A0h264\u00A0"}, // NBSP on edges
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "zero_width_space_edge_trim",
			a: DecisionInput{
				Source:       Source{Container: "mkv"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{Container: "\u200Bmkv\u200B"}, // ZWSP on edges
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "zero_width_non_joiner_edge_trim",
			a: DecisionInput{
				Source:       Source{AudioCodec: "aac"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{AudioCodec: "\u200Caac\u200C"}, // ZWNJ on edges
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "zero_width_joiner_edge_trim",
			a: DecisionInput{
				Source:       Source{VideoCodec: "hevc"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{VideoCodec: "\u200Dhevc\u200D"}, // ZWJ on edges
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "bom_edge_trim",
			a: DecisionInput{
				Source:       Source{Container: "avi"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{Container: "\uFEFFavi\uFEFF"}, // BOM on edges
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "case_folding_mixed",
			a: DecisionInput{
				Source:       Source{Container: "MP4", VideoCodec: "H264"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{Container: "mp4", VideoCodec: "h264"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "tabs_and_newlines",
			a: DecisionInput{
				Source:       Source{Container: "ts"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
			b: DecisionInput{
				Source:       Source{Container: "\t\nts\r\n"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "slice_order_independence",
			a: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"mp4", "mkv", "avi"},
					VideoCodecs: []string{"h264", "hevc"},
				},
				Policy: Policy{},
			},
			b: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"avi", "mp4", "mkv"}, // Different order
					VideoCodecs: []string{"hevc", "h264"},      // Different order
				},
				Policy: Policy{},
			},
		},
		{
			name: "slice_deduplication",
			a: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:    1,
					Containers: []string{"mp4", "mkv"},
				},
				Policy: Policy{},
			},
			b: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:    1,
					Containers: []string{"mp4", "MP4", "mkv", "mp4"}, // Duplicates + case variants
				},
				Policy: Policy{},
			},
		},
	}

	for _, tc := range pairs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			na := NormalizeInput(tc.a)
			nb := NormalizeInput(tc.b)

			if !reflect.DeepEqual(na, nb) {
				t.Fatalf("unicode canonical equivalence violated\nA=%+v\nB=%+v\nnorm(A)=%+v\nnorm(B)=%+v",
					tc.a, tc.b, na, nb)
			}
		})
	}
}

// INV-NORM-004: Nil slices normalize to empty slices (not nil).
// Ensures consistent comparison semantics.
func TestNormalizeInput_NilToEmpty_INV_NORM_004(t *testing.T) {
	t.Parallel()

	input := DecisionInput{
		Source: Source{Container: "mp4"},
		Capabilities: Capabilities{
			Version:     1,
			Containers:  nil, // Nil slice
			VideoCodecs: nil,
			AudioCodecs: nil,
		},
		Policy: Policy{},
	}

	normalized := NormalizeInput(input)

	// Nil slices must become empty slices
	if normalized.Capabilities.Containers == nil {
		t.Errorf("Containers slice is nil, expected empty slice")
	}
	if len(normalized.Capabilities.Containers) != 0 {
		t.Errorf("Containers slice has length %d, expected 0", len(normalized.Capabilities.Containers))
	}

	if normalized.Capabilities.VideoCodecs == nil {
		t.Errorf("VideoCodecs slice is nil, expected empty slice")
	}
	if len(normalized.Capabilities.VideoCodecs) != 0 {
		t.Errorf("VideoCodecs slice has length %d, expected 0", len(normalized.Capabilities.VideoCodecs))
	}

	if normalized.Capabilities.AudioCodecs == nil {
		t.Errorf("AudioCodecs slice is nil, expected empty slice")
	}
	if len(normalized.Capabilities.AudioCodecs) != 0 {
		t.Errorf("AudioCodecs slice has length %d, expected 0", len(normalized.Capabilities.AudioCodecs))
	}
}

// INV-NORM-005: Nil SupportsRange pointer normalizes to *false.
// Documents engine semantic: nil == false.
func TestNormalizeInput_NilSupportsRange_INV_NORM_005(t *testing.T) {
	t.Parallel()

	input := DecisionInput{
		Source: Source{Container: "mp4"},
		Capabilities: Capabilities{
			Version:       1,
			SupportsRange: nil, // Nil pointer
		},
		Policy: Policy{},
	}

	normalized := NormalizeInput(input)

	if normalized.Capabilities.SupportsRange == nil {
		t.Fatal("SupportsRange is nil, expected *false")
	}

	if *normalized.Capabilities.SupportsRange != false {
		t.Errorf("SupportsRange = %v, expected false", *normalized.Capabilities.SupportsRange)
	}
}

// INV-NORM-006: RequestID is preserved (not normalized).
// Documents tracing-only field semantics.
func TestNormalizeInput_RequestIDPreserved_INV_NORM_006(t *testing.T) {
	t.Parallel()

	input := DecisionInput{
		Source:       Source{Container: "mp4"},
		Capabilities: Capabilities{Version: 1},
		Policy:       Policy{},
		RequestID:    "TEST-REQUEST-123-MIXED-CASE",
	}

	normalized := NormalizeInput(input)

	// RequestID must NOT be normalized (case preserved, whitespace preserved)
	if normalized.RequestID != input.RequestID {
		t.Errorf("RequestID changed: got %q, want %q", normalized.RequestID, input.RequestID)
	}
}

type normVec struct {
	name string
	in   DecisionInput
}

// 15+ diverse inputs covering edge cases for normalization.
func normalizationVectors() []normVec {
	trueVal := true
	falseVal := false

	return []normVec{
		{
			name: "empty_input",
			in: DecisionInput{
				Source:       Source{},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "basic_normalized",
			in: DecisionInput{
				Source: Source{
					Container:  "mp4",
					VideoCodec: "h264",
					AudioCodec: "aac",
				},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"mp4"},
					VideoCodecs: []string{"h264"},
					AudioCodecs: []string{"aac"},
				},
				Policy: Policy{AllowTranscode: true},
			},
		},
		{
			name: "mixed_case_strings",
			in: DecisionInput{
				Source: Source{
					Container:  "MP4",
					VideoCodec: "H264",
					AudioCodec: "AAC",
				},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"MP4", "MKV"},
					VideoCodecs: []string{"H264", "HEVC"},
				},
				Policy: Policy{},
			},
		},
		{
			name: "whitespace_heavy",
			in: DecisionInput{
				Source: Source{
					Container:  "  mp4  ",
					VideoCodec: "\t\nh264\r\n",
					AudioCodec: "  aac  ",
				},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"  mp4  ", "  mkv  "},
					VideoCodecs: []string{"  h264  "},
				},
				Policy: Policy{},
			},
		},
		{
			name: "unicode_invisible_chars",
			in: DecisionInput{
				Source: Source{
					Container:  "\u200Bmp4\u200C",
					VideoCodec: "\u200Dh264\uFEFF",
					AudioCodec: "\u00A0aac",
				},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
			},
		},
		{
			name: "nil_slices",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  nil,
					VideoCodecs: nil,
					AudioCodecs: nil,
				},
				Policy: Policy{},
			},
		},
		{
			name: "empty_slices",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{},
					VideoCodecs: []string{},
					AudioCodecs: []string{},
				},
				Policy: Policy{},
			},
		},
		{
			name: "duplicate_values_in_slices",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"mp4", "mp4", "MP4", "mkv", "mp4"},
					VideoCodecs: []string{"h264", "H264", "hevc", "h264"},
				},
				Policy: Policy{},
			},
		},
		{
			name: "unsorted_slices",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"webm", "avi", "mp4", "mkv"},
					VideoCodecs: []string{"vp9", "av1", "h264", "hevc"},
				},
				Policy: Policy{},
			},
		},
		{
			name: "empty_strings_in_slices",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:     1,
					Containers:  []string{"mp4", "", "mkv", "  ", "\t"},
					VideoCodecs: []string{"", "h264"},
				},
				Policy: Policy{},
			},
		},
		{
			name: "supports_range_true",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:       1,
					SupportsRange: &trueVal,
				},
				Policy: Policy{},
			},
		},
		{
			name: "supports_range_false",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:       1,
					SupportsRange: &falseVal,
				},
				Policy: Policy{},
			},
		},
		{
			name: "supports_range_nil",
			in: DecisionInput{
				Source: Source{Container: "mp4"},
				Capabilities: Capabilities{
					Version:       1,
					SupportsRange: nil,
				},
				Policy: Policy{},
			},
		},
		{
			name: "all_fields_populated",
			in: DecisionInput{
				Source: Source{
					Container:   "MP4",
					VideoCodec:  "  H264  ",
					AudioCodec:  "AAC",
					BitrateKbps: 5000,
					Width:       1920,
					Height:      1080,
					FPS:         30.0,
				},
				Capabilities: Capabilities{
					Version:       1,
					Containers:    []string{"MP4", "MKV", "AVI"},
					VideoCodecs:   []string{"H264", "HEVC"},
					AudioCodecs:   []string{"AAC", "AC3"},
					SupportsHLS:   true,
					SupportsRange: &trueVal,
					MaxVideo:      &MaxVideoDimensions{Width: 3840, Height: 2160},
					DeviceType:    "  WEB  ",
				},
				Policy: Policy{
					AllowTranscode: true,
				},
				APIVersion: "  V3.1  ",
				RequestID:  "REQUEST-123",
			},
		},
		{
			name: "api_version_whitespace",
			in: DecisionInput{
				Source:       Source{Container: "mp4"},
				Capabilities: Capabilities{Version: 1},
				Policy:       Policy{},
				APIVersion:   "\t\nv3\r\n  ",
			},
		},
	}
}
