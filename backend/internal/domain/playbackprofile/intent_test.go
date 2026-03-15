// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "testing"

func TestNormalizeRequestedIntent(t *testing.T) {
	t.Run("maps aliases", func(t *testing.T) {
		cases := map[string]PlaybackIntent{
			"direct":      IntentDirect,
			"copy":        IntentDirect,
			"passthrough": IntentDirect,
			"compatible":  IntentCompatible,
			"high":        IntentCompatible,
			"quality":     IntentQuality,
			"repair":      IntentRepair,
			"unknown":     IntentUnknown,
			"":            IntentUnknown,
		}

		for raw, want := range cases {
			if got := NormalizeRequestedIntent(raw); got != want {
				t.Fatalf("NormalizeRequestedIntent(%q) = %q, want %q", raw, got, want)
			}
		}
	})
}

func TestPublicIntentName(t *testing.T) {
	cases := map[PlaybackIntent]string{
		IntentDirect:     "direct",
		IntentCompatible: "compatible",
		IntentQuality:    "quality",
		IntentRepair:     "repair",
		IntentUnknown:    "",
	}

	for intent, want := range cases {
		if got := PublicIntentName(intent); got != want {
			t.Fatalf("PublicIntentName(%q) = %q, want %q", intent, got, want)
		}
	}
}

func TestIsKnownIntent(t *testing.T) {
	if !IsKnownIntent("quality") {
		t.Fatal("expected quality to be known")
	}
	if IsKnownIntent("mystery") {
		t.Fatal("expected mystery to be unknown")
	}
}
