package recordings

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

func (s *Service) applyCapabilityRegistryFallback(ctx context.Context, req PlaybackInfoRequest) PlaybackInfoRequest {
	registry := s.deps.CapabilityRegistry()
	if registry == nil {
		return req
	}

	identity := deviceIdentityForRequest(req, capabilities.PlaybackCapabilities{})
	if !capabilityLookupEligible(identity) {
		return req
	}

	cached, ok, err := registry.LookupCapabilities(ctx, identity)
	if err != nil {
		log.L().Warn().Err(err).Str("clientProfile", req.ClientProfile).Msg("capability registry lookup failed")
		return req
	}
	if !ok {
		return req
	}

	switch {
	case req.Capabilities == nil:
		req.Capabilities = &cached
	case req.Capabilities.RuntimeProbeUsed:
		return req
	default:
		merged := mergeSparseCapabilities(*req.Capabilities, cached)
		req.Capabilities = &merged
	}
	return req
}

func (s *Service) rememberCapabilitySnapshots(ctx context.Context, sourceRef string, req PlaybackInfoRequest, truth playback.MediaTruth, resolved capabilities.PlaybackCapabilities) (string, string, string) {
	registry := s.deps.CapabilityRegistry()
	if registry == nil {
		return "", "", ""
	}

	hostSnapshot := hostSnapshotForRequest(s.deps.HostRuntime(ctx))
	hostFingerprint := hostSnapshot.Identity.Fingerprint()
	if err := registry.RememberHost(ctx, hostSnapshot); err != nil {
		log.L().Warn().Err(err).Str("requestId", req.RequestID).Msg("capability registry host snapshot failed")
	}

	deviceSnapshot := capreg.DeviceSnapshot{
		Identity:     deviceIdentityForRequest(req, resolved),
		Capabilities: resolved,
		Network:      cloneNetworkContext(resolveNetworkContext(req, resolved)),
		UpdatedAt:    time.Now().UTC(),
	}
	deviceFingerprint := deviceSnapshot.Identity.Fingerprint()
	if capabilityLookupEligible(deviceSnapshot.Identity) {
		if err := registry.RememberDevice(ctx, deviceSnapshot); err != nil {
			log.L().Warn().Err(err).Str("requestId", req.RequestID).Msg("capability registry device snapshot failed")
		}
	}

	sourceSnapshot := s.sourceSnapshotForRequest(ctx, sourceRef, req, truth)
	sourceFingerprint := sourceSnapshot.Fingerprint()
	if sourceFingerprint != "" {
		if err := registry.RememberSource(ctx, sourceSnapshot); err != nil {
			log.L().Warn().Err(err).Str("requestId", req.RequestID).Msg("capability registry source snapshot failed")
		}
	}

	return hostFingerprint, deviceFingerprint, sourceFingerprint
}

func (s *Service) recordCapabilityObservation(ctx context.Context, sourceRef string, req PlaybackInfoRequest, truth playback.MediaTruth, resolved capabilities.PlaybackCapabilities, dec *decision.Decision, hostFingerprint string, deviceFingerprint string, sourceFingerprint string) {
	registry := s.deps.CapabilityRegistry()
	if registry == nil || dec == nil {
		return
	}

	observation := capreg.PlaybackObservation{
		ObservedAt:         time.Now().UTC(),
		RequestID:          req.RequestID,
		ObservationKind:    "decision",
		Outcome:            "predicted",
		SourceRef:          strings.TrimSpace(sourceRef),
		SourceFingerprint:  strings.TrimSpace(sourceFingerprint),
		SubjectKind:        string(req.SubjectKind),
		RequestedIntent:    dec.Trace.RequestedIntent,
		ResolvedIntent:     dec.Trace.ResolvedIntent,
		Mode:               string(dec.Mode),
		SelectedContainer:  dec.Selected.Container,
		SelectedVideoCodec: dec.Selected.VideoCodec,
		SelectedAudioCodec: dec.Selected.AudioCodec,
		SourceWidth:        truth.Width,
		SourceHeight:       truth.Height,
		SourceFPS:          truth.FPS,
		HostFingerprint:    hostFingerprint,
		DeviceFingerprint:  deviceFingerprint,
		ClientCapsHash:     capreg.HashCapabilitiesSnapshot(resolved),
		Network:            cloneNetworkContext(resolveNetworkContext(req, resolved)),
	}

	if err := registry.RecordObservation(ctx, observation); err != nil {
		log.L().Warn().Err(err).Str("requestId", req.RequestID).Msg("capability registry observation failed")
	}
}

