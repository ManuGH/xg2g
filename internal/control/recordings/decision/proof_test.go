package decision

import (
	"context"
	"math/rand"
	"reflect"
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

		status, decision, problem := Decide(ctx, input)

		// Assertions:
		// A) Schema violations must be strictly 4xx (Client Error)
		if status < 400 || status >= 500 {
			t.Errorf("Prop_FailClosed_Schema Violation [Iter %d]: Expected 4xx status, got %d", i, status)
			dumpFailure(t, seed, input)
		}

		// Optional: Specific status match (user recommended)
		if status != expectedStatus {
			t.Errorf("Prop_FailClosed_Schema Violation: Expected status %d, got %d", expectedStatus, status)
		}

		// 2. Decision must be nil
		if decision != nil {
			t.Errorf("Prop_FailClosed_Schema Violation: Decision must be nil on schema error")
		}

		// 3. Problem must be present
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

		status, decision, _ := Decide(ctx, input)

		// Assertions:
		// 1. Status 200 (Logic handled successfully)
		if status != 200 {
			t.Errorf("Prop_FailClosed_Logic Violation [Iter %d]: Expected 200 OK for logic denial, got %d", i, status)
			dumpFailure(t, seed, input)
			continue
		}

		// 2. Decision check (Red Issue B: Guard nil)
		if decision == nil {
			t.Errorf("Prop_FailClosed_Logic Violation: Decision is nil! Contract regression.")
			dumpFailure(t, seed, input)
			continue
		}

		// 3. Mode Deny
		if decision.Mode != ModeDeny {
			t.Errorf("Prop_FailClosed_Logic Violation: Expected ModeDeny, matches %s", decision.Mode)
			dumpFailure(t, seed, input)
		}

		// 4. Reason Match
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
		_, baseDec, _ := Decide(ctx, input)
		if baseDec == nil {
			t.Fatalf("Prop_Determinism: Baseline decision is nil for valid input: %+v", input)
		}
		baseHash := input.ComputeHash()

		for j := 0; j < Repeats; j++ {
			_, dec, _ := Decide(ctx, input)
			if dec == nil {
				t.Fatalf("Prop_Determinism: Repeated decision is nil")
			}

			// 1. InputHash Stability
			currHash := input.ComputeHash()
			if currHash != baseHash {
				t.Fatalf("Prop_Determinism: InputHash unstable! %s vs %s", baseHash, currHash)
			}

			// 2. Mode Stability
			if dec.Mode != baseDec.Mode {
				t.Errorf("Prop_Determinism: Mode drift! %s vs %s", baseDec.Mode, dec.Mode)
			}

			// 3. Reasons Stability
			if !reflect.DeepEqual(dec.Reasons, baseDec.Reasons) {
				t.Errorf("Prop_Determinism: Reasons drift! %v vs %v", baseDec.Reasons, dec.Reasons)
			}

			// 4. Trace Stability (Deep Check - Red Issue C)
			if !reflect.DeepEqual(dec.Trace, baseDec.Trace) {
				t.Errorf("Prop_Determinism: Trace deep mismatch!\nBase: %+v\nCurr: %+v", baseDec.Trace, dec.Trace)
			}
		}
	}
}

// 4. Monotonicity (Allow Transcode): Improvement should never degrade mode.
func TestProp_Monotonicity_AllowTranscode(t *testing.T) {
	testMonotonicity(t, true)
}

// 5. Monotonicity (Deny Transcode): Improvement should never degrade (exception aware).
func TestProp_Monotonicity_DenyTranscodePolicy(t *testing.T) {
	testMonotonicity(t, false)
}

func testMonotonicity(t *testing.T, allowTranscode bool) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	const N = 100
	for i := 0; i < N; i++ {
		// Force policy to be constant across the pair
		inA, inB := GenMonotonicPair(r, &allowTranscode)

		_, decA, _ := Decide(ctx, inA)
		_, decB, _ := Decide(ctx, inB)

		if decA == nil || decB == nil {
			continue
		}

		rankA := ModeRank(decA.Mode)
		rankB := ModeRank(decB.Mode)

		// Rule: B >= A
		if rankB < rankA {
			t.Errorf("Prop_Monotonicity Violation [Iter %d]: Capability improvement degraded mode!\nPolicy.AllowTranscode: %v\nA: %s (%d)\nB: %s (%d)",
				i, allowTranscode, decA.Mode, rankA, decB.Mode, rankB)
			dumpFailure(t, seed, inA)
		}
	}
}

// Helpers
func dumpFailure(t *testing.T, seed int64, input DecisionInput) {
	// Canonical JSON via engine method (Red Issue 1)
	b, _ := input.CanonicalJSON()
	hash := input.ComputeHash()

	t.Logf("\n=== FAILURE ARTIFACT ===\nSEED: %d\nINPUT HASH: %s\nINPUT JSON (Canonical):\n%s\n========================\n",
		seed, hash, string(b))
}
