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
	initial := getCounterVecValue(t, decisionTotal, "unknown", "unknown", "false", "unknown", "unknown")

	RecordDecisionSummary("CUSTOM_PROFILE_123", "unmapped-path", "vp9", false, "unmapped-reason")

	actual := getCounterVecValue(t, decisionTotal, "unknown", "unknown", "false", "unknown", "unknown")
	assert.Equal(t, initial+1, actual)
}

func TestRecordDecisionSummary_NormalizesEmptyAndCasing(t *testing.T) {
	initialUnknown := getCounterVecValue(t, decisionTotal, "unknown", "unknown", "false", "unknown", "unknown")
	initialCanonical := getCounterVecValue(t, decisionTotal, "transcode_hw", "h264", "true", "profile_preference", "safari")

	RecordDecisionSummary("", "", "", false, "")
	RecordDecisionSummary(" SAFARI ", " TRANSCODE_HW ", " H264 ", true, " PROFILE_PREFERENCE ")

	actualUnknown := getCounterVecValue(t, decisionTotal, "unknown", "unknown", "false", "unknown", "unknown")
	actualCanonical := getCounterVecValue(t, decisionTotal, "transcode_hw", "h264", "true", "profile_preference", "safari")

	assert.Equal(t, initialUnknown+1, actualUnknown)
	assert.Equal(t, initialCanonical+1, actualCanonical)
}

func TestRecordDecisionSummary_EmptyOutputCodecFromSummaryIsUnknown(t *testing.T) {
	initial := getCounterVecValue(t, decisionTotal, "reject", "unknown", "false", "no_compatible_codec", "av1_required")

	RecordDecisionSummary("av1_required", "reject", "none", false, "no_compatible_codec")

	actual := getCounterVecValue(t, decisionTotal, "reject", "unknown", "false", "no_compatible_codec", "av1_required")
	assert.Equal(t, initial+1, actual)
}
