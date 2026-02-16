package urlutil

import "testing"

func TestSanitizeURL_RemovesUserInfoAndQuery(t *testing.T) {
	in := "http://user:pass@example.com:1234/some/path?ref=abc&x=1"
	got := SanitizeURL(in)
	if got == in {
		t.Fatalf("expected sanitized URL to differ, got same: %q", got)
	}
	if got != "http://example.com:1234/some/path" {
		t.Fatalf("unexpected sanitized URL: %q", got)
	}
}

func TestSanitizeURL_InvalidInputDoesNotLeak(t *testing.T) {
	in := "http://user:pass@exa mple.com"
	got := SanitizeURL(in)
	if got != "invalid-url-redacted" {
		t.Fatalf("unexpected sanitized value: %q", got)
	}
}
