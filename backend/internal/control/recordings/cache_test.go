package recordings

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRecordingVariantHash_MatchesCanonicalTargetProfile(t *testing.T) {
	target := RecordingTargetProfile("")
	require.NotNil(t, target)

	assert.Equal(t, target.Hash(), DefaultRecordingVariantHash())
}

func TestRecordingVariantIdentity_NormalizesThroughSingleSSOT(t *testing.T) {
	serviceRef := " 1:0:0:0:0:0:0:0:0:0:/movie/monk.ts "
	variant := "  AbC123  "

	assert.Equal(t, "1:0:0:0:0:0:0:0:0:0:/movie/monk.ts", RecordingVariantMetadataKey(serviceRef, ""))
	assert.Equal(t, "1:0:0:0:0:0:0:0:0:0:/movie/monk.ts#variant:abc123", RecordingVariantMetadataKey(serviceRef, variant))
	assert.Equal(t, RecordingVariantCacheKey(serviceRef, "abc123"), RecordingVariantCacheKey(serviceRef, variant))
}

func TestRecordingTargetProfile_FMP4BiasFollowsSharedProfilePolicy(t *testing.T) {
	target := RecordingTargetProfile("safari_hevc_hw")
	require.NotNil(t, target)

	assert.Equal(t, playbackprofile.PackagingFMP4, target.Packaging)
	assert.Equal(t, "mp4", target.Container)
	assert.Equal(t, "fmp4", target.HLS.SegmentContainer)

	androidTV := RecordingTargetProfile("android_tv_native")
	require.NotNil(t, androidTV)
	assert.Equal(t, playbackprofile.PackagingFMP4, androidTV.Packaging)
	assert.Equal(t, "mp4", androidTV.Container)
	assert.Equal(t, "fmp4", androidTV.HLS.SegmentContainer)
}
