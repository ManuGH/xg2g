package decision

import (
	"math/rand"
	"os"
	"strconv"
	"testing"
)

// GenValidDecisionInput produces a valid, well-formed input.
func GenValidDecisionInput(r *rand.Rand) DecisionInput {
	containers := []string{"mp4", "mkv", "avi", "mov", "ts"}
	codecs := []string{"h264", "hevc", "aac", "ac3", "mp3"}

	// Valid Source
	src := Source{
		Container:   sample(r, containers),
		VideoCodec:  sample(r, codecs),
		AudioCodec:  sample(r, codecs),
		BitrateKbps: 1000 + r.Intn(5000),
		Width:       1920,
		Height:      1080,
		FPS:         30.0,
	}

	// Valid Capabilities (mostly superset of source for validity)
	caps := Capabilities{
		Version:       1,
		Containers:    subset(r, containers),
		VideoCodecs:   subset(r, codecs),
		AudioCodecs:   subset(r, codecs),
		SupportsHLS:   r.Intn(2) == 0,
		SupportsRange: boolPtr(r.Intn(2) == 0),
		DeviceType:    "proof-gen",
	}

	// Valid Policy
	policy := Policy{
		AllowTranscode: r.Intn(2) == 0,
	}

	return DecisionInput{
		Source:       src,
		Capabilities: caps,
		Policy:       policy,
		APIVersion:   "v3",
		RequestID:    "gen-valid",
	}
}

// GenInvalidSchemaInput produces inputs that fail SCHEMA validation (Rule Red-2).
// Returns input and expected HTTP Status Code (400, 412, 422).
func GenInvalidSchemaInput(r *rand.Rand) (DecisionInput, int) {
	base := GenValidDecisionInput(r)

	caseIdx := r.Intn(3)
	switch caseIdx {
	case 0:
		// V-1: Capabilities Missing (412)
		base.APIVersion = "v3.1"
		base.Capabilities = Capabilities{} // Empty
		return base, 412
	case 1:
		// V-2: Capabilities Invalid Version (400)
		base.Capabilities.Version = 999
		return base, 400
	default:
		// V-3: Source Incomplete (422)
		base.Source.Container = "" // Empty
		return base, 422
	}
}

// GenInvalidLogicInput produces inputs that are valid schema but invalid for playback (fail-closed logic).
// Returns input and expected ReasonCode.
func GenInvalidLogicInput(r *rand.Rand) (DecisionInput, ReasonCode) {
	base := GenValidDecisionInput(r)

	caseIdx := r.Intn(3)
	switch caseIdx {
	case 0:
		// Container Mismatch (Fail Closed)
		base.Capabilities.Containers = []string{"other"}
		base.Source.Container = "needed"
		base.Policy.AllowTranscode = false    // Force deny Transcode
		base.Capabilities.SupportsHLS = false // Force deny DirectStream
		return base, ReasonContainerNotSupported
	case 1:
		// Video Codec Mismatch + Policy Deny
		base.Capabilities.Containers = []string{"needed"}
		base.Source.Container = "needed"
		base.Capabilities.VideoCodecs = []string{"other"}
		base.Source.VideoCodec = "needed"
		base.Policy.AllowTranscode = false
		return base, ReasonPolicyDeniesTranscode
	default:
		// Audio Codec Mismatch + Policy Deny
		base.Capabilities.Containers = []string{"needed"}
		base.Source.Container = "needed"
		base.Capabilities.VideoCodecs = []string{base.Source.VideoCodec}
		base.Capabilities.AudioCodecs = []string{"other"}
		base.Source.AudioCodec = "needed"
		base.Policy.AllowTranscode = false
		return base, ReasonPolicyDeniesTranscode
	}
}

// GenContainerMismatchDPOnly produces inputs where DirectPlay fails ONLY due to Container.
// Codecs match, Range ok, but Container not in Caps. DS should be possible if HLS true.
func GenContainerMismatchDPOnly(r *rand.Rand) DecisionInput {
	codec := "h264"
	audio := "aac"

	return DecisionInput{
		Source: Source{
			Container:   "mkv", // Not in Caps
			VideoCodec:  codec,
			AudioCodec:  audio,
			BitrateKbps: 3000,
			Width:       1920,
			Height:      1080,
			FPS:         30.0,
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4", "mov"}, // Does NOT include mkv
			VideoCodecs:   []string{codec, "hevc"},
			AudioCodecs:   []string{audio, "ac3"},
			SupportsHLS:   true, // DS possible
			SupportsRange: boolPtr(true),
			DeviceType:    "container-test",
		},
		Policy: Policy{
			AllowTranscode: r.Intn(2) == 0, // Random policy
		},
		APIVersion: "v3",
		RequestID:  "gen-container-mismatch",
	}
}

// GenContainerMismatchDSPossible produces inputs where Container mismatches but DS is definitely possible.
// Strict: HLS true, Codecs match. Mode MUST be DirectStream (not Transcode).
func GenContainerMismatchDSPossible(r *rand.Rand) DecisionInput {
	input := GenContainerMismatchDPOnly(r)
	// Force DS to be the winner
	input.Capabilities.SupportsHLS = true
	// Ensure codecs match (already done in GenContainerMismatchDPOnly)
	return input
}

