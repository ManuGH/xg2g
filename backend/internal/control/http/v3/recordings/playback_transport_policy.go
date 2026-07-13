package recordings

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/control/http/v3/clientpolicy"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

type playbackTransportPlanID string

const (
	playbackTransportPlanUnknown                  playbackTransportPlanID = ""
	playbackTransportPlanLiveNativeFMP4           playbackTransportPlanID = "live_native_fmp4"
	playbackTransportPlanLiveNativeDirectStream   playbackTransportPlanID = "live_native_direct_stream_fmp4"
	playbackTransportPlanRecordingNativeFMP4      playbackTransportPlanID = "recording_native_fmp4"
	playbackTransportPlanRecordingNativeDirectHLS playbackTransportPlanID = "recording_native_direct_stream_fmp4"
)

type playbackTransportPlan struct {
	id                  playbackTransportPlanID
	rewriteDirectStream bool
	targetContainer     string
	targetPackaging     playbackprofile.Packaging
	hlsSegmentContainer string
	selectedContainer   string
	qualityRung         playbackprofile.QualityRung
	applied             bool
}

func applyPlaybackTransportPolicy(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) {
	applyPlaybackTransportPolicyWithPolicy(req, resolvedCaps, dec, false)
}

func applyPlaybackTransportPolicyWithPolicy(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision, experimentalAV1MPEGTS bool) {
	plan := resolvePlaybackTransportPlanWithPolicy(req, resolvedCaps, dec, experimentalAV1MPEGTS)
	if !plan.applied || dec == nil || dec.TargetProfile == nil {
		return
	}

	if plan.rewriteDirectStream {
		rewriteDecisionToDirectStream(dec)
	}
	if dec.TargetProfile == nil || dec.SelectedOutputKind != "hls" || !dec.TargetProfile.HLS.Enabled {
		return
	}

	target := *dec.TargetProfile
	target.Container = plan.targetContainer
	target.Packaging = plan.targetPackaging
	target.HLS.Enabled = true
	target.HLS.SegmentContainer = plan.hlsSegmentContainer
	canonical := playbackprofile.CanonicalizeTarget(target)
	dec.TargetProfile = &canonical
	if plan.selectedContainer != "" {
		dec.Selected.Container = plan.selectedContainer
	}
	if dec.Mode == decision.ModeDirectStream && plan.qualityRung != playbackprofile.RungUnknown {
		dec.Trace.QualityRung = string(plan.qualityRung)
	}
}

func clientWantsFMP4(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, _ *decision.Decision) bool {
	return clientpolicy.WantsFMP4Packaging(req.RequestedProfile, resolvedCaps.ClientFamilyFallback)
}

func resolvePlaybackTransportPlan(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) playbackTransportPlan {
	return resolvePlaybackTransportPlanWithPolicy(req, resolvedCaps, dec, false)
}

func resolvePlaybackTransportPlanWithPolicy(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision, experimentalAV1MPEGTS bool) playbackTransportPlan {
	switch req.SubjectKind {
	case PlaybackSubjectLive:
		return resolveLiveNativeTransportPlan(req, resolvedCaps, dec, experimentalAV1MPEGTS)
	case PlaybackSubjectRecording:
		return resolveRecordingNativeTransportPlan(req, resolvedCaps, dec)
	default:
		return noPlaybackTransportPlan()
	}
}

func resolveLiveNativeTransportPlan(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision, experimentalAV1MPEGTS bool) playbackTransportPlan {
	if dec == nil || dec.TargetProfile == nil {
		return noPlaybackTransportPlan()
	}
	if !clientWantsFMP4(req, resolvedCaps, dec) {
		return noPlaybackTransportPlan()
	}
	if clientpolicy.AllowExperimentalNativeAV1TransportStreamWithPolicy(resolvedCaps, dec.Selected.VideoCodec, *dec.TargetProfile, experimentalAV1MPEGTS) {
		return noPlaybackTransportPlan()
	}

	plan := buildPlaybackTransportPlan(
		playbackTransportPlanLiveNativeFMP4,
		"fmp4",
		playbackprofile.PackagingFMP4,
		"fmp4",
		"fmp4",
		playbackprofile.RungCompatibleHLSFMP4,
	)
	if shouldPreferNativeDirectStream(dec) {
		plan.id = playbackTransportPlanLiveNativeDirectStream
		plan.rewriteDirectStream = true
	}
	return plan
}

