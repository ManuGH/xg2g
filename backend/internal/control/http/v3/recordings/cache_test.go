package recordings

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestRecordingVariantCacheKey_DiffersPerVariant(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/movie/monk.ts"

	base := RecordingVariantCacheKey(serviceRef, "")
	a := RecordingVariantCacheKey(serviceRef, "abc123")
	b := RecordingVariantCacheKey(serviceRef, "xyz789")

	assert.NotEmpty(t, base)
	assert.NotEqual(t, base, a)
	assert.NotEqual(t, a, b)
}

func TestTargetProfileQuery_RoundTripAndPlaylistURL(t *testing.T) {
	target := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
		Container: "mpegts",
		Packaging: playbackprofile.PackagingTS,
		Video: playbackprofile.VideoTarget{
			Mode:  playbackprofile.MediaModeCopy,
			Codec: "h264",
		},
		Audio: playbackprofile.AudioTarget{
			Mode:        playbackprofile.MediaModeTranscode,
			Codec:       "aac",
			Channels:    2,
			BitrateKbps: 256,
			SampleRate:  48000,
		},
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: "mpegts",
			SegmentSeconds:   6,
		},
		HWAccel: playbackprofile.HWAccelNone,
	})

	encoded, err := EncodeTargetProfileQuery(&target)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeTargetProfileQuery(encoded)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, target.Hash(), decoded.Hash())

	rawURL := RecordingPlaylistURL("rec123", "generic", &target)
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	assert.Equal(t, "/api/v3/recordings/rec123/playlist.m3u8", parsed.Path)
	assert.Equal(t, "generic", parsed.Query().Get("profile"))
	assert.Equal(t, target.Hash(), parsed.Query().Get("variant"))
	assert.Equal(t, encoded, parsed.Query().Get("target"))
}
