package playbackprofile

import "testing"

func TestNormalizeClientFamilyID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "safari", want: ClientSafariNative},
		{in: "safari_native", want: ClientSafariNative},
		{in: "ios_safari", want: ClientIOSSafariNative},
		{in: "ios_safari_native", want: ClientIOSSafariNative},
		{in: "firefox", want: ClientFirefoxHLSJS},
		{in: "chromium", want: ClientChromiumHLSJS},
		{in: "chrome", want: ClientChromiumHLSJS},
		{in: "edge", want: ClientChromiumHLSJS},
		{in: "unknown_client", want: "unknown_client"},
	}

	for _, tt := range tests {
		if got := NormalizeClientFamilyID(tt.in); got != tt.want {
			t.Fatalf("NormalizeClientFamilyID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
