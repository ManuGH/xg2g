// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"regexp"
	"testing"
)

func TestMakeStableIDFromSRef_Properties(t *testing.T) {
	got1 := makeStableIDFromSRef("1:0:19:2B66:41:1:C00000:0:0:0:")
	got2 := makeStableIDFromSRef("1:0:19:2B66:41:1:C00000:0:0:0:") // same input
	got3 := makeStableIDFromSRef("1:0:1:445C:453:1:C00000:0:0:0:") // different input

	if got1 == "" {
		t.Fatalf("empty id")
	}
	if got1 != got2 {
		t.Fatalf("not deterministic: %q vs %q", got1, got2)
	}
	if got1 == got3 {
		t.Fatalf("not distinguishing different sRefs: %q", got1)
	}

	re := regexp.MustCompile(`^sref-[a-f0-9]+$`)
	if !re.MatchString(got1) {
		t.Fatalf("id contains invalid chars: %q", got1)
	}
}

func BenchmarkMakeStableIDFromSRef(b *testing.B) {
	sref := "1:0:19:2B66:41:1:C00000:0:0:0:"
	for i := 0; i < b.N; i++ {
		_ = makeStableIDFromSRef(sref)
	}
}
