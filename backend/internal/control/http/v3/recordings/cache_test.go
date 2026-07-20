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

	key1 := "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
	encoded, err := EncodeTargetProfileQuery(&target, key1)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeTargetProfileQuery(encoded, key1, "", true)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, target.Hash(), decoded.Hash())

	rawURL := RecordingPlaylistURL("rec123", "generic", &target, key1)
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	assert.Equal(t, "/api/v3/recordings/rec123/playlist.m3u8", parsed.Path)
	assert.Equal(t, "generic", parsed.Query().Get("profile"))
	assert.NotEmpty(t, parsed.Query().Get("variant"))
	assert.Equal(t, encoded, parsed.Query().Get("target"))
}

func TestTargetProfileHMAC_Tamper(t *testing.T) {
	target := playbackprofile.TargetPlaybackProfile{Container: "mpegts"}
	key1 := "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
	encoded, _ := EncodeTargetProfileQuery(&target, key1)

	// Tamper payload
	tamperedPayload := "a" + encoded[1:]
	_, err := DecodeTargetProfileQuery(tamperedPayload, key1, "", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target profile signature")

	// Tamper signature
	tamperedSig := encoded[:len(encoded)-1] + "a"
	_, err = DecodeTargetProfileQuery(tamperedSig, key1, "", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target profile signature")
}

func TestTargetProfileHMAC_MissingSignature(t *testing.T) {
	target := playbackprofile.TargetPlaybackProfile{Container: "mpegts"}
	encodedUnsigned, _ := EncodeTargetProfileQuery(&target, "")
	key1 := "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"

	// Strict = true -> fails
	_, err := DecodeTargetProfileQuery(encodedUnsigned, key1, "", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing target profile signature (strict mode)")

	// Strict = false -> succeeds
	decoded, err := DecodeTargetProfileQuery(encodedUnsigned, key1, "", false)
	require.NoError(t, err)
	assert.NotNil(t, decoded)
}

func TestTargetProfileHMAC_Rotation(t *testing.T) {
	target := playbackprofile.TargetPlaybackProfile{Container: "mpegts"}
	oldKey := "oldkey_abcdefghijklmnopqrstuvwxyz012345678"
	newKey := "newkey_abcdefghijklmnopqrstuvwxyz012345678"
	encodedOld, _ := EncodeTargetProfileQuery(&target, oldKey)

	// URL signed with previous key verifies
	decoded, err := DecodeTargetProfileQuery(encodedOld, newKey, oldKey, true)
	require.NoError(t, err)
	assert.NotNil(t, decoded)

	// URL signed with unknown key fails
	_, err = DecodeTargetProfileQuery(encodedOld, newKey, "wrong_previous_key", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target profile signature")
}

func TestTargetProfileHMAC_CanonicalizationStability(t *testing.T) {
	target := playbackprofile.TargetPlaybackProfile{
		Container: "mpegts",
		Video: playbackprofile.VideoTarget{Codec: "h264"},
	}
	key1 := "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
	encoded1, _ := EncodeTargetProfileQuery(&target, key1)
	
	decoded, _ := DecodeTargetProfileQuery(encoded1, key1, "", true)
	encoded2, _ := EncodeTargetProfileQuery(decoded, key1)
	
	assert.Equal(t, encoded1, encoded2)
}
