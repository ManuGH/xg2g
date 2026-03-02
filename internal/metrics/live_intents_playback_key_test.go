package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIncLiveIntentsPlaybackKey_IncrementsCounter(t *testing.T) {
	initial := getCounterVecValue(t, LiveIntentsPlaybackKeyTotal, "both", "mismatch")

	IncLiveIntentsPlaybackKey("both", "mismatch")

	actual := getCounterVecValue(t, LiveIntentsPlaybackKeyTotal, "both", "mismatch")
	assert.Equal(t, initial+1, actual)
}

func TestIncLiveIntentsPlaybackKey_NormalizesUnknownLabels(t *testing.T) {
	initial := getCounterVecValue(t, LiveIntentsPlaybackKeyTotal, "unknown", "unknown")

	IncLiveIntentsPlaybackKey("custom_key", "custom_result")

	actual := getCounterVecValue(t, LiveIntentsPlaybackKeyTotal, "unknown", "unknown")
	assert.Equal(t, initial+1, actual)
}