// GenContainerCaseVariants produces inputs with case/whitespace variations for container.
// Used to test normalization consistency (R2-001 gate).
func GenContainerCaseVariants(r *rand.Rand) DecisionInput {
	// Case variants of MP4 family containers
	variants := []string{"MP4", " mp4 ", "Mov", "M4V", "mP4", "MOV", " M4v"}
	container := variants[r.Intn(len(variants))]

	return DecisionInput{
		Source: Source{
			Container:   container,
			VideoCodec:  "h264",
			AudioCodec:  "aac",
			BitrateKbps: 3000,
			Width:       1920,
			Height:      1080,
			FPS:         30.0,
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4", "mov", "m4v"}, // Normalized versions
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: boolPtr(true),
			DeviceType:    "normalization-test",
		},
		Policy: Policy{
			AllowTranscode: true,
		},
		APIVersion: "v3",
		RequestID:  "gen-case-variant",
	}
}

// GenDuplicateCapsVariants produces inputs with duplicate entries in capability slices.
// R2-B Attack #3: Hash dedupes but contains() doesn't - potential proof drift.
func GenDuplicateCapsVariants(r *rand.Rand) (DecisionInput, DecisionInput) {
	// Base input with NO duplicates
	base := DecisionInput{
		Source: Source{
			Container:   "mp4",
			VideoCodec:  "h264",
			AudioCodec:  "aac",
			BitrateKbps: 3000,
			Width:       1920,
			Height:      1080,
			FPS:         30.0,
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4", "mkv"},
			VideoCodecs:   []string{"h264", "hevc"},
			AudioCodecs:   []string{"aac", "ac3"},
			SupportsHLS:   true,
			SupportsRange: boolPtr(true),
			DeviceType:    "duplicate-test",
		},
		Policy: Policy{
			AllowTranscode: true,
		},
		APIVersion: "v3",
		RequestID:  "gen-no-dups",
	}

	// Variant with DUPLICATES
	duped := base
	duped.RequestID = "gen-with-dups"
	duped.Capabilities.Containers = []string{"mp4", "mp4", "mkv", "mkv"}
	duped.Capabilities.VideoCodecs = []string{"h264", "h264", "hevc"}
	duped.Capabilities.AudioCodecs = []string{"aac", "aac", "ac3"}

	return base, duped
}

// GenNilVsEmptySliceVariants produces inputs with nil vs empty slices.
// R2-B Attack #2: Semantic equivalence but potential JSON/Hash divergence.
func GenNilVsEmptySliceVariants(r *rand.Rand) (DecisionInput, DecisionInput) {
	// Base with empty slices
	withEmpty := DecisionInput{
		Source: Source{
			Container:   "mkv",
			VideoCodec:  "h264",
			AudioCodec:  "aac",
			BitrateKbps: 3000,
			Width:       1920,
			Height:      1080,
			FPS:         30.0,
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{}, // Empty slice
			VideoCodecs:   []string{}, // Empty slice
			AudioCodecs:   []string{}, // Empty slice
			SupportsHLS:   false,
			SupportsRange: boolPtr(false),
			DeviceType:    "nil-empty-test",
		},
		Policy: Policy{
			AllowTranscode: false,
		},
		APIVersion: "v3",
		RequestID:  "gen-empty-slices",
	}

	// Variant with nil slices
	withNil := withEmpty
	withNil.RequestID = "gen-nil-slices"
	withNil.Capabilities.Containers = nil
	withNil.Capabilities.VideoCodecs = nil
	withNil.Capabilities.AudioCodecs = nil

	return withEmpty, withNil
}

// GenNilVsFalseRangeVariants produces inputs with nil vs false SupportsRange.
// R2-B Attack #1: Tri-state semantics - both mean "no range" but may differ in hash.
func GenNilVsFalseRangeVariants(r *rand.Rand) (DecisionInput, DecisionInput) {
	base := DecisionInput{
		Source: Source{
			Container:   "mp4",
			VideoCodec:  "h264",
			AudioCodec:  "aac",
			BitrateKbps: 3000,
			Width:       1920,
			Height:      1080,
			FPS:         30.0,
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: boolPtr(false), // Explicit false
			DeviceType:    "range-test",
		},
		Policy: Policy{
			AllowTranscode: true,
		},
		APIVersion: "v3",
		RequestID:  "gen-range-false",
	}

	withNilRange := base
	withNilRange.RequestID = "gen-range-nil"
	withNilRange.Capabilities.SupportsRange = nil // nil

	return base, withNilRange
}

