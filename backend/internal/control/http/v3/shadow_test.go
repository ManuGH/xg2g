package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/assert"
)

func TestComparableFromLegacy(t *testing.T) {
	dec := &decision.Decision{
		Mode: decision.ModeTranscode,
		SelectedOutputKind: "hls",
		Selected: decision.SelectedFormats{
			Container:  "ts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Video: playbackprofile.VideoTarget{
				Mode: "transcode",
				BitrateKbps: 4000,
				Width: 1280,
				Height: 720,
			},
			Audio: playbackprofile.AudioTarget{
				Mode: "copy",
			},
		},
	}
	
	comp := ComparableFromLegacy(dec)
	assert.Equal(t, "allow", comp.Outcome)
	assert.Equal(t, "transcode", comp.Mode)
	assert.Equal(t, "hls", comp.Engine)
	assert.Equal(t, "ts", comp.Container)
	assert.Equal(t, "h264", comp.VideoCodec)
	assert.Equal(t, 4000, comp.TargetBitrate)
	assert.Equal(t, 1280, comp.ScaleWidth)
	assert.Equal(t, "transcode", comp.VideoMode)
	assert.Equal(t, "copy", comp.AudioMode)
}

func TestDiffComparablePlans(t *testing.T) {
	legacy := ComparablePlaybackPlan{
		Outcome: "allow",
		Mode: "remux",
		Engine: "hls",
		Container: "ts",
		VideoMode: "copy",
		TargetBitrate: 5000,
	}
	
	newPlan := ComparablePlaybackPlan{
		Outcome: "allow",
		Mode: "transcode", // mismatch
		Engine: "hls",
		Container: "ts",
		VideoMode: "transcode", // mismatch
		TargetBitrate: 4000, // mismatch
	}
	
	diffs := DiffComparablePlans(legacy, newPlan)
	assert.Contains(t, diffs, "mode_mismatch")
	assert.Contains(t, diffs, "video_mode_mismatch")
	assert.Contains(t, diffs, "target_bitrate_drift")
	assert.NotContains(t, diffs, "outcome_mismatch")
	assert.NotContains(t, diffs, "engine_mismatch")
}

func TestComparableFromPlanner(t *testing.T) {
	plan := playbackplanner.PlaybackPlan{
		Outcome: "allow",
		Mode: "transcode",
		DeliveryEngine: "hls",
		Packaging: playbackplanner.Packaging{
			Container: "ts",
		},
		Codecs: playbackplanner.Codecs{
			Video: "h264",
			Audio: "copy",
		},
		RateControl: playbackplanner.RateControl{
			TargetVideoBitrateKbps: 3000,
		},
	}
	
	comp := ComparableFromPlanner(plan)
	assert.Equal(t, "allow", comp.Outcome)
	assert.Equal(t, "transcode", comp.Mode)
	assert.Equal(t, "ts", comp.Container)
	assert.Equal(t, "h264", comp.VideoCodec)
	assert.Equal(t, 3000, comp.TargetBitrate)
}
