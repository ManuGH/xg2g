// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"regexp"
	"testing"
)

func TestMakeTvgID_Properties(t *testing.T) {
	// Human readable format: name-normalized-hash
	// e.g. das-erste-hd-1a2b3c

	got1 := makeTvgID("Das Erste HD", "1:0:19:2B66:41:1:C00000:0:0:0:")
	got2 := makeTvgID("Das Erste HD", "1:0:19:2B66:41:1:C00000:0:0:0:")  // same input
	got3 := makeTvgID("Das Zweite HD", "1:0:1:445C:453:1:C00000:0:0:0:") // different input

	if got1 == "" {
		t.Fatalf("empty id")
	}
	if got1 != got2 {
		t.Fatalf("not deterministic: %q vs %q", got1, got2)
	}
	if got1 == got3 {
		t.Fatalf("not distinguishing different inputs: %q", got1)
	}

	// Should contain part of the name
	if !regexp.MustCompile(`das-erste-hd`).MatchString(got1) {
		t.Fatalf("id should contain normalized name: %q", got1)
	}
}

func BenchmarkMakeTvgID(b *testing.B) {
	sref := "1:0:19:2B66:41:1:C00000:0:0:0:"
	for i := 0; i < b.N; i++ {
		_ = makeTvgID("Das Erste HD", sref)
	}
}
