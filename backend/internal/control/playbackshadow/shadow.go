package playbackshadow

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

type ComparablePlaybackPlan struct {
	IsValid         bool   // false if legacy is nil
	TerminalKind    string // "decision" or "problem"
	Outcome         string
	ReasonCode      string
	Mode            string
	Engine          string
	Container       string
	VideoMode       string
	AudioMode       string
	VideoCodec      string
	AudioCodec      string
	TargetBitrate   int
	MaxBitrate      int
	MaxBitrateKnown bool
	ScaleWidth      int
	ScaleHeight     int
	MinQualityRung  string
	MaxQualityRung  string
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
		TerminalKind:   "decision",
		Outcome:        outcome,
		Mode:           mode,
		Engine:         "hls", // live legacy intents are always HLS in this context
		MinQualityRung: trace.VideoQualityRung,
		MaxQualityRung: "", // legacy trace doesn't store max quality rung for live usually
	}

	if prof != nil {
		c.VideoMode = "copy"
		c.AudioMode = "copy"
		if prof.TranscodeVideo {
			c.VideoMode = "transcode"
		}
		if prof.TranscodesAudio() {
			c.AudioMode = "transcode"
		}

		c.VideoCodec = prof.VideoCodec
		if c.VideoCodec == "" && trace.Source != nil {
			c.VideoCodec = trace.Source.VideoCodec
		}
		c.AudioCodec = prof.ResolvedAudioCodec()
		if !prof.TranscodesAudio() && trace.Source != nil {
			c.AudioCodec = trace.Source.AudioCodec
		}
		c.Container = prof.Container
		if c.Container == "" && trace.Source != nil {
			c.Container = trace.Source.Container
		}
		if c.Container == "ts" {
			c.Container = "mpegts" // Align with new planner nomenclature
		}
		c.TargetBitrate = prof.VideoTargetRateK
		if c.TargetBitrate <= 0 {
			c.TargetBitrate = prof.VideoMaxRateK
		}
		c.MaxBitrate = prof.VideoMaxRateK
		c.MaxBitrateKnown = true
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
	switch dec.Mode {
	case decision.ModeTranscode:
		mode = "transcode"
	case decision.ModeDirectStream:
		mode = "remux"
	case decision.ModeDeny:
		mode = "none"
	}

	engine := dec.SelectedOutputKind
	if engine == "" {
		engine = decision.ProtocolFrom(dec)
	}
	if engine == "file" {
		engine = "direct"
	}

	c := ComparablePlaybackPlan{
		IsValid:        true,
		TerminalKind:   "decision",
		Outcome:        outcome,
		ReasonCode:     decision.ReasonPrimaryFrom(dec, nil),
		Mode:           mode,
		Engine:         engine,
		Container:      dec.Selected.Container,
		VideoCodec:     dec.Selected.VideoCodec,
		AudioCodec:     dec.Selected.AudioCodec,
		MinQualityRung: "", // Legacy does not have a MinQualityRung guardrail; dec.Trace.QualityRung is the selected profile rung.
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
	if outcome == "deny" {
		c.Mode = "none"
		c.Engine = "none"
		c.Container = "none"
		c.VideoMode = "none"
		c.AudioMode = "none"
		c.VideoCodec = "none"
		c.AudioCodec = "none"
	}

	return c
}

// ComparableFromLegacyProblem preserves the distinction between an HTTP
// contract problem and a valid deny decision. A problem must never compare as
// equivalent to a planner deny merely because neither path can start playback.
func ComparableFromLegacyProblem(prob *decision.Problem) ComparablePlaybackPlan {
	if prob == nil {
		return ComparablePlaybackPlan{IsValid: false}
	}
	return ComparablePlaybackPlan{
		IsValid:      true,
		TerminalKind: "problem",
		Outcome:      "problem",
		Mode:         "none",
		Engine:       "none",
		Container:    "none",
		VideoMode:    "none",
		AudioMode:    "none",
		VideoCodec:   "none",
		AudioCodec:   "none",
		ReasonCode:   prob.Code,
	}
}

func ComparableFromPlanner(plan playbackplanner.PlaybackPlan) ComparablePlaybackPlan {
	comparable := ComparablePlaybackPlan{
		IsValid:         true,
		TerminalKind:    "decision",
		Outcome:         plan.Outcome,
		ReasonCode:      plan.ReasonCode,
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
	if plan.Outcome == "deny" {
		comparable.Mode = "none"
		comparable.Engine = "none"
		comparable.Container = "none"
		comparable.VideoMode = "none"
		comparable.AudioMode = "none"
		comparable.VideoCodec = "none"
		comparable.AudioCodec = "none"
	}
	return comparable
}

// DiffComparablePlans compares two plans and returns bounded mismatch codes.
// e.g. "mode_mismatch", "packaging_mismatch", "target_bitrate_drift"
func DiffComparablePlans(legacy, new ComparablePlaybackPlan) []string {
	var diffs []string

	if !legacy.IsValid {
		return []string{"legacy_invalid"}
	}
	if !new.IsValid {
		return []string{"planner_invalid"}
	}
	if legacy.TerminalKind != new.TerminalKind {
		diffs = append(diffs, "terminal_kind_mismatch")
	}
	if legacy.Outcome != new.Outcome {
		diffs = append(diffs, "outcome_mismatch")
	}
	if (legacy.Outcome == "deny" || legacy.TerminalKind == "problem") && legacy.ReasonCode != new.ReasonCode {
		diffs = append(diffs, "reason_mismatch")
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
	if legacy.TargetBitrate > 0 && new.TargetBitrate > 0 && legacy.TargetBitrate != new.TargetBitrate {
		diffs = append(diffs, "target_bitrate_drift")
	}
	if legacy.MaxBitrateKnown && new.MaxBitrateKnown && legacy.MaxBitrate != new.MaxBitrate {
		diffs = append(diffs, "max_bitrate_drift")
	}
	if legacy.ScaleWidth != new.ScaleWidth || legacy.ScaleHeight != new.ScaleHeight {
		diffs = append(diffs, "scale_drift")
	}
	if legacy.MinQualityRung != "" && new.MinQualityRung != "" && legacy.MinQualityRung != new.MinQualityRung {
		diffs = append(diffs, "guardrails_mismatch")
	}
	if legacy.MaxQualityRung != "" && new.MaxQualityRung != "" && legacy.MaxQualityRung != new.MaxQualityRung {
		diffs = append(diffs, "guardrails_mismatch")
	}

	return diffs
}

type DiffDisposition string

const (
	DiffAccepted    DiffDisposition = "accepted"
	DiffUnexplained DiffDisposition = "unexplained"
)

type ClassifiedDiff struct {
	Code        string
	Disposition DiffDisposition
	Reason      string
}

// ClassifyComparableDiffs classifies strict raw differences. It never removes
// information from DiffComparablePlans; accepted differences remain observable
// and can be counted separately from unexplained cutover blockers.
func ClassifyComparableDiffs(legacy, planner ComparablePlaybackPlan) []ClassifiedDiff {
	return classifyComparableDiffs(legacy, planner, nil)
}

// ClassifyComparableDiffsWithEvidence permits only differences that can be
// proven from the immutable evidence itself. The raw diff remains observable.
func ClassifyComparableDiffsWithEvidence(legacy, planner ComparablePlaybackPlan, evidence playbackplanner.PlaybackEvidence) []ClassifiedDiff {
	return classifyComparableDiffs(legacy, planner, &evidence)
}

func classifyComparableDiffs(legacy, planner ComparablePlaybackPlan, evidence *playbackplanner.PlaybackEvidence) []ClassifiedDiff {
	raw := DiffComparablePlans(legacy, planner)
	classified := make([]ClassifiedDiff, 0, len(raw))
	for _, code := range raw {
		item := ClassifiedDiff{Code: code, Disposition: DiffUnexplained}
		switch code {
		case "packaging_mismatch":
			if legacy.Engine == "hls" && planner.Engine == "hls" &&
				((legacy.Container == "hls" && isConcreteHLSSegmentContainer(planner.Container)) ||
					(planner.Container == "hls" && isConcreteHLSSegmentContainer(legacy.Container))) {
				item.Disposition = DiffAccepted
				item.Reason = "generic_hls_wrapper_vs_known_segment_container"
			} else if legacy.Engine == "hls" && planner.Engine == "hls" &&
				legacy.Container == "fmp4" && planner.Container == "mpegts" &&
				planner.VideoMode == "copy" {
				// Intended divergence: ffmpeg's HLS fMP4 muxer corrupts the
				// leading B-frame at every segment cut on open-GOP broadcast
				// sources, so the planner keeps copied video on MPEG-TS.
				item.Disposition = DiffAccepted
				item.Reason = "copied_video_avoids_fmp4_open_gop_defect"
			}
		case "audio_mode_mismatch":
			if legacy.Mode == "transcode" && planner.Mode == "transcode" &&
				legacy.AudioMode == "transcode" && planner.AudioMode == "copy" &&
				legacy.AudioCodec != "" && legacy.AudioCodec == planner.AudioCodec {
				item.Disposition = DiffAccepted
				item.Reason = "compatible_audio_copy_avoids_reencode"
			} else if legacy.AudioMode == "copy" && planner.AudioMode == "transcode" &&
				plannerTranscodesDolbyForBrowser(planner, evidence) {
				item.Disposition = DiffAccepted
				item.Reason = playbackplanner.ReasonBrowserCannotDecodeDolby
			}
		case "audio_codec_mismatch":
			if legacy.AudioMode == "copy" && planner.AudioMode == "transcode" &&
				plannerTranscodesDolbyForBrowser(planner, evidence) {
				item.Disposition = DiffAccepted
				item.Reason = playbackplanner.ReasonBrowserCannotDecodeDolby
			}
		case "mode_mismatch":
			if legacy.Mode == "remux" && planner.Mode == "transcode" &&
				planner.VideoMode == "copy" && planner.AudioMode == "transcode" &&
				plannerTranscodesDolbyForBrowser(planner, evidence) {
				item.Disposition = DiffAccepted
				item.Reason = playbackplanner.ReasonBrowserCannotDecodeDolby
			}
		case "video_mode_mismatch":
			if legacy.Mode == "transcode" && planner.Mode == "transcode" &&
				legacy.VideoMode == "transcode" && planner.VideoMode == "copy" &&
				legacy.VideoCodec != "" && legacy.VideoCodec == planner.VideoCodec &&
				legacy.AudioMode == "transcode" && planner.AudioMode == "transcode" {
				item.Disposition = DiffAccepted
				item.Reason = "compatible_video_copy_avoids_reencode_during_audio_transcode"
			}
		case "scale_drift":
			if evidence != nil && legacy.Mode == "transcode" && planner.Mode == "transcode" {
				if legacy.ScaleWidth == 0 && legacy.ScaleHeight == 0 && planner.ScaleHeight == 0 &&
					evidence.SourceTruth.Width > evidence.ClientEvidence.MaxVideoWidth &&
					evidence.ClientEvidence.MaxVideoWidth > 0 &&
					planner.ScaleWidth == evidence.ClientEvidence.MaxVideoWidth {
					item.Disposition = DiffAccepted
					item.Reason = "signed_client_width_limit_enforced"
				} else if planner.ScaleWidth == 0 && planner.ScaleHeight == 0 &&
					legacy.ScaleWidth == evidence.SourceTruth.Width &&
					legacy.ScaleHeight == evidence.SourceTruth.Height {
					item.Disposition = DiffAccepted
					item.Reason = "planner_omits_legacy_fixed_scaling"
				}
			}
		}
		classified = append(classified, item)
	}
	return classified
}

func isConcreteHLSSegmentContainer(value string) bool {
	return value == "fmp4" || value == "mpegts"
}

// plannerTranscodesDolbyForBrowser asks the planner's capability truth table
// (the single source of the veto) whether the source audio claim was
// overridden for this client — the planner then deliberately diverges from
// legacy by transcoding to AAC, which is an intended, explained difference.
func plannerTranscodesDolbyForBrowser(planner ComparablePlaybackPlan, evidence *playbackplanner.PlaybackEvidence) bool {
	if evidence == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(planner.AudioCodec), "aac") {
		return false
	}
	reason, vetoed := playbackplanner.VetoedCapability("audio", evidence.SourceTruth.AudioCodec, evidence.ClientEvidence)
	return vetoed && reason == playbackplanner.ReasonBrowserCannotDecodeDolby
}

func UnexplainedDiffCodes(legacy, planner ComparablePlaybackPlan) []string {
	classified := ClassifyComparableDiffs(legacy, planner)
	return unexplainedDiffCodes(classified)
}

func UnexplainedDiffCodesWithEvidence(legacy, planner ComparablePlaybackPlan, evidence playbackplanner.PlaybackEvidence) []string {
	classified := ClassifyComparableDiffsWithEvidence(legacy, planner, evidence)
	return unexplainedDiffCodes(classified)
}

func unexplainedDiffCodes(classified []ClassifiedDiff) []string {
	out := make([]string, 0, len(classified))
	for _, diff := range classified {
		if diff.Disposition == DiffUnexplained {
			out = append(out, diff.Code)
		}
	}
	return out
}