func resolveRecordingNativeTransportPlan(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) playbackTransportPlan {
	if dec == nil || dec.TargetProfile == nil {
		return noPlaybackTransportPlan()
	}
	if !clientWantsFMP4(req, resolvedCaps, dec) {
		return noPlaybackTransportPlan()
	}
	if shouldPreferNativeDirectStream(dec) && recordingClientSupportsDirectTransportStream(req.RequestedProfile, resolvedCaps.ClientFamilyFallback) {
		return noPlaybackTransportPlan()
	}

	plan := buildPlaybackTransportPlan(
		playbackTransportPlanRecordingNativeFMP4,
		"mp4",
		playbackprofile.PackagingFMP4,
		"fmp4",
		"",
		playbackprofile.RungCompatibleHLSFMP4,
	)
	if shouldPreferNativeDirectStream(dec) {
		plan.id = playbackTransportPlanRecordingNativeDirectHLS
		plan.rewriteDirectStream = true
	}
	return plan
}

func noPlaybackTransportPlan() playbackTransportPlan {
	return playbackTransportPlan{
		id:          playbackTransportPlanUnknown,
		qualityRung: playbackprofile.RungUnknown,
	}
}

func buildPlaybackTransportPlan(
	id playbackTransportPlanID,
	targetContainer string,
	targetPackaging playbackprofile.Packaging,
	hlsSegmentContainer string,
	selectedContainer string,
	qualityRung playbackprofile.QualityRung,
) playbackTransportPlan {
	return playbackTransportPlan{
		id:                  id,
		targetContainer:     targetContainer,
		targetPackaging:     targetPackaging,
		hlsSegmentContainer: hlsSegmentContainer,
		selectedContainer:   selectedContainer,
		qualityRung:         qualityRung,
		applied:             true,
	}
}

func shouldPreferNativeDirectStream(dec *decision.Decision) bool {
	if dec == nil || dec.Mode != decision.ModeDirectPlay || dec.TargetProfile == nil {
		return false
	}

	target := playbackprofile.CanonicalizeTarget(*dec.TargetProfile)
	if target.Video.Mode != playbackprofile.MediaModeCopy || target.Audio.Mode != playbackprofile.MediaModeCopy {
		return false
	}

	switch normalize.Token(target.Container) {
	case "ts", "mpegts":
		return true
	}
	return target.Packaging == playbackprofile.PackagingTS
}

func rewriteDecisionToDirectStream(dec *decision.Decision) {
	if dec == nil || dec.TargetProfile == nil {
		return
	}

	target := playbackprofile.CanonicalizeTarget(*dec.TargetProfile)
	target.HLS.Enabled = true
	target.HLS.SegmentContainer = "mpegts"
	canonical := playbackprofile.CanonicalizeTarget(target)

	dec.Mode = decision.ModeDirectStream
	dec.Outputs = []decision.Output{
		{
			Kind: "hls",
			URL:  "placeholder://direct-stream.m3u8",
		},
	}
	dec.TargetProfile = &canonical
	dec.Reasons = []decision.ReasonCode{decision.ReasonDirectStreamMatch}
	dec.SelectedOutputKind = "hls"
	dec.SelectedOutputURL = "placeholder://direct-stream.m3u8"
	dec.Trace.ResolvedIntent = playbackprofile.PublicIntentName(playbackprofile.IntentCompatible)
	dec.Trace.QualityRung = string(playbackprofile.RungCompatibleHLSTS)
	dec.Trace.AudioQualityRung = ""
	dec.Trace.VideoQualityRung = ""
	if dec.Trace.RequestedIntent == string(playbackprofile.IntentDirect) {
		dec.Trace.DegradedFrom = string(playbackprofile.IntentDirect)
	} else {
		dec.Trace.DegradedFrom = ""
	}
	dec.Trace.Why = []decision.Reason{
		{
			Code: decision.ReasonDirectStreamMatch,
		},
	}
}

func recordingClientSupportsDirectTransportStream(requestedProfile string, clientFamily string) bool {
	switch strings.ToLower(strings.TrimSpace(requestedProfile)) {
	case "android_native", "android_tv_native":
		return true
	}

	switch strings.ToLower(strings.TrimSpace(clientFamily)) {
	case "android_native", "android_tv_native":
		return true
	default:
		return false
	}
}
