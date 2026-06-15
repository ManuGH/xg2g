package m3u

import "testing"

// L19: the channel name is everything after the attribute block, so a name containing a
// comma must be preserved (LastIndex(",") truncated it to the text after the last comma).
func TestParse_NameWithCommaPreserved(t *testing.T) {
	content := "#EXTM3U\n#EXTINF:-1 tvg-id=\"x\" group-title=\"News\",Tagesschau, das Erste\nhttp://example/1\n"
	chans := Parse(content)
	if len(chans) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(chans))
	}
	if chans[0].Name != "Tagesschau, das Erste" {
		t.Fatalf("name with comma must be preserved, got %q", chans[0].Name)
	}
}

// Regression guards: no attributes, and a comma inside an attribute value.
func TestParse_NameVariants(t *testing.T) {
	cases := map[string]string{
		"#EXTINF:-1,Plain, Name\nhttp://x/1\n":                           "Plain, Name",
		"#EXTINF:-1 group-title=\"A,B\",Channel One\nhttp://x/2\n":       "Channel One",
		"#EXTINF:-1 tvg-id=\"y\" group-title=\"News\",ARD\nhttp://x/3\n": "ARD",
	}
	for line, want := range cases {
		chans := Parse("#EXTM3U\n" + line)
		if len(chans) != 1 {
			t.Fatalf("%q: expected 1 channel, got %d", line, len(chans))
		}
		if chans[0].Name != want {
			t.Fatalf("%q: name = %q, want %q", line, chans[0].Name, want)
		}
	}
}
