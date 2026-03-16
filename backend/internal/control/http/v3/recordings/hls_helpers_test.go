package recordings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRewritePlaylist_AppendsVariantToMediaURIs(t *testing.T) {
	input := `#EXTM3U
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.0,
seg_00000.ts
seg_00001.ts?foo=bar`

	output := RewritePlaylist(input, "VOD", "abc123")

	require.Contains(t, output, "#EXT-X-PLAYLIST-TYPE:VOD")
	assert.Contains(t, output, "seg_00000.ts?variant=abc123")
	assert.Contains(t, output, "seg_00001.ts?foo=bar&variant=abc123")
	assert.NotContains(t, output, "init.mp4?variant=abc123")
	assert.Contains(t, output, "#EXT-X-ENDLIST")
}

func TestRewritePlaylist_EmptyPlaylistTypeDoesNotInjectBlankHeader(t *testing.T) {
	input := `#EXTM3U
#EXTINF:6.0,
seg_00000.ts`

	output := RewritePlaylist(input, "", "")

	assert.NotContains(t, output, "#EXT-X-PLAYLIST-TYPE:")
}
