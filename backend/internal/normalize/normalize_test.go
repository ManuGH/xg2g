// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package normalize_test

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/stretchr/testify/assert"
)

func TestValidateLiveRef_ExhaustiveSecurityMatrix(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		wantValid bool
	}{
		// 1. Valid Enigma2 DVB Live Service References
		{"ValidDVBStandard", "1:0:19:283D:3FB:1:C00000:0:0:0:", true},
		{"ValidDVBNoTrailingColon", "1:0:19:283D:3FB:1:C00000:0:0:0", true},
		{"ValidDVBShort", "1:0:1:1:1:1:0:0:0:0:", true},
		{"ValidDVBWithUnderscore", "1:0:19:283D_1:3FB:1:C00000:0:0:0:", true},

		// 2. Path Traversal and Slashes
		{"SlashOnly", "/", false},
		{"PathTraversalDots", "../", false},
		{"PathTraversalInRef", "1:0:19:283D:3FB:1:C00000:0:0:0:/../etc/passwd", false},
		{"SlashInRef", "1:0:19:283D:3FB:1:C00000:0:0:0:/movie.ts", false},
		{"Backslash", "\\", false},
		{"BackslashInRef", "1:0:19:283D:3FB:1:C00000:0:0:0:\\windows\\win.ini", false},

		// 3. Encoded Traversal & Double-Encoding Vectors
		{"PercentEncodedSlashUpper", "%2F", false},
		{"PercentEncodedSlashLower", "%2f", false},
		{"DoubleEncodedSlash", "%252F", false},
		{"PercentEncodedDots", "%2e%2e", false},
		{"DoubleEncodedDots", "%252e%252e", false},
		{"EncodedBackslash", "%5C", false},
		{"EncodedBackslashLower", "%5c", false},

		// 4. Query & Fragment Injections
		{"QuestionMark", "?", false},
		{"QuestionMarkInRef", "1:0:19:283D:3FB:1:C00000:0:0:0:?stream=test", false},
		{"EncodedQuestionMark", "%3F", false},
		{"EncodedQuestionMarkLower", "%3f", false},
		{"HashFragment", "#", false},
		{"HashFragmentInRef", "1:0:19:283D:3FB:1:C00000:0:0:0:#frag", false},
		{"EncodedHash", "%23", false},

		// 5. Control Characters & CRLF
		{"CarriageReturn", "\r", false},
		{"LineFeed", "\n", false},
		{"CRLFInRef", "1:0:19:283D:3FB\r\n:1:C00000:0:0:0:", false},
		{"EncodedCR", "%0d", false},
		{"EncodedLF", "%0a", false},
		{"EncodedCRLF", "%0d%0a", false},
		{"NullByte", "\x00", false},
		{"NullByteInRef", "1:0:19:283D:\x00:1:C00000:0:0:0:", false},
		{"UnicodeControlChar", "1:0:19:283D:\u0007:1:C00000:0:0:0:", false},
		{"UnicodeFormatCharRTL", "1:0:19:283D:\u202E:1:C00000:0:0:0:", false},
		{"UnicodeZeroWidthSpace", "1:0:19:283D:\u200B:1:C00000:0:0:0:", false},

		// 6. Whitespace and Structural Violations
		{"LeadingWhitespace", " 1:0:19:283D:3FB:1:C00000:0:0:0:", false},
		{"TrailingWhitespace", "1:0:19:283D:3FB:1:C00000:0:0:0: ", false},
		{"InternalWhitespace", "1:0:19: 283D:3FB:1:C00000:0:0:0:", false},
		{"CompletelyEmpty", "", false},
		{"WhitespaceOnly", "   ", false},
		{"MissingColons", "1019283D3FB1C00000000", false},
		{"TooFewParts", "1:0:1", false},
		{"NonAsciiCharacters", "1:0:19:283D:3FB:1:C00000:0:0:äöü:", false},

		// 7. Extremes
		{"TooShort", "1:0:1:", false},
		{"ExtremelyLongRef", "1:0:19:" + strings.Repeat("A", 300) + ":0:0:0:", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalize.ValidateLiveRef(tt.ref)
			if tt.wantValid {
				assert.NoError(t, err, "ref %q should be valid", tt.ref)
			} else {
				assert.ErrorIs(t, err, normalize.ErrInvalidLiveRef, "ref %q must be rejected", tt.ref)
			}
		})
	}
}
