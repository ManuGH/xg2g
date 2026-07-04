// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playlist

import (
	"bytes"
	"strings"
	"testing"
)

// TestWriteM3UNeutralizesInjection proves that CR/LF and double quotes in
// upstream-controlled channel/bouquet names cannot split the #EXTINF record
// (injecting a fake URL/entry) or break out of a quoted attribute.
func TestWriteM3UNeutralizesInjection(t *testing.T) {
	var buf bytes.Buffer
	items := []Item{{
		Name:  "Evil\" tvg-logo=\"x\nhttp://attacker.example/inject.ts\n#EXTINF:-1,Fake",
		Group: "Grp\"\nstuff",
		TvgID: "id\nx",
		URL:   "http://box/stream.ts",
	}}

	if err := WriteM3U(&buf, items, "", ""); err != nil {
		t.Fatalf("WriteM3U: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Header + exactly one #EXTINF + exactly one URL line. Any injected newline
	// would produce extra lines here.
	if len(lines) != 3 {
		t.Fatalf("newline injection split the record; want 3 lines, got %d:\n%s", len(lines), out)
	}
	if lines[0] != "#EXTM3U" {
		t.Errorf("line 0 = %q, want #EXTM3U", lines[0])
	}
	if !strings.HasPrefix(lines[1], "#EXTINF:-1 ") {
		t.Errorf("line 1 is not an EXTINF record: %q", lines[1])
	}
	if lines[2] != "http://box/stream.ts" {
		t.Errorf("URL line = %q; injection must not replace the real stream URL", lines[2])
	}
	// The raw quote from the name must be neutralised (no attribute breakout).
	if strings.Contains(out, `Evil"`) {
		t.Error("raw double quote from the channel name survived (attribute breakout)")
	}
	if !strings.Contains(out, "Evil'") {
		t.Error("expected the channel-name double quote to be replaced with a single quote")
	}
}

func TestSanitizeM3UField(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		"a\nb":         "a b",
		"a\r\nb":       "a  b",
		"a\tb":         "a b",
		`say "hi"`:     "say 'hi'",
		"ctrl\x00char": "ctrlchar",
		"  trim me  ":  "trim me",
	}
	for in, want := range cases {
		if got := sanitizeM3UField(in); got != want {
			t.Errorf("sanitizeM3UField(%q) = %q, want %q", in, got, want)
		}
	}
}
