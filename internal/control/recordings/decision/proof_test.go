package decision

import (
	"context"
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// P2: Property Proof System (CTO-Grade)
// Verifies core invariants with zero-flake guarantees.

// 1. Fail-Closed (Schema): Invalid schema must return error status (4xx), never success.
func TestProp_FailClosed_InvalidSchema(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 50
	for i := 0; i < N; i++ {
		input, expectedStatus := GenInvalidSchemaInput(r)

		status, decision, problem := Decide(ctx, input, "test")

		// Assertions:
		// A) Schema violations must be strictly 4xx (Client Error)
		if status < 400 || status >= 500 {
			t.Errorf("Prop_FailClosed_Schema Violation [Iter %d]: Expected 4xx status, got %d", i, status)
			dumpFailure(t, seed, input)
		}

		if status != expectedStatus {
			t.Errorf("Prop_FailClosed_Schema Violation: Expected status %d, got %d", expectedStatus, status)
		}

		if decision != nil {
			t.Errorf("Prop_FailClosed_Schema Violation: Decision must be nil on schema error")
		}

		if problem == nil {
			t.Errorf("Prop_FailClosed_Schema Violation: Problem detail missing")
		}
	}
}

// 2. Fail-Closed (Logic): Valid schema + Incompatible media -> 200 OK + ModeDeny.
func TestProp_FailClosed_IncompatibleButValid(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 50
	for i := 0; i < N; i++ {
		input, expectedReason := GenInvalidLogicInput(r)

		status, decision, _ := Decide(ctx, input, "test")

		if status != 200 {
			t.Errorf("Prop_FailClosed_Logic Violation [Iter %d]: Expected 200 OK for logic denial, got %d", i, status)
			dumpFailure(t, seed, input)
			continue
		}

		if decision == nil {
			t.Errorf("Prop_FailClosed_Logic Violation: Decision is nil! Contract regression.")
			dumpFailure(t, seed, input)
			continue
		}

		if decision.Mode != ModeDeny {
			t.Errorf("Prop_FailClosed_Logic Violation: Expected ModeDeny, matches %s", decision.Mode)
			dumpFailure(t, seed, input)
		}

		found := false
		for _, rc := range decision.Reasons {
			if rc == expectedReason {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Prop_FailClosed_Logic Violation: Expected reason %s not found in %v", expectedReason, decision.Reasons)
		}
	}
}

// 3. Determinism: Bit-identical output for fixed input (Hash, Mode, Reasons, Trace).
func TestProp_Determinism(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const Samples = 20
	const Repeats = 50

	for i := 0; i < Samples; i++ {
		input := GenValidDecisionInput(r)

		// Baseline run
		_, baseDec, _ := Decide(ctx, input, "test")
		if baseDec == nil {
			t.Fatalf("Prop_Determinism: Baseline decision is nil for valid input: %+v", input)
		}
		baseHash := input.ComputeHash()

		for j := 0; j < Repeats; j++ {
			_, dec, _ := Decide(ctx, input, "test")
			if dec == nil {
				t.Fatalf("Prop_Determinism: Repeated decision is nil")
			}

			currHash := input.ComputeHash()
			if currHash != baseHash {
				t.Fatalf("Prop_Determinism: InputHash unstable! %s vs %s", baseHash, currHash)
			}

			if dec.Mode != baseDec.Mode {
				t.Errorf("Prop_Determinism: Mode drift! %s vs %s", baseDec.Mode, dec.Mode)
			}

			if !reflect.DeepEqual(dec.Reasons, baseDec.Reasons) {
				t.Errorf("Prop_Determinism: Reasons drift! %v vs %v", baseDec.Reasons, dec.Reasons)
			}

			if !reflect.DeepEqual(dec.Trace, baseDec.Trace) {
				t.Errorf("Prop_Determinism: Trace deep mismatch!\nBase: %+v\nCurr: %+v", baseDec.Trace, dec.Trace)
			}
		}
	}
}

func TestProp_Monotonicity_AllowTranscode(t *testing.T) {
	testMonotonicity(t, true)
}

func TestProp_Monotonicity_DenyTranscodePolicy(t *testing.T) {
	testMonotonicity(t, false)
}

func testMonotonicity(t *testing.T, allowTranscode bool) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 100
	for i := 0; i < N; i++ {
		inA, inB := GenMonotonicPair(r, &allowTranscode)

		_, decA, _ := Decide(ctx, inA, "test")
		_, decB, _ := Decide(ctx, inB, "test")

		if decA == nil || decB == nil {
			continue
		}

		rankA := ModeRank(decA.Mode)
		rankB := ModeRank(decB.Mode)

		if rankB < rankA {
			t.Errorf("Prop_Monotonicity Violation [Iter %d]: Degradation! A: %s (%d), B: %s (%d)",
				i, decA.Mode, rankA, decB.Mode, rankB)
			dumpFailure(t, seed, inA)
		}
	}
}

func TestProp_ContainerMismatch_BlocksOnlyDirectPlay(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 100
	for i := 0; i < N; i++ {
		input := GenContainerMismatchDPOnly(r)
		_, decision, _ := Decide(ctx, input, "test")

		if decision != nil && decision.Mode == ModeDirectPlay {
			t.Errorf("Prop_ContainerMismatchViolation [Iter %d]: Mode is DirectPlay!", i)
		}
	}
}

func TestProp_ContainerMismatch_DoesNotForceTranscode(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 100
	for i := 0; i < N; i++ {
		input := GenContainerMismatchDSPossible(r)
		_, decision, _ := Decide(ctx, input, "test")

		if decision != nil && decision.Mode != ModeDirectStream {
			t.Errorf("Prop_ContainerMismatch_NoTranscode Violation [Iter %d]: Expected DS, got %s", i, decision.Mode)
		}
	}
}

func TestProp_Normalization_DirectPlay(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 50
	for i := 0; i < N; i++ {
		input := GenContainerCaseVariants(r)
		status, decision, _ := Decide(ctx, input, "test")

		if status == 200 && decision != nil {
			expectedDP := isMP4Family(input.Source.Container) &&
				input.Capabilities.SupportsRange != nil && *input.Capabilities.SupportsRange

			if expectedDP && decision.Mode != ModeDirectPlay {
				t.Errorf("Prop_Normalization_DirectPlay Violation [Iter %d]: Expected DP for '%s', got %s",
					i, input.Source.Container, decision.Mode)
			}
		}
	}
}

func isMP4Family(container string) bool {
	norm := strings.ToLower(strings.TrimSpace(container))
	return norm == "mp4" || norm == "mov" || norm == "m4v"
}

func TestProp_Canonicalization_NoDuplicateDrift(t *testing.T) {
	ctx := context.Background()
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))

	const N = 20
	for i := 0; i < N; i++ {
		base, duped := GenDuplicateCapsVariants(r)

		_, decBase, _ := Decide(ctx, base, "test")
		_, decDuped, _ := Decide(ctx, duped, "test")

		if decBase != nil && decDuped != nil {
			if decBase.Mode != decDuped.Mode {
				t.Errorf("Prop_Canonicalization_NoDuplicateDrift [Iter %d]: Mode drift!", i)
			}
			if base.ComputeHash() != duped.ComputeHash() {
				t.Errorf("Prop_Canonicalization_NoDuplicateDrift [Iter %d]: Hash drift!", i)
			}
		}
	}
}

