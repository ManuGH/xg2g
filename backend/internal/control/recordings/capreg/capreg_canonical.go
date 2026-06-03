package capreg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func (s SourceSnapshot) Fingerprint() string {
	canonical := canonicalSourceSnapshot(s)
	if canonical.SubjectKind == "" && canonical.Container == "" && canonical.VideoCodec == "" && canonical.AudioCodec == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"subjectKind":       canonical.SubjectKind,
		"origin":            canonical.Origin,
		"container":         canonical.Container,
		"videoCodec":        canonical.VideoCodec,
		"audioCodec":        canonical.AudioCodec,
		"bitrateConfidence": canonical.BitrateConfidence,
		"bitrateBucket":     canonical.BitrateBucket,
		"width":             canonical.Width,
		"height":            canonical.Height,
		"fps":               canonical.FPS,
		"signalFps":         canonical.SignalFPS,
		"interlaced":        canonical.Interlaced,
		"problemFlags":      canonical.ProblemFlags,
		"receiver":          canonical.ReceiverContext,
	})
}

func (s HostSnapshot) DecisionFingerprint() string {
	canonical := canonicalHostSnapshot(s)
	if canonical.Identity.OSName == "" && canonical.Identity.OSVersion == "" && canonical.Identity.Architecture == "" && len(canonical.EncoderCapabilities) == 0 {
		return ""
	}
	keys := make([]decisionFingerprintEncoderKey, 0, len(canonical.EncoderCapabilities))
	seen := make(map[string]struct{}, len(canonical.EncoderCapabilities))
	for _, capability := range canonical.EncoderCapabilities {
		codec := normalizeToken(capability.Codec)
		if codec == "" {
			continue
		}
		if _, ok := seen[codec]; ok {
			continue
		}
		seen[codec] = struct{}{}
		keys = append(keys, decisionFingerprintEncoderKey{Codec: codec})
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Codec < keys[j].Codec
	})
	profileKeys := make([]decisionFingerprintProfileKey, 0, len(canonical.Runtime.Benchmark.Profiles))
	for _, benchmark := range canonical.Runtime.Benchmark.Profiles {
		if benchmark.ProfileID == "" {
			continue
		}
		profileKeys = append(profileKeys, decisionFingerprintProfileKey{
			ProfileID: benchmark.ProfileID,
			Class:     benchmark.Class,
		})
	}
	sort.Slice(profileKeys, func(i, j int) bool {
		return profileKeys[i].ProfileID < profileKeys[j].ProfileID
	})
	return "df4:" + sha256JSON(decisionFingerprintInput{
		Version:        "df4",
		HostClass:      canonical.Runtime.PerformanceClass,
		BenchmarkClass: playbackprofile.BenchmarkClassForCodec(canonical.Runtime.Benchmark, "h264"),
		ProfileKeys:    profileKeys,
		OSName:         canonical.Identity.OSName,
		OSVersion:      canonical.Identity.OSVersion,
		Architecture:   canonical.Identity.Architecture,
		EncoderKeys:    keys,
	})
}

func canonicalDeviceIdentity(in DeviceIdentity) DeviceIdentity {
	out := in
	out.ClientFamily = normalizeToken(out.ClientFamily)
	out.ClientCapsSource = normalizeToken(out.ClientCapsSource)
	out.DeviceType = normalizeToken(out.DeviceType)
	out.DeviceContext = canonicalDeviceContext(out.DeviceContext)
	return out
}

func (i DeviceIdentity) Fingerprint() string {
	canonical := canonicalDeviceIdentity(i)
	if canonical.ClientFamily == "" && canonical.ClientCapsSource == "" && canonical.DeviceType == "" && canonical.DeviceContext == nil {
		return ""
	}
	return sha256JSON(map[string]any{
		"clientFamily":     canonical.ClientFamily,
		"clientCapsSource": canonical.ClientCapsSource,
		"deviceType":       canonical.DeviceType,
		"deviceContext":    canonical.DeviceContext,
	})
}

func canonicalHostIdentity(in HostIdentity) HostIdentity {
	out := in
	out.Hostname = normalizeToken(out.Hostname)
	out.OSName = normalizeToken(out.OSName)
	out.OSVersion = strings.TrimSpace(strings.ToLower(out.OSVersion))
	out.Architecture = normalizeToken(out.Architecture)
	return out
}

func (i HostIdentity) Fingerprint() string {
	canonical := canonicalHostIdentity(i)
	if canonical.Hostname == "" && canonical.OSName == "" && canonical.OSVersion == "" && canonical.Architecture == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"hostname":     canonical.Hostname,
		"osName":       canonical.OSName,
		"osVersion":    canonical.OSVersion,
		"architecture": canonical.Architecture,
	})
}

