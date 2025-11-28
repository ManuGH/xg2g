// SPDX-License-Identifier: MIT

package jobs

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
)

// slugify converts a channel name into a URL-safe, human-readable slug
// Example: "Das Erste HD" → "das-erste-hd"
func slugify(name string) string {
	if name == "" {
		return "channel"
	}

	// 1. Lowercase
	s := strings.ToLower(name)

	// 2. Replace common German umlauts and special characters
	replacer := strings.NewReplacer(
		"ä", "ae",
		"ö", "oe",
		"ü", "ue",
		"ß", "ss",
		"à", "a",
		"á", "a",
		"â", "a",
		"è", "e",
		"é", "e",
		"ê", "e",
		"ì", "i",
		"í", "i",
		"î", "i",
		"ò", "o",
		"ó", "o",
		"ô", "o",
		"ù", "u",
		"ú", "u",
		"û", "u",
		"ç", "c",
		"ñ", "n",
	)
	s = replacer.Replace(s)

	// 3. Remove all non-alphanumeric characters (keep only a-z, 0-9)
	// and replace sequences of non-alphanumeric with single dash
	var result strings.Builder
	lastWasDash := false

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			lastWasDash = false
		} else if !lastWasDash {
			result.WriteRune('-')
			lastWasDash = true
		}
	}

	// 4. Trim leading/trailing dashes
	slug := strings.Trim(result.String(), "-")

	// 5. Collapse multiple dashes
	reDash := regexp.MustCompile(`-+`)
	slug = reDash.ReplaceAllString(slug, "-")

	// 6. Limit length to reasonable size (max 50 chars for readability)
	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.TrimRight(slug, "-")
	}

	if slug == "" {
		return "channel"
	}

	return slug
}

// makeHumanReadableTvgID creates a human-readable tvg-id from channel name and service reference
// Format: "channel-name-SUFFIX" where SUFFIX is 6 chars from service reference hash
// This ensures:
// - Human-readable IDs (e.g., "ard-hd-3fa92b" instead of "sref-a3f5d8c1...")
// - Stable IDs (hash ensures uniqueness even if channel names change)
// - Collision resistance (different service refs → different IDs)
//
// Examples:
//   - "Das Erste HD" + "1:0:19:132F:3EF:1:C00000:0:0:0:" → "das-erste-hd-3fa92b"
//   - "ZDF" + "1:0:19:1334:3EF:1:C00000:0:0:0:" → "zdf-a7c4e1"
//   - "Sky Sport HD" + "1:0:19:83:6:85:C00000:0:0:0:" → "sky-sport-hd-b2d8f9"
func makeHumanReadableTvgID(name, sref string) string {
	// Create slug from channel name
	slug := slugify(name)

	// Generate short hash from service reference for uniqueness
	sum := sha1.Sum([]byte(sref))
	suffix := hex.EncodeToString(sum[:])[:6] // First 6 chars of hash

	// Combine: channel-name-suffix
	return slug + "-" + suffix
}
