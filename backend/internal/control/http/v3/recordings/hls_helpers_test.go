package recordings

import (
	"strings"
	"testing"
)

func TestRewritePlaylistType_DiscontinuityPreservation(t *testing.T) {
	input := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:6.0,
seg_00000.ts
#EXT-X-DISCONTINUITY
#EXTINF:4.0,
seg_00001.ts`

	output := RewritePlaylistType(input, "VOD")

	if !strings.Contains(output, "#EXT-X-DISCONTINUITY") {
		t.Errorf("Expected #EXT-X-DISCONTINUITY to be preserved, but it was removed")
	}

	if !strings.Contains(output, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Errorf("Expected #EXT-X-PLAYLIST-TYPE:VOD to be added")
	}

	if !strings.Contains(output, "#EXT-X-ENDLIST") {
		t.Errorf("Expected #EXT-X-ENDLIST to be added for VOD")
	}
}

func TestRewritePlaylistType_MultipleDiscontinuities(t *testing.T) {
	input := `#EXTM3U
#EXTINF:6.0,
seg_00000.ts
#EXT-X-DISCONTINUITY
#EXTINF:6.0,
seg_00001.ts
#EXT-X-DISCONTINUITY
#EXTINF:2.0,
seg_00002.ts`

	output := RewritePlaylistType(input, "VOD")

	count := strings.Count(output, "#EXT-X-DISCONTINUITY")
	if count != 2 {
		t.Errorf("Expected 2 #EXT-X-DISCONTINUITY tags, found %d", count)
	}
}
