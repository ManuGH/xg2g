package recordings

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
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
	intent := &ports.BuildIntent{Target: target}
	encoded, err := EncodeTargetProfileQuery(intent, key1)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeTargetProfileQuery(encoded, key1, "", true)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, target.Hash(), decoded.Target.Hash())

	rawURL := RecordingPlaylistURL("rec123", "generic", intent, key1)
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
	encoded, _ := EncodeTargetProfileQuery(&ports.BuildIntent{Target: target}, key1)

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
	encodedUnsigned, _ := EncodeTargetProfileQuery(&ports.BuildIntent{Target: target}, "")
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
	encodedOld, _ := EncodeTargetProfileQuery(&ports.BuildIntent{Target: target}, oldKey)

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
		Video:     playbackprofile.VideoTarget{Codec: "h264"},
	}
	key1 := "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
	encoded1, _ := EncodeTargetProfileQuery(&ports.BuildIntent{Target: target}, key1)

	decoded, _ := DecodeTargetProfileQuery(encoded1, key1, "", true)
	encoded2, _ := EncodeTargetProfileQuery(decoded, key1)

	assert.Equal(t, encoded1, encoded2)
}

func TestDecodeTargetProfileQuery_LegacyBareTargetPayload(t *testing.T) {
	target := playbackprofile.TargetPlaybackProfile{
		Container: "mp4",
		Video:     playbackprofile.VideoTarget{Codec: "hevc"},
	}
	key := "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"

	// Simulate Phase 3 legacy payload: bare TargetPlaybackProfile JSON, signed with key
	b, err := json.Marshal(target)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(b)

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	encodedLegacy := fmt.Sprintf("%s.%s", payload, sig)

	// Decode in strict mode
	intent, err := DecodeTargetProfileQuery(encodedLegacy, key, "", true)
	require.NoError(t, err)
	require.NotNil(t, intent)

	assert.Equal(t, "mp4", intent.Target.Container)
	assert.Equal(t, "hevc", intent.Target.Video.Codec)
	assert.Equal(t, ports.SourceProfile{}, intent.SourceTruth)
}
