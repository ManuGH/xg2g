package v3

import (
	"fmt"
	"net/url"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

func TestValidateRecordingRef_Hardening(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		wantError bool
	}{
		{
			name:      "Valid Ref",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts",
			wantError: false,
		},
		{
			name:      "Ref with space (allowed, will be encoded in URL)",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/Foo Bar.ts",
			wantError: false,
		},
		{
			name:      "Ref with percent (allowed, standard encoding expected)",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/100% Fun.ts",
			wantError: false,
		},
		{
			name:      "Ref with non-ASCII (Mädchen.ts)",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/Mädchen.ts",
			wantError: false,
		},
		{
			name:      "Injection: Newline",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts\nGET /other",
			wantError: true,
		},
		{
			name:      "Injection: Tab",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts\t",
			wantError: true,
		},
		{
			name:      "Injection: VT (Vertical Tab - Unicode Control)",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts\v",
			wantError: true,
		},
		{
			name:      "Injection: Query Param",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts?hack=1",
			wantError: true,
		},
		{
			name:      "Path Traversal (Explicit ..)",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/../../etc/passwd",
			wantError: true,
		},
		{
			name:      "Invalid UTF-8",
			ref:       "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/\xff\xfe\xfd",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This calls the internal validation function
			err := ValidateRecordingRef(tt.ref)
			if tt.wantError {
				assert.ErrorIs(t, err, errRecordingInvalid, "Expected errRecordingInvalid for input: %q", tt.ref)
			} else {
				assert.NoError(t, err, "Expected valid for input: %q", tt.ref)
			}
		})
	}

	// Extra checks for specific unicode categories
	t.Run("Unicode Control Char U+009F", func(t *testing.T) {
		ref := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test" + string(rune(0x009F))
		assert.True(t, unicode.IsControl(rune(0x009F)))
		err := ValidateRecordingRef(ref)
		assert.ErrorIs(t, err, errRecordingInvalid)
	})

	t.Run("Unicode Format Char U+200B (Zero Width Space)", func(t *testing.T) {
		// U+200B is category Cf
		ref := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test" + string(rune(0x200B))
		assert.True(t, unicode.Is(unicode.Cf, rune(0x200B)))
		err := ValidateRecordingRef(ref)
		assert.ErrorIs(t, err, errRecordingInvalid)
	})
}

func TestResolveRecordingPlaybackSource_URLConstruction(t *testing.T) {
	// Manual test of the URL construction logic to verify valid RawPath generation

	host := "192.168.1.10"
	port := 8001
	user := "root"
	pass := "secret"

	tests := []struct {
		name            string
		ref             string
		wantEscapedPath string
	}{
		{
			name: "Standard Ref with Spaces",
			ref:  "1:0:1:1234:0:0:0:0:0:0:/media/hdd/movie/20250101 2015 - Test.ts",
			// Expectation: Colons literal, Spaces %20
			wantEscapedPath: "/1:0:1:1234:0:0:0:0:0:0:/media/hdd/movie/20250101%202015%20-%20Test.ts",
		},
		{
			name: "Ref with Percent Sign",
			ref:  "1:0:1:1234:0:0:0:0:0:0:/media/hdd/movie/100% Fun.ts",
			// Expectation: Colons literal, % -> %25, Space -> %20
			wantEscapedPath: "/1:0:1:1234:0:0:0:0:0:0:/media/hdd/movie/100%25%20Fun.ts",
		},
		{
			name: "Ref with Non-ASCII (UTF-8)",
			ref:  "1:0:1:1234:0:0:0:0:0:0:/media/hdd/movie/Mädchen.ts",
			// Expectation: 'ä' (0xE4 in ISO, but here UTF-8: 0xC3 0xA4) -> %C3%A4
			// Go source is UTF-8. 'ä' is 2 bytes.
			wantEscapedPath: "/1:0:1:1234:0:0:0:0:0:0:/media/hdd/movie/M%C3%A4dchen.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This logic duplicates the new escapeServiceRefPath helper to verify it
			rawPath := EscapeServiceRefPath("/" + tt.ref)

			u := url.URL{
				Scheme:  "http",
				Host:    fmt.Sprintf("%s:%d", host, port),
				Path:    "/" + tt.ref, // Decoded path
				RawPath: rawPath,      // Encoded path via our helper
			}
			if user != "" && pass != "" {
				u.User = url.UserPassword(user, pass)
			}

			generated := u.String()
			t.Logf("Generated: %s", generated)

			// Strict Structural Parsing Verification (User Request Phase 4)
			// We parse the generated string back to ensure it is structurally valid
			// and that the fields align with expectations.
			parsed, err := url.Parse(generated)
			// Use require so we stop if basic parsing fails
			if err != nil {
				t.Fatalf("Generated URL %q failed to parse: %v", generated, err)
			}

			assert.Equal(t, "http", parsed.Scheme)
			assert.Equal(t, fmt.Sprintf("%s:%d", host, port), parsed.Host)

			// Verify User Info
			parsedUser := parsed.User.Username()
			parsedPass, _ := parsed.User.Password()
			assert.Equal(t, user, parsedUser)
			assert.Equal(t, pass, parsedPass)

			// Verify Path
			// EscapedPath() should match our constructed RawPath EXACTLY
			assert.Equal(t, tt.wantEscapedPath, parsed.EscapedPath(), "Escaped path mismatch")

			// Critical check: Ensure Go accepted our RawPath (if it rejected it, EscapedPath != RawPath)
			assert.Equal(t, rawPath, parsed.EscapedPath(), "Go rejected our RawPath construction")

			// Initial Sanity: NO colon double escaping in the final string
			assert.NotContains(t, generated, "%3A", "Colons should not be escaped")
		})
	}
}

// Private helper copy for testing (since tests share package v3, we can just call it if we export it,
// or easier: just copy it here for a self-contained test if it was private.
// But wait, the function is in the same package 'api', so we can call escapingServiceRefPath directly if not private?)
// I made it private `escapeServiceRefPath`. Since I am in `package v3` (same package), I can access it.
// Wait, test file is `package v3` or `package api_test`?
// I declared `package v3` in the previous write. So I can call `escapeServiceRefPath`.
