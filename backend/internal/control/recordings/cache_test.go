package recordings

import (
	"testing"

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