// GenMonotonicPair produces (A, B) where Caps(B) >= Caps(A).
// Optional 'forcePolicy' argument allows strict policy testing.
func GenMonotonicPair(r *rand.Rand, forcePolicy *bool) (DecisionInput, DecisionInput) {
	inputA := GenValidDecisionInput(r)

	// Override policy if requested
	if forcePolicy != nil {
		inputA.Policy.AllowTranscode = *forcePolicy
	}

	inputB := inputA // Copy
	inputB.RequestID = "gen-monotonic-B"

	// Improve B's capabilities (Superset)
	// 1. Add more containers
	inputB.Capabilities.Containers = append(inputB.Capabilities.Containers, "new-container")
	// 2. Add more codecs
	inputB.Capabilities.VideoCodecs = append(inputB.Capabilities.VideoCodecs, "new-vcodec")
	inputB.Capabilities.AudioCodecs = append(inputB.Capabilities.AudioCodecs, "new-acodec")
	// 3. Improve HLS
	if !inputA.Capabilities.SupportsHLS {
		inputB.Capabilities.SupportsHLS = true
	}

	return inputA, inputB
}

// R4-A Generators for Input Reality Testing

// GenMissingFieldInputs produces inputs with empty/whitespace source fields.
// Expected: 400 (validation failure)
func GenMissingFieldInputs() []DecisionInput {
	base := func() DecisionInput {
		return DecisionInput{
			Source: Source{
				Container:   "mp4",
				VideoCodec:  "h264",
				AudioCodec:  "aac",
				BitrateKbps: 3000,
				Width:       1920,
				Height:      1080,
				FPS:         30.0,
			},
			Capabilities: Capabilities{
				Version:       1,
				Containers:    []string{"mp4"},
				VideoCodecs:   []string{"h264"},
				AudioCodecs:   []string{"aac"},
				SupportsHLS:   true,
				SupportsRange: boolPtr(true),
				DeviceType:    "test",
			},
			Policy: Policy{
				AllowTranscode: true,
			},
			APIVersion: "v3",
			RequestID:  "missing-field-test",
		}
	}

	inputs := []DecisionInput{}

	// M1: Empty container
	m1 := base()
	m1.Source.Container = ""
	inputs = append(inputs, m1)

	// M2: Empty video codec
	m2 := base()
	m2.Source.VideoCodec = ""
	inputs = append(inputs, m2)

	// M3: Empty audio codec
	m3 := base()
	m3.Source.AudioCodec = ""
	inputs = append(inputs, m3)

	// M4: Whitespace-only container
	m4 := base()
	m4.Source.Container = "   "
	inputs = append(inputs, m4)

	return inputs
}

// GenUnrecognizedValueInputs produces inputs with unrecognized but valid codec strings.
// Expected: Transcode (if AllowTranscode=true), NOT Deny
func GenUnrecognizedValueInputs() []DecisionInput {
	base := func() DecisionInput {
		return DecisionInput{
			Source: Source{
				Container:   "mp4",
				VideoCodec:  "h264",
				AudioCodec:  "aac",
				BitrateKbps: 3000,
				Width:       1920,
				Height:      1080,
				FPS:         30.0,
			},
			Capabilities: Capabilities{
				Version:       1,
				Containers:    []string{"mp4"},
				VideoCodecs:   []string{"h264", "hevc"},
				AudioCodecs:   []string{"aac"},
				SupportsHLS:   true,
				SupportsRange: boolPtr(true),
				DeviceType:    "test",
			},
			Policy: Policy{
				AllowTranscode: true,
			},
			APIVersion: "v3",
			RequestID:  "unrecognized-test",
		}
	}

	inputs := []DecisionInput{}

	// U1: Future codec "av1"
	u1 := base()
	u1.Source.VideoCodec = "av1"
	inputs = append(inputs, u1)

	// U2: RFC6381-style "avc1.4d401f"
	u2 := base()
	u2.Source.VideoCodec = "avc1.4d401f"
	inputs = append(inputs, u2)

	// U3: Composite string "h264 (avc1)"
	u3 := base()
	u3.Source.VideoCodec = "h264 (avc1)"
	inputs = append(inputs, u3)

	// U4: Future codec pair
	u4 := base()
	u4.Source.VideoCodec = "vvc"
	u4.Source.AudioCodec = "opus"
	inputs = append(inputs, u4)

	return inputs
}
func sample(r *rand.Rand, list []string) string {
	return list[r.Intn(len(list))]
}

func subset(r *rand.Rand, list []string) []string {
	k := r.Intn(len(list))
	if k == 0 {
		return []string{} // empty subset
	}
	out := make([]string, k)
	for i := 0; i < k; i++ {
		out[i] = list[i]
	}
	return out
}

func boolPtr(b bool) *bool {
	return &b
}

// ModeRank defines the experience order (Higher is better)
func ModeRank(m Mode) int {
	switch m {
	case ModeDirectPlay:
		return 3
	case ModeDirectStream:
		return 2
	case ModeTranscode:
		return 1
	case ModeDeny:
		return 0
	default:
		return -1
	}
}

func GetProofSeed(t *testing.T) int64 {
	// In real CI, read from os.Getenv("XG2G_PROOF_SEED")
	// or default to stable seed if env not set for reproducible local runs
	if s := os.Getenv("XG2G_PROOF_SEED"); s != "" {
		val, _ := strconv.ParseInt(s, 10, 64)
		return val
	}
	return 123456789
}
