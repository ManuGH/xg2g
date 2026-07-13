package intents

import (
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// resolvePlannerStartProfile consumes the exact immutable plan carried by the
// verified receipt. It deliberately does not re-run client, network, host
// pressure, operator, or profile-selection policy.
func (s *Service) resolvePlannerStartProfile(intent Intent, hw startHardwareState) (startProfileResolution, *Error) {
	if intent.PlannerPlan == nil || intent.PlanningReceipt == nil || intent.PlannerEvidence == nil {
		return startProfileResolution{}, plannerStartError("planner plan, evidence, and receipt must be provided together")
	}
	plan := *intent.PlannerPlan
	evidence := *intent.PlannerEvidence
	receipt := *intent.PlanningReceipt
	if plan.Decision != playbackplanner.DecisionAllow || plan.Outcome != playbackplanner.DecisionAllow {
		return startProfileResolution{}, plannerStartError("planner receipt does not authorize playback")
	}
	if receipt.PlanHash == "" || receipt.EvidenceHash == "" || receipt.PlannerVersion == "" {
		return startProfileResolution{}, plannerStartError("planner receipt binding is incomplete")
	}
	planHash, err := plan.Hash()
	if err != nil || planHash != receipt.PlanHash {
		return startProfileResolution{}, plannerStartError("planner plan hash does not match receipt")
	}
	evidenceHash, err := evidence.Hash()
	if err != nil || evidenceHash != receipt.EvidenceHash {
		return startProfileResolution{}, plannerStartError("planner evidence hash does not match receipt")
	}
	if normalize.ServiceRef(evidence.SourceIdentity) != normalize.ServiceRef(intent.ServiceRef) {
		return startProfileResolution{}, plannerStartError("planner evidence does not match service reference")
	}
	if err := validatePlannerExecutionPlan(plan); err != nil {
		return startProfileResolution{}, plannerStartError(err.Error())
	}

	profileID, err := plannerExecutionProfileID(plan)
	if err != nil {
		return startProfileResolution{}, plannerStartError(err.Error())
	}
	capability := plannerCapability(evidence)
	profileSpec := s.resolveProfileSpec(profileID, "", capability, hw, profiles.HWAccelAuto)
	profileSpec = applyPlannerPlanToProfile(profileSpec, plan)

	publicProfile := plannerPublicProfile(plan)
	bucket := "0"
	if intent.StartMs != nil && *intent.StartMs > 0 {
		bucket = fmt.Sprintf("%d", *intent.StartMs/1000)
	}
	idempotencyProfile := profileID + "@" + receipt.PlanHash

	resolution := startProfileResolution{
		requestedPlaybackMode: plannerClientPath(evidence, plan),
		publicRequestProfile:  publicProfile,
		effectiveProfileID:    profileID,
		profileSpec:           profileSpec,
		sourceProfile:         plannerSourceProfile(evidence),
		hostPressureBand:      playbackprofile.NormalizeHostPressureBand(evidence.HostSnapshot.PressureBand),
		bucket:                bucket,
		idempotencyKey:        ComputeIdemKey(model.IntentTypeStreamStart, intent.ServiceRef, idempotencyProfile, bucket),
		resolvedIntent:        publicProfile,
	}
	if maxRung := playbackprofile.NormalizeQualityRung(plan.Guardrails.MaxQualityRung); maxRung != playbackprofile.RungUnknown {
		resolution.operatorSnapshot.MaxQualityRung = maxRung
		resolution.operatorSnapshot.OverrideApplied = true
	}
	return resolution, nil
}

func validatePlannerExecutionPlan(plan playbackplanner.PlaybackPlan) error {
	if plan.Startup.DVRWindowSeconds < 0 {
		return fmt.Errorf("planner DVR window cannot be negative")
	}
	if normalize.Token(plan.DeliveryEngine) != "hls" {
		return fmt.Errorf("unsupported planner delivery engine %q", plan.DeliveryEngine)
	}
	switch normalize.Token(plan.Packaging.Container) {
	case "ts", "mpegts", "fmp4", "mp4":
	default:
		return fmt.Errorf("unsupported planner packaging %q", plan.Packaging.Container)
	}
	switch normalize.Token(plan.Audio.Mode) {
	case "copy":
	case "transcode":
		if codec := normalize.Token(plan.Audio.Codec); codec != "aac" {
			return fmt.Errorf("unsupported planner audio codec %q", plan.Audio.Codec)
		}
		if plan.Audio.Channels != 0 && plan.Audio.Channels != 2 {
			return fmt.Errorf("unsupported planner audio channels %d", plan.Audio.Channels)
		}
		if plan.Audio.SampleRate != 0 && plan.Audio.SampleRate != 48000 {
			return fmt.Errorf("unsupported planner audio sample rate %d", plan.Audio.SampleRate)
		}
	default:
		return fmt.Errorf("unsupported planner audio mode %q", plan.Audio.Mode)
	}
	if plan.Filters.ScaleHeight > 0 {
		return fmt.Errorf("planner scale height is not executable yet")
	}
	if plan.RateControl.TargetVideoBitrateKbps > 0 && plan.RateControl.MaxVideoBitrateKbps <= 0 {
		return fmt.Errorf("planner target bitrate requires a maximum bitrate")
	}
	if plan.RateControl.TargetVideoBitrateKbps > plan.RateControl.MaxVideoBitrateKbps && plan.RateControl.MaxVideoBitrateKbps > 0 {
		return fmt.Errorf("planner target bitrate exceeds maximum bitrate")
	}
	return nil
}

func plannerStartError(message string) *Error {
	return &Error{Kind: ErrorInvalidInput, Message: message}
}

func plannerExecutionProfileID(plan playbackplanner.PlaybackPlan) (string, error) {
	switch plan.Video.Mode {
	case "copy":
		return profiles.ProfileCopy, nil
	case "transcode":
		switch normalize.Token(plan.Video.Codec) {
		case "h264", "avc", "libx264":
			return profiles.ProfileH264FMP4, nil
		case "hevc", "h265", "libx265":
			return profiles.ProfileSafariHEVCHW, nil
		case "av1":
			return profiles.ProfileAV1HW, nil
		default:
			return "", fmt.Errorf("unsupported planner video codec %q", plan.Video.Codec)
		}
	default:
		return "", fmt.Errorf("unsupported planner video mode %q", plan.Video.Mode)
	}
}

func applyPlannerPlanToProfile(spec model.ProfileSpec, plan playbackplanner.PlaybackPlan) model.ProfileSpec {
	spec.PlannerBound = true
	spec.DVRWindowSec = plan.Startup.DVRWindowSeconds
	spec.TranscodeVideo = plan.Video.Mode == "transcode"
	spec.AudioMode = plan.Audio.Mode
	spec.AudioCodec = normalize.Token(plan.Audio.Codec)
	spec.Container = plannerProfileContainer(plan.Packaging.Container)
	spec.Deinterlace = spec.TranscodeVideo && plan.Filters.Deinterlace
	spec.VideoMaxWidth = plan.Filters.ScaleWidth
	spec.VideoTargetRateK = plan.RateControl.TargetVideoBitrateKbps
	if plan.RateControl.MaxVideoBitrateKbps > 0 {
		spec.VideoMaxRateK = plan.RateControl.MaxVideoBitrateKbps
		spec.VideoBufSizeK = plan.RateControl.MaxVideoBitrateKbps * 2
	}
	if plan.Audio.BitrateKbps > 0 {
		spec.AudioBitrateK = plan.Audio.BitrateKbps
	}
	if !spec.TranscodeVideo {
		spec.VideoCodec = ""
		spec.HWAccel = ""
		spec.VideoCRF = 0
		spec.VideoQP = 0
		spec.Preset = ""
		spec.BFrames = 0
	} else {
		plannedCodec := normalize.Token(plan.Video.Codec)
		baseCodec := normalize.Token(spec.VideoCodec)
		if plannedCodec != "h264" || (baseCodec != "h264" && baseCodec != "libx264") {
			spec.VideoCodec = plannedCodec
		}
		if plan.RateControl.TargetVideoBitrateKbps > 0 {
			// A planner rate-control target is an explicit bitrate plan, not a
			// legacy CRF/CQP profile hint.
			spec.VideoCRF = 0
			spec.VideoQP = 0
		}
	}
	return spec
}

func plannerProfileContainer(raw string) string {
	switch normalize.Token(raw) {
	case "fmp4", "mp4":
		return "fmp4"
	default:
		return "mpegts"
	}
}

func plannerCapability(evidence playbackplanner.PlaybackEvidence) *scan.Capability {
	truth := evidence.SourceTruth
	return &scan.Capability{
		State:       scan.CapabilityStateOK,
		Container:   truth.Container,
		VideoCodec:  truth.VideoCodec,
		AudioCodec:  truth.AudioCodec,
		Width:       truth.Width,
		Height:      truth.Height,
		FPS:         float64(truth.FPS),
		Interlaced:  truth.Interlaced,
		BitrateKbps: truth.BitrateKbps,
	}
}

func plannerSourceProfile(evidence playbackplanner.PlaybackEvidence) *playbackprofile.SourceProfile {
	truth := evidence.SourceTruth
	return &playbackprofile.SourceProfile{
		Container:   truth.Container,
		VideoCodec:  truth.VideoCodec,
		AudioCodec:  truth.AudioCodec,
		BitrateKbps: truth.BitrateKbps,
		Width:       truth.Width,
		Height:      truth.Height,
		FPS:         float64(truth.FPS),
		Interlaced:  truth.Interlaced,
	}
}

func plannerClientPath(evidence playbackplanner.PlaybackEvidence, plan playbackplanner.PlaybackPlan) string {
	switch normalize.Token(evidence.ClientEvidence.PreferredEngine) {
	case "native":
		return "native_hls"
	case "hls.js", "hlsjs":
		return "hlsjs"
	}
	if plan.DeliveryEngine == "direct" {
		return "direct_mp4"
	}
	if plan.Mode == "transcode" {
		return "transcode"
	}
	return "hlsjs"
}

func plannerPublicProfile(plan playbackplanner.PlaybackPlan) string {
	if plan.Mode == "copy" {
		return profiles.PublicProfileDirect
	}
	if strings.EqualFold(plan.Guardrails.MaxQualityRung, "bandwidth") || strings.EqualFold(plan.Guardrails.MaxQualityRung, "low") {
		return profiles.PublicProfileBandwidth
	}
	return profiles.PublicProfileCompatible
}
