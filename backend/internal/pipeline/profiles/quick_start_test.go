package profiles

import "testing"

func TestUsesQuickStartHLSSegments(t *testing.T) {
	tests := []struct {
		profile string
		want    bool
	}{
		{profile: ProfileSafari, want: true},
		{profile: ProfileSafariRuntimeHQ, want: true},
		{profile: ProfileSafariDirty, want: true},
		{profile: "bandwidth", want: true},
		{profile: ProfileHigh, want: false},
		{profile: ProfileCopy, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			if got := UsesQuickStartHLSSegments(tt.profile); got != tt.want {
				t.Fatalf("UsesQuickStartHLSSegments(%q) = %v, want %v", tt.profile, got, tt.want)
			}
		})
	}
}
