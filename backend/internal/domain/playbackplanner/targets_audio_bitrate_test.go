package playbackplanner

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/assert"
)

// The planner is the runtime SSOT for the executed audio bitrate:
// applyPlannerPlanToProfile overwrites ProfileSpec.AudioBitrateK from the plan, so
// a transcode plan carrying a stale literal silently defeats the profile axes.
func TestResolveMediaTargets_TranscodeAudioBitrateIsShared(t *testing.T) {
	plan := &PlaybackPlan{Mode: "transcode"}
	resolveMediaTargets(plan, PlaybackEvidence{})

	assert.Equal(t, "transcode", plan.Audio.Mode)
	assert.Equal(t, "aac", plan.Audio.Codec)
	assert.Equal(t, playbackprofile.LiveTranscodeAudioBitrateKbps, plan.Audio.BitrateKbps)
	assert.Equal(t, 2, plan.Audio.Channels)
	assert.Equal(t, 48000, plan.Audio.SampleRate)
}