func TestProp_SemanticEquivalence_NilEmptySlices(t *testing.T) {
	ctx := context.Background()
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))

	const N = 10
	for i := 0; i < N; i++ {
		withEmpty, withNil := GenNilVsEmptySliceVariants(r)

		_, decEmpty, _ := Decide(ctx, withEmpty, "test")
		_, decNil, _ := Decide(ctx, withNil, "test")

		if decEmpty != nil && decNil != nil {
			if decEmpty.Mode != decNil.Mode {
				t.Errorf("Prop_SemanticEquivalence_NilEmptySlices [Iter %d]: Mode drift!", i)
			}
			if withEmpty.ComputeHash() != withNil.ComputeHash() {
				t.Errorf("Prop_SemanticEquivalence_NilEmptySlices [Iter %d]: Hash drift!", i)
			}
		}
	}
}

func TestProp_NormalizedEquivalence_Strict(t *testing.T) {
	ctx := context.Background()
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))

	const N = 100
	for i := 0; i < N; i++ {
		raw := GenValidDecisionInput(r)
		norm := NormalizeInput(raw)

		_, decRaw, _ := Decide(ctx, raw, "test")
		_, decNorm, _ := Decide(ctx, norm, "test")

		if decRaw != nil && decNorm != nil {
			if decRaw.Mode != decNorm.Mode {
				t.Errorf("Prop_NormalizedEquivalence_Strict [Iter %d]: Mode mismatch!", i)
			}
			if raw.ComputeHash() != norm.ComputeHash() {
				t.Errorf("Prop_NormalizedEquivalence_Strict [Iter %d]: Hash mismatch!", i)
			}
		}
	}
}

