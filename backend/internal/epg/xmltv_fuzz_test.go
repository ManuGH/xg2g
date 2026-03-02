// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build go1.18

package epg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzBuildNameToIDMap(f *testing.F) {
	// Seed corpus with valid XMLTV examples
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="test1">
    <display-name>Test Channel</display-name>
  </channel>
</tv>`))

	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="hd1">
    <display-name>HD Channel HD</display-name>
  </channel>
  <channel id="uhd1">
    <display-name>4K Channel UHD</display-name>
  </channel>
</tv>`))

	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="special">
    <display-name>Österreich AT</display-name>
  </channel>
</tv>`))

	// Malformed/edge cases
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8"?><tv></tv>`))
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8"?><tv><channel></channel></tv>`))
	f.Add([]byte(``))
	f.Add([]byte(`<invalid xml`))
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="">
    <display-name></display-name>
  </channel>
</tv>`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Create temp file with fuzz data
		tmpDir := t.TempDir()
		xmltvPath := filepath.Join(tmpDir, "test.xml")

		if err := os.WriteFile(xmltvPath, data, 0600); err != nil {
			t.Skip("failed to write test file")
		}

		// This should never panic, regardless of input
		nameMap, err := BuildNameToIDMap(xmltvPath)

		// We don't care if it errors (invalid XML is expected)
		// but we do care that it doesn't panic or cause issues
		if err == nil {
			// If successful, the map should be valid
			if nameMap == nil {
				t.Fatal("BuildNameToIDMap returned nil map without error")
			}

			// All keys should be normalized (lowercase, no trailing spaces)
			for key := range nameMap {
				normalized := norm(key)
				if key != normalized {
					t.Errorf("key %q is not normalized (norm returns %q, XML preview: %.100s...)", key, normalized, string(data))
				}
			}
		}
	})
}

func FuzzNameKey(f *testing.F) {
	// Seed corpus with various inputs
	f.Add("Test Channel")
	f.Add("HD Channel HD")
	f.Add("4K UHD")
	f.Add("Österreich AT")
	f.Add("  Multiple   Spaces  ")
	f.Add("")
	f.Add("   ")
	f.Add("\n\t\r")

	f.Fuzz(func(t *testing.T, input string) {
		// NameKey should never panic
		result := NameKey(input)

		// NameKey is just a wrapper around norm, so they should be equal
		expected := norm(input)
		if result != expected {
			t.Errorf("NameKey(%q) = %q, want %q", input, result, expected)
		}

		// Result should be trimmed
		if result != strings.TrimSpace(result) {
			t.Errorf("NameKey(%q) = %q has leading/trailing whitespace", input, result)
		}

		// Result should be lowercase
		if result != strings.ToLower(result) {
			t.Errorf("NameKey(%q) = %q is not lowercase", input, result)
		}

		// Result should have single spaces only
		if strings.Contains(result, "  ") {
			t.Errorf("NameKey(%q) = %q contains multiple consecutive spaces", input, result)
		}
	})
}

func FuzzNorm(f *testing.F) {
	// Seed corpus
	f.Add("Normal Channel")
	f.Add("Channel HD")
	f.Add("Channel UHD")
	f.Add("Channel 4K")
	f.Add("Channel Austria")
	f.Add("Channel Österreich")
	f.Add("Channel Oesterreich")
	f.Add("Channel AT")
	f.Add("Channel DE")
	f.Add("Channel CH")
	f.Add("   Leading/Trailing   ")
	f.Add("Multiple   Spaces")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		// norm should never panic
		result := norm(input)

		// Idempotence: norm(norm(x)) == norm(x)
		if norm(result) != result {
			t.Errorf("norm is not idempotent: norm(%q) = %q, but norm(norm(%q)) = %q",
				input, result, input, norm(result))
		}

		// Result should be trimmed
		if result != strings.TrimSpace(result) {
			t.Errorf("norm(%q) = %q has leading/trailing whitespace", input, result)
		}

		// Result should be lowercase
		if result != strings.ToLower(result) {
			t.Errorf("norm(%q) = %q is not lowercase", input, result)
		}
	})
}
