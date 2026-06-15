package http

import (
	"errors"
	"testing"
)

// M22: a suffix range (bytes=-N) on a zero-size resource has no satisfiable bytes. It must
// return ErrInvalidRange, not Range{Start:0, End:-1} (End < Start) with a nil error.
func TestParseRange_SuffixOnZeroSizeIsInvalid(t *testing.T) {
	if _, err := ParseRange("bytes=-500", 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("expected ErrInvalidRange for suffix range on zero-size resource, got %v", err)
	}
}

// Regression guard: suffix ranges on non-empty resources must still resolve correctly.
func TestParseRange_SuffixOnNonZeroStillWorks(t *testing.T) {
	r, err := ParseRange("bytes=-500", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Start != 500 || r.End != 999 {
		t.Fatalf("got %+v, want {Start:500, End:999}", r)
	}
}