func TestProp_PermutationInvariance(t *testing.T) {
	ctx := context.Background()
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))

	const N = 50
	for i := 0; i < N; i++ {
		base := GenValidDecisionInput(r)
		permuted := base
		permuted.Capabilities.Containers = shuffleSlice(r, base.Capabilities.Containers)
		permuted.Capabilities.VideoCodecs = shuffleSlice(r, base.Capabilities.VideoCodecs)
		permuted.Capabilities.AudioCodecs = shuffleSlice(r, base.Capabilities.AudioCodecs)

		_, decBase, _ := Decide(ctx, base, "test")
		_, decPerm, _ := Decide(ctx, permuted, "test")

		if decBase != nil && decPerm != nil {
			if decBase.Mode != decPerm.Mode || base.ComputeHash() != permuted.ComputeHash() {
				t.Errorf("Prop_PermutationInvariance [Iter %d]: Invariance broken!", i)
			}
		}
	}
}

func TestProp_NormalizeIdempotence(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))

	const N = 100
	for i := 0; i < N; i++ {
		raw := GenValidDecisionInput(r)
		norm1 := NormalizeInput(raw)
		norm2 := NormalizeInput(norm1)
		if !reflect.DeepEqual(norm1, norm2) {
			t.Errorf("Prop_NormalizeIdempotence [Iter %d]: Not idempotent!", i)
		}
	}
}

func shuffleSlice(r *rand.Rand, in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, len(in))
	copy(out, in)
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func TestProp_MissingFields_Always400(t *testing.T) {
	ctx := context.Background()
	for i, input := range GenMissingFieldInputs() {
		status, decision, problem := Decide(ctx, input, "test")
		if status < 400 || status >= 500 || decision != nil || problem == nil {
			t.Errorf("Prop_MissingFields_Always400 [Case %d]: Expected 4xx, got %d", i, status)
		}
	}
}

func TestProp_EmptyObject_Always400(t *testing.T) {
	_, prob := DecodeDecisionInput([]byte(`{}`))
	if prob == nil || prob.Status != 400 {
		t.Errorf("Expected 400 for empty object, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "object cannot be empty") {
		t.Errorf("Wrong error detail: %s", prob.Detail)
	}
}

func TestProp_SmallAPIVersionMatch(t *testing.T) {
	// Root v3 matching
	jsonStr := `{"source":{"c":"mp4","v":"h264","a":"aac"}, "api":"v3"}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 412 {
		t.Errorf("Expected 412 for 'v3' request without caps, got %v", prob)
	}
}

func TestProp_EmptyBody_Always400(t *testing.T) {
	_, prob := DecodeDecisionInput([]byte(``))
	if prob == nil || prob.Status != 400 {
		t.Errorf("Expected 400 for empty body, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "unexpected end of JSON input") {
		t.Errorf("Wrong error detail: %s", prob.Detail)
	}
}

func TestProp_SchemaLess_Always400(t *testing.T) {
	_, prob := DecodeDecisionInput([]byte(`{"foo": "bar"}`))
	if prob == nil || prob.Status != 400 {
		t.Errorf("Expected 400 for schema-less object, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "Unknown root key") {
		t.Errorf("Wrong error detail: %s", prob.Detail)
	}
}

func TestProp_UnknownRootKey_Always400(t *testing.T) {
	jsonStr := `{"source":{"c":"mp4","v":"h264","a":"aac"},"caps":{"v":1},"api":"v3","foo":1}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for unknown root key, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "Unknown root key") {
		t.Errorf("Wrong error detail: %s", prob.Detail)
	}
}