func canonicalDeviceSnapshot(in DeviceSnapshot) DeviceSnapshot {
	out := in
	out.Identity = canonicalDeviceIdentity(out.Identity)
	out.Capabilities = capabilities.CanonicalizeCapabilities(out.Capabilities)
	out.Network = canonicalNetworkContext(out.Network)
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	return out
}

func canonicalHostSnapshot(in HostSnapshot) HostSnapshot {
	out := in
	out.Identity = canonicalHostIdentity(out.Identity)
	out.Runtime = playbackprofile.CanonicalizeHostRuntime(out.Runtime)
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	if out.EncoderCapabilities == nil {
		out.EncoderCapabilities = []EncoderCapability{}
	}
	for idx := range out.EncoderCapabilities {
		out.EncoderCapabilities[idx].Codec = normalizeToken(out.EncoderCapabilities[idx].Codec)
		if out.EncoderCapabilities[idx].ProbeElapsedMS < 0 {
			out.EncoderCapabilities[idx].ProbeElapsedMS = 0
		}
	}
	return out
}

func canonicalSourceSnapshot(in SourceSnapshot) SourceSnapshot {
	out := in
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.Origin = normalizeToken(out.Origin)
	out.Container = normalizeSourceContainer(out.Container)
	out.VideoCodec = normalizeToken(out.VideoCodec)
	out.AudioCodec = normalizeToken(out.AudioCodec)
	out.BitrateConfidence = normalizeToken(out.BitrateConfidence)
	out.BitrateBucket = normalizeToken(out.BitrateBucket)
	if out.Width < 0 {
		out.Width = 0
	}
	if out.Height < 0 {
		out.Height = 0
	}
	if out.FPS < 0 {
		out.FPS = 0
	}
	if out.SignalFPS < 0 {
		out.SignalFPS = 0
	}
	out.ProblemFlags = canonicalStringSlice(out.ProblemFlags)
	out.ReceiverContext = canonicalReceiverContext(out.ReceiverContext)
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	return out
}

func HashCapabilitiesSnapshot(in capabilities.PlaybackCapabilities) string {
	canonical := capabilities.CanonicalizeCapabilities(in)
	canonical.NetworkContext = nil
	return sha256JSON(canonical)
}

func canonicalObservation(in PlaybackObservation) PlaybackObservation {
	out := in
	if out.ObservedAt.IsZero() {
		out.ObservedAt = time.Now().UTC()
	} else {
		out.ObservedAt = out.ObservedAt.UTC()
	}
	out.RequestID = strings.TrimSpace(out.RequestID)
	out.ObservationKind = normalizeToken(out.ObservationKind)
	if out.ObservationKind == "" {
		out.ObservationKind = "decision"
	}
	out.Outcome = normalizeToken(out.Outcome)
	if out.Outcome == "" {
		out.Outcome = "predicted"
	}
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.SourceRef = strings.TrimSpace(out.SourceRef)
	out.SourceFingerprint = strings.TrimSpace(out.SourceFingerprint)
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.RequestedIntent = normalizeToken(out.RequestedIntent)
	out.ResolvedIntent = normalizeToken(out.ResolvedIntent)
	out.Mode = normalizeToken(out.Mode)
	out.SelectedContainer = normalizeToken(out.SelectedContainer)
	out.SelectedVideoCodec = normalizeToken(out.SelectedVideoCodec)
	out.SelectedAudioCodec = normalizeToken(out.SelectedAudioCodec)
	if out.SourceWidth < 0 {
		out.SourceWidth = 0
	}
	if out.SourceHeight < 0 {
		out.SourceHeight = 0
	}
	if out.SourceFPS < 0 {
		out.SourceFPS = 0
	}
	out.HostFingerprint = strings.TrimSpace(out.HostFingerprint)
	out.DeviceFingerprint = strings.TrimSpace(out.DeviceFingerprint)
	out.ClientCapsHash = strings.TrimSpace(out.ClientCapsHash)
	out.Network = canonicalNetworkContext(out.Network)
	out.FeedbackEvent = normalizeToken(out.FeedbackEvent)
	if out.FeedbackCode < 0 {
		out.FeedbackCode = 0
	}
	out.FeedbackMessage = strings.TrimSpace(out.FeedbackMessage)
	return out
}

func canonicalFeedbackSummaryQuery(in FeedbackSummaryQuery) FeedbackSummaryQuery {
	out := in
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.SourceFingerprint = strings.TrimSpace(out.SourceFingerprint)
	out.DeviceFingerprint = strings.TrimSpace(out.DeviceFingerprint)
	out.HostFingerprint = strings.TrimSpace(out.HostFingerprint)
	if out.Since.IsZero() {
		out.Since = time.Time{}
	} else {
		out.Since = out.Since.UTC()
	}
	if out.Limit <= 0 {
		out.Limit = 8
	}
	if out.Limit > 32 {
		out.Limit = 32
	}
	return out
}

