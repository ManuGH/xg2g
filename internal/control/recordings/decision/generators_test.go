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

// Helpers
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