func TestProp_SourceMustBeObject_Compact(t *testing.T) {
	jsonStr := `{"source":"lol","caps":{"v":1},"api":"v3"}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for non-object source, got %v", prob)
	}
}

func TestProp_CapsMustBeObject_Compact(t *testing.T) {
	jsonStr := `{"source":{"c":"mp4","v":"h264","a":"aac"},"caps":123,"api":"v3"}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for non-object caps, got %v", prob)
	}
}

func TestProp_PolicyMustBeObject_Legacy(t *testing.T) {
	jsonStr := `{
		"Source":{"container":"mp4","videoCodec":"h264","audioCodec":"aac"},
		"Capabilities":{"version":1},
		"Policy":true,
		"APIVersion":"v3"
	}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for non-object Policy, got %v", prob)
	}
}

func TestProp_SourceNull_Always400(t *testing.T) {
	jsonStr := `{"source":null,"caps":{"v":1},"api":"v3"}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for null source, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "must be an object") {
		t.Errorf("wrong error: %s", prob.Detail)
	}
}

func TestProp_SourceArray_Always400(t *testing.T) {
	jsonStr := `{"source":[],"caps":{"v":1},"api":"v3"}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for array source, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "must be an object") {
		t.Errorf("wrong error: %s", prob.Detail)
	}
}

func TestProp_CapsNull_Always400(t *testing.T) {
	jsonStr := `{"source":{"c":"mp4"}, "caps":null, "api":"v3"}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for null caps, got %v", prob)
	}
}

