// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package playlist

import (
	"strings"
	"testing"
)

func TestWriteM3UTable(t *testing.T) {
	tests := []struct {
		name   string
		items  []Item
		expect []string
	}{
		{
			name: "basic with logo and channel number",
			items: []Item{{
				Name: "ORF1 HD", TvgID: "orf1.at", Group: "AT", TvgLogo: "http://p/ORF1.png", URL: "http://sref/1:0:19:132F", TvgChNo: 1,
			}},
			expect: []string{
				"#EXTM3U",
				`tvg-id="orf1.at"`,
				`group-title="AT"`,
				`tvg-logo="http://p/ORF1.png"`,
				`tvg-chno="1"`,
				",ORF1 HD",
				"http://sref/1:0:19:132F",
			},
		},
		{
			name: "missing logo keeps stable tvg-id",
			items: []Item{{
				Name: "ORF2N HD", TvgID: "orf2n.at", Group: "AT", URL: "http://sref/1:0:19:1334", TvgChNo: 2,
			}},
			expect: []string{
				`tvg-id="orf2n.at"`,
				`group-title="AT"`,
				`tvg-logo=""`,
				`tvg-chno="2"`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			if err := WriteM3U(&b, tc.items, "", ""); err != nil {
				t.Fatalf("WriteM3U failed: %v", err)
			}
			out := b.String()
			for _, want := range tc.expect {
				if !strings.Contains(out, want) {
					t.Fatalf("missing substring %q\n--- output ---\n%s", want, out)
				}
			}
			if strings.Count(out, "#EXTINF:") != len(tc.items) {
				t.Fatalf("expected %d EXTINF lines, got %d", len(tc.items), strings.Count(out, "#EXTINF:"))
			}
		})
	}
}