func (s *Service) sourceSnapshotForRequest(ctx context.Context, sourceRef string, req PlaybackInfoRequest, truth playback.MediaTruth) capreg.SourceSnapshot {
	sourceSnapshot := capreg.SourceSnapshot{
		SubjectKind:     string(req.SubjectKind),
		Container:       truth.Container,
		VideoCodec:      truth.VideoCodec,
		AudioCodec:      truth.AudioCodec,
		Width:           truth.Width,
		Height:          truth.Height,
		FPS:             truth.FPS,
		Interlaced:      truth.Interlaced,
		ReceiverContext: cloneReceiverContext(s.deps.ReceiverContext(ctx)),
		UpdatedAt:       time.Now().UTC(),
	}

	flags := make([]string, 0, 6)
	switch req.SubjectKind {
	case PlaybackSubjectLive:
		origin, liveFlags := liveSourceOriginAndFlags(sourceRef, s.deps.ChannelTruthSource())
		sourceSnapshot.Origin = origin
		flags = append(flags, liveFlags...)
	case PlaybackSubjectRecording:
		sourceSnapshot.Origin = "recording_truth"
	default:
		sourceSnapshot.Origin = "runtime_truth"
	}

	flags = append(flags, sourceTruthProblemFlags(truth)...)
	sourceSnapshot.ProblemFlags = flags
	return sourceSnapshot
}

func liveSourceOriginAndFlags(sourceRef string, source ChannelTruthSource) (string, []string) {
	if source == nil {
		return "live_fallback", []string{"legacy_live_assumption", "scanner_unavailable"}
	}

	capability, found := source.GetCapability(sourceRef)
	if !found {
		return "live_fallback", []string{"legacy_live_assumption", "missing_scan_truth"}
	}

	normalized := capability.Normalized()
	if normalized.IsInactiveEventFeed() {
		return "live_fallback", []string{"legacy_live_assumption", "inactive_event_feed"}
	}
	if !normalized.HasMediaTruth() {
		flags := []string{"legacy_live_assumption", "incomplete_scan_truth"}
		switch normalized.State {
		case scan.CapabilityStatePartial:
			flags = append(flags, "partial_scan_truth")
		case scan.CapabilityStateFailed:
			flags = append(flags, "failed_scan_truth")
		}
		if strings.TrimSpace(normalized.FailureReason) != "" {
			flags = append(flags, "scan_failure_reason_set")
		}
		return "live_fallback", flags
	}

	return "live_scan", nil
}

func sourceTruthProblemFlags(truth playback.MediaTruth) []string {
	flags := make([]string, 0, 5)
	if truth.Interlaced {
		flags = append(flags, "interlaced")
	}
	if truth.Width <= 0 || truth.Height <= 0 {
		flags = append(flags, "missing_dimensions")
	}
	if truth.FPS <= 0 {
		flags = append(flags, "missing_fps")
	}
	if strings.TrimSpace(truth.Container) == "" {
		flags = append(flags, "missing_container")
	}
	if strings.TrimSpace(truth.VideoCodec) == "" {
		flags = append(flags, "missing_video_codec")
	}
	if strings.TrimSpace(truth.AudioCodec) == "" {
		flags = append(flags, "missing_audio_codec")
	}
	return flags
}

func deviceIdentityForRequest(req PlaybackInfoRequest, resolved capabilities.PlaybackCapabilities) capreg.DeviceIdentity {
	clientFamily := strings.TrimSpace(resolved.ClientFamilyFallback)
	if clientFamily == "" {
		clientFamily = strings.TrimSpace(req.ClientProfile)
	}

	clientCapsSource := strings.TrimSpace(resolved.ClientCapsSource)
	if clientCapsSource == "" && req.Capabilities != nil {
		clientCapsSource = strings.TrimSpace(req.Capabilities.ClientCapsSource)
	}

	deviceType := strings.TrimSpace(resolved.DeviceType)
	if deviceType == "" && req.Capabilities != nil {
		deviceType = strings.TrimSpace(req.Capabilities.DeviceType)
	}

	return capreg.DeviceIdentity{
		ClientFamily:     clientFamily,
		ClientCapsSource: clientCapsSource,
		DeviceType:       deviceType,
		DeviceContext:    cloneDeviceContext(resolveDeviceContext(req, resolved)),
	}
}

func capabilityLookupEligible(identity capreg.DeviceIdentity) bool {
	if identity.DeviceContext != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(identity.ClientFamily)) {
	case "android_native", "android_tv_native", "ios_safari_native", "safari_native":
		return strings.TrimSpace(identity.DeviceType) != ""
	default:
		return false
	}
}

func resolveDeviceContext(req PlaybackInfoRequest, resolved capabilities.PlaybackCapabilities) *capabilities.DeviceContext {
	if resolved.DeviceContext != nil {
		return resolved.DeviceContext
	}
	if req.Capabilities != nil {
		return req.Capabilities.DeviceContext
	}
	return nil
}

func resolveNetworkContext(req PlaybackInfoRequest, resolved capabilities.PlaybackCapabilities) *capabilities.NetworkContext {
	if resolved.NetworkContext != nil {
		return resolved.NetworkContext
	}
	if req.Capabilities != nil {
		return req.Capabilities.NetworkContext
	}
	return nil
}