func TestProp_APIMissing_Always400(t *testing.T) {
	jsonStr := `{"source":{"c":"mp4"}, "caps":{"v":1}}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for missing api, got %v", prob)
	}
	if !strings.Contains(prob.Detail, "api") || !strings.Contains(prob.Detail, "required") {
		t.Errorf("wrong error: %s", prob.Detail)
	}
}

func TestProp_UnrecognizedValues_NeverDenyIfTranscodeAllowed(t *testing.T) {
	ctx := context.Background()
	for i, input := range GenUnrecognizedValueInputs() {
		_, decision, _ := Decide(ctx, input, "test")
		if decision != nil && decision.Mode == ModeDeny {
			t.Errorf("Prop_UnrecognizedValues [Case %d]: Got Deny for unrecognized codec!", i)
		}
	}
}

func TestProp_DualDecode_Compatibility(t *testing.T) {
	ctx := context.Background()
	legacyJSON := `{
		"Source": {"container": "mp4", "videoCodec":"h264", "audioCodec":"aac"},
		"Capabilities": {"version": 1, "containers":["mp4"], "videoCodecs":["h264"], "audioCodecs":["aac"]},
		"Policy": {"allowTranscode": true},
		"APIVersion": "v3"
	}`
	compactJSON := `{
		"source": {"c": "mp4", "v":"h264", "a":"aac"},
		"caps": {"v": 1, "c":["mp4"], "vc":["h264"], "ac":["aac"]},
		"policy": {"tx": true},
		"api": "v3"
	}`

	inputLegacy, probL := DecodeDecisionInput([]byte(legacyJSON))
	inputCompact, probC := DecodeDecisionInput([]byte(compactJSON))

	if probL != nil || probC != nil {
		t.Fatalf("Decode failed: L=%v, C=%v", probL, probC)
	}

	_, decL, _ := Decide(ctx, inputLegacy, "legacy")
	_, decC, _ := Decide(ctx, inputCompact, "compact")

	if decL == nil || decC == nil {
		t.Fatalf("Decide returned nil: L=%v, C=%v", decL, decC)
	}

	if decL.Mode != decC.Mode || inputLegacy.ComputeHash() != inputCompact.ComputeHash() {
		t.Errorf("Dual-Decode Mismatch! L_hash=%s, C_hash=%s", inputLegacy.ComputeHash(), inputCompact.ComputeHash())
	}
}

func TestProp_UnicodeWhitespace_Equivalence(t *testing.T) {
	ctx := context.Background()
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))

	const N = 20
	for i := 0; i < N; i++ {
		base := GenValidDecisionInput(r)
		variants := []string{
			base.Source.Container,
			base.Source.Container + "\u00A0",
			"\u200B" + base.Source.Container,
			base.Source.Container + "\uFEFF",
		}

		firstHash := ""
		for j, v := range variants {
			input := base
			input.Source.Container = v
			status, _, _ := Decide(ctx, input, "test")
			if status != 200 {
				t.Fatalf("Variant failed: %s", v)
			}

			hash := input.ComputeHash()
			if j == 0 {
				firstHash = hash
				continue
			}
			if hash != firstHash {
				t.Errorf("Unicode Equivalence Broken! Iter %d, Variant %d", i, j)
			}
		}
	}
}

func TestProp_NoMixedSchemaAmbiguity_Exhaustive(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"source vs Source", `{"source":{}, "Source":{}, "api":"v3"}`},
		{"caps vs Capabilities", `{"caps":{}, "Capabilities":{}, "api":"v3"}`},
		{"policy vs Policy", `{"policy":{}, "Policy":{}, "api":"v3"}`},
		{"api vs APIVersion", `{"api":"v3", "APIVersion":"v3", "source":{}}`},
		{"rid vs RequestID", `{"rid":"a", "RequestID":"a", "api":"v3"}`},
		{"Nested source mix", `{"source":{"c":"mp4","container":"mp4"}, "api":"v3"}`},
		{"Nested caps mix", `{"caps":{"v":1,"version":1}, "api":"v3"}`},
		{"Symmetric inner (comp in legacy src)", `{"Source":{"c":"mp4", "container":"mp4"}, "api":"v3"}`},
		{"Symmetric inner (comp in legacy cap)", `{"Capabilities":{"v":1, "version":1}, "api":"v3"}`},
		{"Symmetric inner (comp in legacy pol)", `{"Policy":{"tx":true, "allowTranscode":true}, "api":"v3"}`},
		{"Cross-Mix Source+Caps", `{"Source":{}, "caps":{}, "api":"v3"}`},
		{"Cross-Mix source+Capabilities", `{"source":{}, "Capabilities":{}, "api":"v3"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, prob := DecodeDecisionInput([]byte(tc.json))
			if prob == nil {
				t.Fatalf("Expected problem for mixed schema: %s, got success", tc.name)
			}
			if prob.Status != 400 {
				t.Errorf("Expected 400 mixed_schema for: %s, got %d", tc.name, prob.Status)
			}
			if !strings.Contains(strings.ToLower(prob.Detail), "mixed schema") {
				t.Errorf("Detail missing mixed schema mention: %s", prob.Detail)
			}
		})
	}
}

func TestProp_FPSAllowed_Compact(t *testing.T) {
	jsonStr := `{
		"source":{"c":"mp4","v":"h264","a":"aac","fps":30},
		"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"]},
		"api":"v3"
	}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob != nil {
		t.Fatalf("fps should be allowed in compact schema, got %v", prob)
	}
}

func TestProp_FPSAllowed_Legacy(t *testing.T) {
	jsonStr := `{
		"Source":{"container":"mp4","videoCodec":"h264","audioCodec":"aac","fps":30},
		"Capabilities":{"version":1,"containers":["mp4"],"videoCodecs":["h264"],"audioCodecs":["aac"]},
		"APIVersion":"v3"
	}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob != nil {
		t.Fatalf("fps should be allowed in legacy schema, got %v", prob)
	}
}

