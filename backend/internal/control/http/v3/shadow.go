package v3

import (
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

type ComparablePlaybackPlan struct {
	IsValid          bool // false if legacy is nil
	Outcome          string
	Mode             string
	Engine           string
	Container        string
	VideoMode        string
	AudioMode        string
	VideoCodec       string
	AudioCodec       string
	TargetBitrate    int
	MaxBitrate       int
	MaxBitrateKnown  bool
	ScaleWidth       int
	ScaleHeight      int
	MinQualityRung   string
	MaxQualityRung   string
}

func ComparableFromLegacySession(trace *model.PlaybackTrace, prof *ports.ProfileSpec) ComparablePlaybackPlan {
	if trace == nil {
		return ComparablePlaybackPlan{IsValid: false}
	}

	outcome := "allow"
	// trace doesn't explicitly store 'deny'. The HTTP handler rejects it and no session is created.
	// We handle deny explicitly via the characterization test outcome.

	mode := "remux"
	if prof != nil && prof.TranscodeVideo {
		mode = "transcode"
	}

	c := ComparablePlaybackPlan{
		IsValid:        true,
		Outcome:        outcome,
		Mode:           mode,
		Engine:         "hls", // live legacy intents are always HLS in this context
		MinQualityRung: trace.VideoQualityRung,
		MaxQualityRung: "", // legacy trace doesn't store max quality rung for live usually
	}

	if prof != nil {
		c.VideoMode = "remux"
		c.AudioMode = "remux"
		if prof.TranscodeVideo {
			c.VideoMode = "transcode"
			// Audio is somewhat hardcoded in legacy to transcode if video does, or copy if not
			c.AudioMode = "transcode"
		}
		
		c.VideoCodec = prof.VideoCodec
		if c.VideoCodec == "" && trace.Source != nil {
			c.VideoCodec = trace.Source.VideoCodec
		}
		c.AudioCodec = "aac" // legacy intent usually defaults audio to aac for transcode
		if !prof.TranscodeVideo && trace.Source != nil {
			c.AudioCodec = trace.Source.AudioCodec
		}
		c.Container = prof.Container
		if c.Container == "" && trace.Source != nil {
			c.Container = trace.Source.Container
		}
		c.TargetBitrate = prof.VideoMaxRateK
		c.MaxBitrateKnown = false
		c.ScaleWidth = prof.VideoMaxWidth
		c.ScaleHeight = 0 // Legacy doesn't explicitly target height except via source matching
	} else {
		c.VideoMode = "copy"
		c.AudioMode = "copy"
	}

	return c
}

func ComparableFromLegacy(dec *decision.Decision) ComparablePlaybackPlan {
	if dec == nil {
		return ComparablePlaybackPlan{IsValid: false}
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
		IsValid:        true,
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
		c.VideoCodec = dec.TargetProfile.Video.Codec
		c.AudioCodec = dec.TargetProfile.Audio.Codec
		c.Container = dec.TargetProfile.Container
		c.TargetBitrate = dec.TargetProfile.Video.BitrateKbps
		c.MaxBitrateKnown = false // Legacy doesn't distinct Target vs Max correctly here or uses same. Unknown is safer.
		c.ScaleWidth = dec.TargetProfile.Video.Width
		c.ScaleHeight = dec.TargetProfile.Video.Height
	} else {
		c.VideoMode = "copy"
		c.AudioMode = "copy"
	}

	return c
}

func ComparableFromPlanner(plan playbackplanner.PlaybackPlan) ComparablePlaybackPlan {
	return ComparablePlaybackPlan{
		IsValid:         true,
		Outcome:         plan.Outcome,
		Mode:            plan.Mode,
		Engine:          plan.DeliveryEngine,
		Container:       plan.Packaging.Container,
		VideoMode:       plan.Video.Mode,
		AudioMode:       plan.Audio.Mode,
		VideoCodec:      plan.Video.Codec,
		AudioCodec:      plan.Audio.Codec,
		TargetBitrate:   plan.RateControl.TargetVideoBitrateKbps,
		MaxBitrate:      plan.RateControl.MaxVideoBitrateKbps,
		MaxBitrateKnown: true,
		ScaleWidth:      plan.Filters.ScaleWidth,
		ScaleHeight:     plan.Filters.ScaleHeight,
		MinQualityRung:  plan.Guardrails.MinQualityRung,
		MaxQualityRung:  plan.Guardrails.MaxQualityRung,
	}
}

// DiffComparablePlans compares two plans and returns bounded mismatch codes.
// e.g. "mode_mismatch", "packaging_mismatch", "target_bitrate_drift"
func DiffComparablePlans(legacy, new ComparablePlaybackPlan) []string {
	var diffs []string

	if !legacy.IsValid {
		return []string{"legacy_invalid"}
	}

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
	if legacy.MaxBitrateKnown && new.MaxBitrateKnown && legacy.MaxBitrate != new.MaxBitrate {
		diffs = append(diffs, "max_bitrate_drift")
	}
	if legacy.ScaleWidth != new.ScaleWidth || legacy.ScaleHeight != new.ScaleHeight {
		diffs = append(diffs, "scale_drift")
	}
	if legacy.MinQualityRung != new.MinQualityRung {
		diffs = append(diffs, "guardrails_mismatch")
	} else if legacy.MaxQualityRung != "" && legacy.MaxQualityRung != new.MaxQualityRung {
		diffs = append(diffs, "guardrails_mismatch")
	}

	return diffs
}
