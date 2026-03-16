package playbackprofile

import "testing"

func TestNormalizeHostPressureBand(t *testing.T) {
	if got := NormalizeHostPressureBand(" Elevated "); got != HostPressureElevated {
		t.Fatalf("expected elevated band, got %q", got)
	}
	if got := NormalizeHostPressureBand("bogus"); got != HostPressureUnknown {
		t.Fatalf("expected unknown band for bogus token, got %q", got)
	}
}
