package v3

import (
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
)

type ComparablePlaybackPlan struct {
	Outcome        string
	Mode           string
	Engine         string
	Container      string
	VideoMode      string
	AudioMode      string
	VideoCodec     string
	AudioCodec     string
	TargetBitrate  int
	MaxBitrate     int
	ScaleWidth     int
	ScaleHeight    int
	MinQualityRung string
	MaxQualityRung string
}

func ComparableFromLegacy(dec *decision.Decision) ComparablePlaybackPlan {
	if dec == nil {
		return ComparablePlaybackPlan{Outcome: "deny"}
	}

	outcome := "allow"
	if dec.Mode == decision.ModeDeny {
		outcome = "deny"
	}

	mode := "copy"
	if dec.Mode == decision.ModeTranscode {
		mode = "transcode"
	} else if dec.Mode == decision.ModeDirectStream {
		mode = "remux"
	}

	engine := dec.SelectedOutputKind
	if engine == "" {
		engine = decision.ProtocolFrom(dec)
	}

	c := ComparablePlaybackPlan{
		Outcome:        outcome,
		Mode:           mode,
		Engine:         engine,
		Container:      dec.Selected.Container,
		VideoCodec:     dec.Selected.VideoCodec,
		AudioCodec:     dec.Selected.AudioCodec,
		MinQualityRung: dec.Trace.QualityRung, // rough map
		MaxQualityRung: dec.Trace.MaxQualityRung,
	}

	if dec.TargetProfile != nil {
		c.VideoMode = string(dec.TargetProfile.Video.Mode)
		c.AudioMode = string(dec.TargetProfile.Audio.Mode)
		c.TargetBitrate = dec.TargetProfile.Video.BitrateKbps
		c.MaxBitrate = dec.TargetProfile.Video.BitrateKbps // Legacy has no split max bitrate
		c.ScaleWidth = dec.TargetProfile.Video.Width
		c.ScaleHeight = dec.TargetProfile.Video.Height
	} else {
		// If copy mode, set mode implicitly
		c.VideoMode = "copy"
		c.AudioMode = "copy"
	}

	return c
}

func ComparableFromPlanner(plan playbackplanner.PlaybackPlan) ComparablePlaybackPlan {
	return ComparablePlaybackPlan{
		Outcome:        plan.Outcome,
		Mode:           plan.Mode,
		Engine:         plan.DeliveryEngine,
		Container:      plan.Packaging.Container,
		VideoMode:      plan.Codecs.Video,
		AudioMode:      plan.Codecs.Audio,
		VideoCodec:     plan.Codecs.Video,
		AudioCodec:     plan.Codecs.Audio,
		TargetBitrate:  plan.RateControl.TargetVideoBitrateKbps,
		MaxBitrate:     plan.RateControl.MaxVideoBitrateKbps,
		ScaleWidth:     plan.Filters.ScaleWidth,
		ScaleHeight:    plan.Filters.ScaleHeight,
		MinQualityRung: plan.Guardrails.MinQualityRung,
		MaxQualityRung: plan.Guardrails.MaxQualityRung,
	}
}

// DiffComparablePlans compares two plans and returns bounded mismatch codes.
// e.g. "mode_mismatch", "packaging_mismatch", "target_bitrate_drift"
func DiffComparablePlans(legacy, new ComparablePlaybackPlan) []string {
	var diffs []string

	if legacy.Outcome != new.Outcome {
		diffs = append(diffs, "outcome_mismatch")
	}
	if legacy.Mode != new.Mode {
		diffs = append(diffs, "mode_mismatch")
	}
	if legacy.Engine != new.Engine {
		diffs = append(diffs, "engine_mismatch")
	}
	if legacy.Container != new.Container {
		diffs = append(diffs, "packaging_mismatch")
	}
	if legacy.VideoMode != new.VideoMode {
		diffs = append(diffs, "video_mode_mismatch")
	}
	if legacy.AudioMode != new.AudioMode {
		diffs = append(diffs, "audio_mode_mismatch")
	}
	if legacy.VideoCodec != new.VideoCodec {
		diffs = append(diffs, "video_codec_mismatch")
	}
	if legacy.AudioCodec != new.AudioCodec {
		diffs = append(diffs, "audio_codec_mismatch")
	}
	if legacy.TargetBitrate != new.TargetBitrate {
		diffs = append(diffs, "target_bitrate_drift")
	}
	if legacy.MaxBitrate != new.MaxBitrate {
		diffs = append(diffs, "max_bitrate_drift")
	}
	if legacy.ScaleWidth != new.ScaleWidth || legacy.ScaleHeight != new.ScaleHeight {
		diffs = append(diffs, "scale_drift")
	}
	if legacy.MinQualityRung != new.MinQualityRung || legacy.MaxQualityRung != new.MaxQualityRung {
		diffs = append(diffs, "guardrails_mismatch")
	}

	return diffs
}
