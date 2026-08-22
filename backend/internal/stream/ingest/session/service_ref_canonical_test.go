// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import "testing"

// The canonical form of a service reference decides what counts as the same
// upstream. If two spellings of one channel ever produce two keys, they produce two
// sessions, two dials and two tuners for one programme - which is precisely what
// session sharing exists to prevent. These tests pin the rule so it cannot drift
// apart again between a caller, a cache and a test fixture.

const (
	wireForm      = "1:0:19:132F:3EF:1:C00000:0:0:0:"
	canonicalForm = "1:0:19:132F:3EF:1:C00000:0:0:0"
)

func TestServiceRef_WireAndCanonicalFormsAreOneKey(t *testing.T) {
	withColon := NewSessionKey("10.10.55.64", 8001, wireForm).Canonicalize()
	without := NewSessionKey("10.10.55.64", 8001, canonicalForm).Canonicalize()

	if withColon.ServiceRef != canonicalForm {
		t.Fatalf("canonical form must drop the trailing colon: got %q", withColon.ServiceRef)
	}
	if withColon != without {
		t.Fatalf("both spellings must canonicalise to one key:\n  %#v\n  %#v", withColon, without)
	}

	// Comparable as a map key, which is how sessions are actually looked up.
	seen := map[SessionKey]int{}
	seen[withColon]++
	seen[without]++
	if len(seen) != 1 {
		t.Fatalf("one channel must occupy one map entry, got %d", len(seen))
	}
}

// Repeated trailing colons and surrounding whitespace are spellings of the same
// thing too - a hand-edited bouquet or a client that concatenates carelessly.
func TestServiceRef_CanonicalisationIsIdempotentAndForgiving(t *testing.T) {
	for _, spelling := range []string{
		wireForm,
		canonicalForm,
		"  " + wireForm + "  ",
		canonicalForm + "::",
	} {
		got := NewSessionKey("10.10.55.64", 8001, spelling).Canonicalize()
		if got.ServiceRef != canonicalForm {
			t.Errorf("%q canonicalised to %q, want %q", spelling, got.ServiceRef, canonicalForm)
		}
		// Canonicalising again must not change it further.
		if again := got.Canonicalize(); again != got {
			t.Errorf("canonicalisation is not idempotent for %q", spelling)
		}
	}
}

// The rest of the key is canonicalised alongside it, and for the same reason: a
// difference in any field is a different upstream.
func TestServiceRef_KeyIdentityCoversHostPortProfileAndSource(t *testing.T) {
	base := SessionKey{
		ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: wireForm,
	}.Canonicalize()

	if base.Profile != "native" || base.SourceType != "enigma2" {
		t.Fatalf("defaults must be filled in, got profile=%q source=%q", base.Profile, base.SourceType)
	}

	mixedCase := SessionKey{
		ReceiverHost: "10.10.55.64", StreamPort: 8001, ServiceRef: wireForm,
		Profile: "NATIVE", SourceType: "Enigma2",
	}.Canonicalize()
	if mixedCase != base {
		t.Fatalf("profile and source type must be case-insensitive:\n  %#v\n  %#v", mixedCase, base)
	}

	// A different programme on the same service is a different upstream.
	other := base
	other.TargetProgram = 2
	if other == base {
		t.Fatal("target programme must take part in identity")
	}
}