func mergeSparseCapabilities(current capabilities.PlaybackCapabilities, cached capabilities.PlaybackCapabilities) capabilities.PlaybackCapabilities {
	if current.RuntimeProbeUsed {
		return current
	}

	merged := current
	if len(merged.Containers) == 0 {
		merged.Containers = append([]string(nil), cached.Containers...)
	}
	if len(merged.VideoCodecs) == 0 {
		merged.VideoCodecs = append([]string(nil), cached.VideoCodecs...)
	}
	if len(merged.VideoCodecSignals) == 0 {
		merged.VideoCodecSignals = append([]capabilities.VideoCodecSignal(nil), cached.VideoCodecSignals...)
	}
	if len(merged.AudioCodecs) == 0 {
		merged.AudioCodecs = append([]string(nil), cached.AudioCodecs...)
	}
	if !merged.SupportsHLSExplicit && cached.SupportsHLSExplicit {
		merged.SupportsHLS = cached.SupportsHLS
		merged.SupportsHLSExplicit = true
	}
	if merged.DeviceType == "" {
		merged.DeviceType = cached.DeviceType
	}
	if merged.DeviceContext == nil {
		merged.DeviceContext = cloneDeviceContext(cached.DeviceContext)
	}
	if merged.NetworkContext == nil {
		merged.NetworkContext = cloneNetworkContext(cached.NetworkContext)
	}
	if len(merged.HLSEngines) == 0 {
		merged.HLSEngines = append([]string(nil), cached.HLSEngines...)
	}
	if merged.PreferredHLSEngine == "" {
		merged.PreferredHLSEngine = cached.PreferredHLSEngine
	}
	if !merged.RuntimeProbeUsed {
		merged.RuntimeProbeVersion = maxInt(merged.RuntimeProbeVersion, cached.RuntimeProbeVersion)
	}
	if merged.ClientFamilyFallback == "" {
		merged.ClientFamilyFallback = cached.ClientFamilyFallback
	}
	if merged.ClientCapsSource == "" {
		merged.ClientCapsSource = cached.ClientCapsSource
	}
	if merged.AllowTranscode == nil && cached.AllowTranscode != nil {
		v := *cached.AllowTranscode
		merged.AllowTranscode = &v
	}
	if merged.MaxVideo == nil && cached.MaxVideo != nil {
		merged.MaxVideo = &capabilities.MaxVideo{
			Width:  cached.MaxVideo.Width,
			Height: cached.MaxVideo.Height,
			Fps:    cached.MaxVideo.Fps,
		}
	}
	if merged.SupportsRange == nil && cached.SupportsRange != nil {
		v := *cached.SupportsRange
		merged.SupportsRange = &v
	}
	return capabilities.CanonicalizeCapabilities(merged)
}

func hostSnapshotForRequest(runtimeSnapshot playbackprofile.HostRuntimeSnapshot) capreg.HostSnapshot {
	hostname, _ := os.Hostname()
	osName, osVersion := resolveHostOSIdentity()
	return capreg.HostSnapshot{
		Identity: capreg.HostIdentity{
			Hostname:     hostname,
			OSName:       osName,
			OSVersion:    osVersion,
			Architecture: runtime.GOARCH,
		},
		Runtime:             runtimeSnapshot,
		EncoderCapabilities: hostEncoderCapabilities(),
		UpdatedAt:           time.Now().UTC(),
	}
}

func hostEncoderCapabilities() []capreg.EncoderCapability {
	encoders := []struct {
		codec   string
		encoder string
	}{
		{codec: "h264", encoder: "h264_vaapi"},
		{codec: "hevc", encoder: "hevc_vaapi"},
		{codec: "av1", encoder: "av1_vaapi"},
	}
	out := make([]capreg.EncoderCapability, 0, len(encoders))
	for _, entry := range encoders {
		capability, ok := hardware.VAAPIEncoderCapabilityFor(entry.encoder)
		if !ok {
			continue
		}
		out = append(out, capreg.EncoderCapability{
			Codec:          entry.codec,
			Verified:       capability.Verified,
			AutoEligible:   capability.AutoEligible,
			ProbeElapsedMS: capability.ProbeElapsed.Milliseconds(),
		})
	}
	return out
}

func cloneDeviceContext(in *capabilities.DeviceContext) *capabilities.DeviceContext {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneNetworkContext(in *capabilities.NetworkContext) *capabilities.NetworkContext {
	if in == nil {
		return nil
	}
	out := *in
	if in.Metered != nil {
		v := *in.Metered
		out.Metered = &v
	}
	if in.InternetValidated != nil {
		v := *in.InternetValidated
		out.InternetValidated = &v
	}
	return &out
}

func cloneReceiverContext(in *capreg.ReceiverContext) *capreg.ReceiverContext {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
