package decision

import (
	"bytes"
	"testing"
)

// These tests pin the operational-compatibility contract for the
// VideoCodecSignals extension: adding the field MUST NOT change the cache key
// (InputHash, ADR-009.2) for the signal-less requests that every existing
// client sends today, and MUST produce a distinct key only when real signals
// are present. "Schema-additiv" is not enough — this proves it is also
// operationally additive.

// A signal-less request must serialize without the "vcs" key, so its canonical
// bytes — and therefore its InputHash — are byte-identical to before the field
// existed. This is what keeps every cached decision valid across the rollout.
func TestVideoCodecSignals_AbsentKeepsCacheKeyStable(t *testing.T) {
	in := baseDecisionInput() // carries no VideoCodecSignals

	canonical, err := in.CanonicalJSONForHash()
	if err != nil {
		t.Fatalf("CanonicalJSONForHash: %v", err)
	}
	if bytes.Contains(canonical, []byte(`"vcs"`)) {
		t.Errorf("signal-less request must omit the vcs key (omitempty), got:\n%s", canonical)
	}

	// An explicitly empty slice must be treated identically to nil (ADR-009.2 §5).
	withEmpty := baseDecisionInput()
	withEmpty.Capabilities.VideoCodecSignals = []VideoCodecSignal{}
	if withEmpty.ComputeHash() != in.ComputeHash() {
		t.Errorf("empty-slice signals must hash identically to nil signals")
	}
}

// A request that actually carries signals is a distinct semantic input and must
// hash differently — otherwise the matrix decision would be served a stale,
// signal-blind cache entry.
func TestVideoCodecSignals_PresentChangesCacheKey(t *testing.T) {
	plain := baseDecisionInput()

	withSignals := baseDecisionInput()
	withSignals.Capabilities.VideoCodecSignals = []VideoCodecSignal{
		{Codec: "h264", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
	}

	canonical, err := withSignals.CanonicalJSONForHash()
	if err != nil {
		t.Fatalf("CanonicalJSONForHash: %v", err)
	}
	if !bytes.Contains(canonical, []byte(`"vcs"`)) {
		t.Errorf("request with signals must emit the vcs key, got:\n%s", canonical)
	}
	if withSignals.ComputeHash() == plain.ComputeHash() {
		t.Errorf("signals must change the InputHash; got identical hashes")
	}
}

// Signals are a set keyed by normalized codec (ADR-009.2 §6): order and
// codec-name casing/whitespace must not affect the InputHash.
func TestVideoCodecSignals_OrderAndNormalizationInsensitive(t *testing.T) {
	a := baseDecisionInput()
	a.Capabilities.VideoCodecSignals = []VideoCodecSignal{
		{Codec: "av1", Supported: true},
		{Codec: " HEVC ", Supported: true, Smooth: boolPtr(true)},
	}

	b := baseDecisionInput()
	b.Capabilities.VideoCodecSignals = []VideoCodecSignal{
		{Codec: "hevc", Supported: true, Smooth: boolPtr(true)},
		{Codec: "av1", Supported: true},
	}

	if a.ComputeHash() != b.ComputeHash() {
		t.Errorf("reordered / differently-cased signals must hash identically (set semantics)")
	}
}
