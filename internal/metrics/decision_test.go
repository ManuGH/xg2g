package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecordDecisionSummary_IncrementsCounter(t *testing.T) {
	initial := getCounterVecValue(t, decisionTotal, "transcode_hw", "hevc", "true", "profile_preference", "safari")

	RecordDecisionSummary("safari", "transcode_hw", "hevc", true, "profile_preference")

	actual := getCounterVecValue(t, decisionTotal, "transcode_hw", "hevc", "true", "profile_preference", "safari")
	assert.Equal(t, initial+1, actual)
}

func TestRecordDecisionSummary_NormalizesLabels(t *testing.T) {
	initial := getCounterVecValue(t, decisionTotal, "unknown", "other", "false", "unknown", "custom")

	RecordDecisionSummary("CUSTOM_PROFILE_123", "unmapped-path", "vp9", false, "unmapped-reason")

	actual := getCounterVecValue(t, decisionTotal, "unknown", "other", "false", "unknown", "custom")
	assert.Equal(t, initial+1, actual)
}
