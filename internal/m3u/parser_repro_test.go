package m3u

import (
	"testing"
)

func TestParseRepro(t *testing.T) {
	content := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="1:0:1:300:7:85:C00000:0:0:0:" tvg-logo="/logos/1_0_1_300_7_85_C00000_0_0_0.png?v=1767922888" group-title="Last Scanned" tvg-name=".",.
http://10.10.55.64/web/stream.m3u?ref=1%3A0%3A1%3A300%3A7%3A85%3AC00000%3A0%3A0%3A0%3A&name=.
`
	channels := Parse(content)
	if len(channels) != 1 {
		t.Fatalf("Expected 1 channel, got %d", len(channels))
	}
	ch := channels[0]
	if ch.TvgID != "1:0:1:300:7:85:C00000:0:0:0:" {
		t.Errorf("Expected TvgID '1:0:1:300:7:85:C00000:0:0:0:', got '%s'", ch.TvgID)
	}
	if ch.Group != "Last Scanned" {
		t.Errorf("Expected Group 'Last Scanned', got '%s'", ch.Group)
	}
}