func canonicalPlaybackPolicyStateQuery(in PlaybackPolicyStateQuery) PlaybackPolicyStateQuery {
	out := in
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.SourceFingerprint = strings.TrimSpace(out.SourceFingerprint)
	out.DeviceFingerprint = strings.TrimSpace(out.DeviceFingerprint)
	out.HostFingerprint = strings.TrimSpace(out.HostFingerprint)
	return out
}

func (s PlaybackPolicyState) Fingerprint() string {
	return queryFingerprint(canonicalPlaybackPolicyStateQuery(PlaybackPolicyStateQuery{
		SubjectKind:       s.SubjectKind,
		SourceFingerprint: s.SourceFingerprint,
		DeviceFingerprint: s.DeviceFingerprint,
		HostFingerprint:   s.HostFingerprint,
	}))
}

func canonicalPlaybackPolicyState(in PlaybackPolicyState) PlaybackPolicyState {
	out := in
	query := canonicalPlaybackPolicyStateQuery(PlaybackPolicyStateQuery{
		SubjectKind:       out.SubjectKind,
		SourceFingerprint: out.SourceFingerprint,
		DeviceFingerprint: out.DeviceFingerprint,
		HostFingerprint:   out.HostFingerprint,
	})
	out.SubjectKind = query.SubjectKind
	out.SourceFingerprint = query.SourceFingerprint
	out.DeviceFingerprint = query.DeviceFingerprint
	out.HostFingerprint = query.HostFingerprint
	out.MaxQualityRung = playbackprofile.NormalizeQualityRung(string(out.MaxQualityRung))
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	return out
}

func queryFingerprint(query PlaybackPolicyStateQuery) string {
	if query.SubjectKind == "" || query.SourceFingerprint == "" || query.DeviceFingerprint == "" || query.HostFingerprint == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"subjectKind":       query.SubjectKind,
		"sourceFingerprint": query.SourceFingerprint,
		"deviceFingerprint": query.DeviceFingerprint,
		"hostFingerprint":   query.HostFingerprint,
	})
}

func canonicalDeviceContext(in *capabilities.DeviceContext) *capabilities.DeviceContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Brand = normalizeToken(out.Brand)
	out.Product = normalizeToken(out.Product)
	out.Device = normalizeToken(out.Device)
	out.Platform = normalizeToken(out.Platform)
	out.Manufacturer = normalizeToken(out.Manufacturer)
	out.Model = normalizeToken(out.Model)
	out.OSName = normalizeToken(out.OSName)
	out.OSVersion = strings.TrimSpace(strings.ToLower(out.OSVersion))
	if out.SDKInt < 0 {
		out.SDKInt = 0
	}
	if out.Brand == "" && out.Product == "" && out.Device == "" && out.Platform == "" && out.Manufacturer == "" && out.Model == "" && out.OSName == "" && out.OSVersion == "" && out.SDKInt == 0 {
		return nil
	}
	return &out
}

func canonicalReceiverContext(in *ReceiverContext) *ReceiverContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Platform = normalizeToken(out.Platform)
	out.Brand = normalizeToken(out.Brand)
	out.Model = normalizeToken(out.Model)
	out.OSName = normalizeToken(out.OSName)
	out.OSVersion = strings.TrimSpace(out.OSVersion)
	out.KernelVersion = strings.TrimSpace(out.KernelVersion)
	out.EnigmaVersion = strings.TrimSpace(out.EnigmaVersion)
	out.WebInterfaceVersion = strings.TrimSpace(out.WebInterfaceVersion)
	if out.Platform == "" && out.Brand == "" && out.Model == "" && out.OSName == "" && out.OSVersion == "" && out.KernelVersion == "" && out.EnigmaVersion == "" && out.WebInterfaceVersion == "" {
		return nil
	}
	return &out
}

func canonicalNetworkContext(in *capabilities.NetworkContext) *capabilities.NetworkContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Kind = normalizeToken(out.Kind)
	if out.DownlinkKbps < 0 {
		out.DownlinkKbps = 0
	}
	if out.Kind == "" && out.DownlinkKbps == 0 && out.Metered == nil && out.InternetValidated == nil {
		return nil
	}
	return &out
}

func normalizeToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeSourceContainer(s string) string {
	switch normalizeToken(s) {
	case "mpegts":
		return "ts"
	default:
		return normalizeToken(s)
	}
}

func canonicalStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		token := normalizeToken(raw)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func sha256JSON(v any) string {
	payload, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
