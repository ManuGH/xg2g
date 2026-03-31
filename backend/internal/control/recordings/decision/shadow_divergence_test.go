package decision

import (
	"context"
	"testing"
)

func TestShadowCollectorRoundTrip(t *testing.T) {
	collector := NewShadowCollector()
	ctx := WithShadowCollector(context.Background(), collector)

	collector.Add(ShadowDivergence{
		Predicate: "Video",
		NewReasons: []string{
			"codec_mismatch",
			"codec_mismatch",
			"",
		},
	})

	got := ShadowDivergencesFromContext(ctx)
	if len(got) != 1 {
		t.Fatalf("expected one divergence, got %d", len(got))
	}
	if got[0].Predicate != "video" {
		t.Fatalf("expected normalized predicate, got %q", got[0].Predicate)
	}
	if len(got[0].NewReasons) != 1 || got[0].NewReasons[0] != "codec_mismatch" {
		t.Fatalf("expected normalized reasons, got %+v", got[0].NewReasons)
	}
}
