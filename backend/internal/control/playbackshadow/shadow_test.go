package playbackshadow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffComparablePlans_RemainsStrict(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "decision", Outcome: "allow", Mode: "transcode",
		Engine: "hls", Container: "fmp4", VideoMode: "transcode", AudioMode: "transcode",
		VideoCodec: "h264", AudioCodec: "aac",
	}
	planner := legacy
	planner.Container = "mpegts"
	planner.AudioMode = "copy"

	assert.ElementsMatch(t, []string{"packaging_mismatch", "audio_mode_mismatch"}, DiffComparablePlans(legacy, planner))
	assert.ElementsMatch(t, []string{"packaging_mismatch"}, UnexplainedDiffCodes(legacy, planner))
}

func TestClassifyComparableDiffs_GenericHLSWrapperIsAccepted(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "decision", Outcome: "allow", Mode: "remux",
		Engine: "hls", Container: "hls", VideoMode: "copy", AudioMode: "copy",
		VideoCodec: "h264", AudioCodec: "aac",
	}
	planner := legacy
	planner.Container = "fmp4"

	classified := ClassifyComparableDiffs(legacy, planner)
	assert.Equal(t, []ClassifiedDiff{{
		Code: "packaging_mismatch", Disposition: DiffAccepted,
		Reason: "generic_hls_wrapper_vs_known_segment_container",
	}}, classified)
	assert.Empty(t, UnexplainedDiffCodes(legacy, planner))
}

func TestClassifyComparableDiffs_CompatibleVideoCopyDuringAudioTranscodeIsAccepted(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "decision", Outcome: "allow", Mode: "transcode",
		Engine: "hls", Container: "fmp4", VideoMode: "transcode", AudioMode: "transcode",
		VideoCodec: "h264", AudioCodec: "aac",
	}
	planner := legacy
	planner.VideoMode = "copy"

	classified := ClassifyComparableDiffs(legacy, planner)
	require.Equal(t, []ClassifiedDiff{{
		Code:        "video_mode_mismatch",
		Disposition: DiffAccepted,
		Reason:      "compatible_video_copy_avoids_reencode_during_audio_transcode",
	}}, classified)
	require.Empty(t, UnexplainedDiffCodes(legacy, planner))
}

func TestClassifyComparableDiffs_VideoCopyDifferenceWithoutAudioTranscodeRemainsUnexplained(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "decision", Outcome: "allow", Mode: "transcode",
		Engine: "hls", Container: "fmp4", VideoMode: "transcode", AudioMode: "copy",
		VideoCodec: "h264", AudioCodec: "aac",
	}
	planner := legacy
	planner.VideoMode = "copy"

	require.Equal(t, []string{"video_mode_mismatch"}, UnexplainedDiffCodes(legacy, planner))
}

func TestDiffComparablePlans_DenyReasonMismatchBlocksCutover(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "decision", Outcome: "deny", Mode: "none",
		Engine: "none", Container: "none", VideoMode: "none", AudioMode: "none",
		VideoCodec: "none", AudioCodec: "none", ReasonCode: "policy_denies_transcode",
	}
	planner := legacy
	planner.ReasonCode = "no_compatible_mode_available"

	assert.Equal(t, []string{"reason_mismatch"}, UnexplainedDiffCodes(legacy, planner))
}

func TestDiffComparablePlans_ProblemIsNotDecisionDeny(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "problem", Outcome: "problem", Mode: "none",
		ReasonCode: "capabilities_invalid",
	}
	planner := ComparablePlaybackPlan{
		IsValid: true, TerminalKind: "decision", Outcome: "deny", Mode: "none",
		ReasonCode: "no_compatible_mode_available",
	}

	assert.Contains(t, UnexplainedDiffCodes(legacy, planner), "terminal_kind_mismatch")
	assert.Contains(t, UnexplainedDiffCodes(legacy, planner), "outcome_mismatch")
}