func TestProp_FPSDoesNotBypassMixedDetection(t *testing.T) {
	jsonStr := `{
		"source":{"c":"mp4","container":"mp4","fps":30},
		"api":"v3"
	}`
	_, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for mixed keys, got %v", prob)
	}
}

func TestKeyRules_Guardrail_NoIdenticalPairs(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for identical pair keys, got none")
		}
	}()
	rules := KeyRules{
		Pairs:  map[string]string{"fps": "fps"},
		Shared: map[string]struct{}{"fps": {}},
	}
	mustNoSharedInPairs(rules)
}

func TestProp_PresenceAwareMerge_ZeroValues(t *testing.T) {
	// Case A: Pure Legacy Request (Verification of recursive TitleCase decoding)
	fallbackJSON := `{
		"Source": {"container": "mp4", "videoCodec": "h264", "audioCodec": "aac"},
		"Capabilities": {"version": 1},
		"Policy": {"allowTranscode": true},
		"APIVersion": "v3"
	}`
	input, prob := DecodeDecisionInput([]byte(fallbackJSON))
	if prob != nil {
		t.Fatalf("Decode failed for pure legacy: %v", prob)
	}
	if !input.Policy.AllowTranscode {
		t.Error("Failed to decode legacy Policy.allowTranscode")
	}

	// Case B: Compact Policy present (false), Legacy keys absent.
	compactFalseJSON := `{
		"source": {"c":"mp4", "v":"h264", "a":"aac"},
		"caps": {"v":1},
		"policy": {"tx": false},
		"api": "v3"
	}`
	input2, _ := DecodeDecisionInput([]byte(compactFalseJSON))
	if input2.Policy.AllowTranscode {
		t.Error("Compact false was incorrectly overwritten or failed to unmarshal")
	}
}

func TestProp_OptionalPolicy_DefaultStability(t *testing.T) {
	// If policy is omitted, it should default to false (safe/fail-closed).
	jsonStr := `{
		"source": {"c":"mp4", "v":"h264", "a":"aac"},
		"caps": {"v":1},
		"api": "v3"
	}`
	input, prob := DecodeDecisionInput([]byte(jsonStr))
	if prob != nil {
		t.Fatalf("Optional policy should be allowed, got %v", prob)
	}
	if input.Policy.AllowTranscode {
		t.Error("Policy.AllowTranscode should default to false if omitted")
	}
}

func TestProp_Explainability_NoEmptyReasonsOnDeny(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		input, _ := GenInvalidLogicInput(r)
		_, dec, _ := Decide(ctx, input, "test")
		if dec != nil && dec.Mode == ModeDeny && len(dec.Reasons) == 0 {
			t.Errorf("Deny with empty Reasons!")
		}
	}
}

func TestProp_Explainability_NoEmptyReasonsOnTranscode(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		input := GenValidDecisionInput(r)
		input.Capabilities.VideoCodecs = []string{"other"}
		input.Policy.AllowTranscode = true
		_, dec, _ := Decide(ctx, input, "test")
		if dec != nil && dec.Mode == ModeTranscode && len(dec.Reasons) == 0 {
			t.Errorf("Transcode with empty Reasons!")
		}
	}
}

func TestProp_ReplayArtifact_ReproducesDecision(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		orig := GenValidDecisionInput(r)
		_, dec1, _ := Decide(ctx, orig, "test")
		b, _ := orig.CanonicalJSON()
		var re DecisionInput
		json.Unmarshal(b, &re)
		_, dec2, _ := Decide(ctx, re, "replay")
		if dec1.Mode != dec2.Mode {
			t.Errorf("Replay mismatch!")
		}
	}
}

func dumpFailure(t *testing.T, seed int64, input DecisionInput) {
	b, _ := json.MarshalIndent(input, "", "  ")
	t.Errorf("\n=== FAILURE ARTIFACT ===\nSEED: %d\nINPUT:\n%s\n========================\n", seed, string(b))
}
