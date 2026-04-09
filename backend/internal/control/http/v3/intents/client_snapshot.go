package intents

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func normalizedClientCaps(in *capabilities.PlaybackCapabilities) *capabilities.PlaybackCapabilities {
	if in == nil {
		return nil
	}
	out := capabilities.ResolveRuntimeProbeCapabilities(*in)
	return &out
}

func clientFamilyForIntent(intent Intent) string {
	if clientFamily := normalize.Token(intent.Params[model.CtxKeyClientFamily]); clientFamily != "" {
		return clientFamily
	}
	if intent.ClientCaps != nil {
		return normalize.Token(intent.ClientCaps.ClientFamilyFallback)
	}
	return ""
}

func preferredEngineForIntent(intent Intent) string {
	if preferredEngine := normalize.Token(intent.Params[model.CtxKeyPreferredEngine]); preferredEngine != "" {
		return preferredEngine
	}
	if intent.ClientCaps != nil {
		return normalize.Token(intent.ClientCaps.PreferredHLSEngine)
	}
	return ""
}

func deviceTypeForIntent(intent Intent) string {
	if deviceType := normalize.Token(intent.Params[model.CtxKeyDeviceType]); deviceType != "" {
		return deviceType
	}
	if intent.ClientCaps != nil {
		return normalize.Token(intent.ClientCaps.DeviceType)
	}
	return ""
}

func clientCapHashForIntent(intent Intent) string {
	if capHash := normalize.Token(intent.ClientCapHash); capHash != "" {
		return capHash
	}
	if capHash := normalize.Token(intent.Params["capHash"]); capHash != "" {
		return capHash
	}
	return normalize.Token(intent.Params["cap_hash"])
}

func buildStartClientSnapshot(intent Intent, capturedAt time.Time) *model.PlaybackClientSnapshot {
	canonicalCaps := normalizedClientCaps(intent.ClientCaps)
	snapshot := &model.PlaybackClientSnapshot{
		CapturedAtUnix:     capturedAt.Unix(),
		CapHash:            clientCapHashForIntent(intent),
		ClientFamily:       clientFamilyForIntent(intent),
		PreferredHLSEngine: preferredEngineForIntent(intent),
		DeviceType:         deviceTypeForIntent(intent),
	}

	if canonicalCaps != nil {
		if snapshot.ClientFamily == "" {
			snapshot.ClientFamily = strings.TrimSpace(canonicalCaps.ClientFamilyFallback)
		}
		if snapshot.PreferredHLSEngine == "" {
			snapshot.PreferredHLSEngine = strings.TrimSpace(canonicalCaps.PreferredHLSEngine)
		}
		if snapshot.DeviceType == "" {
			snapshot.DeviceType = strings.TrimSpace(canonicalCaps.DeviceType)
		}
		snapshot.ClientCapsSource = strings.TrimSpace(canonicalCaps.ClientCapsSource)
		snapshot.RuntimeProbeUsed = canonicalCaps.RuntimeProbeUsed
		snapshot.RuntimeProbeVersion = canonicalCaps.RuntimeProbeVersion
		snapshot.DeviceContext = cloneClientDeviceContext(canonicalCaps.DeviceContext)
		snapshot.NetworkContext = cloneClientNetworkContext(canonicalCaps.NetworkContext)
	}

	if snapshotIsEmpty(snapshot) {
		return nil
	}
	return snapshot
}

func snapshotIsEmpty(snapshot *model.PlaybackClientSnapshot) bool {
	if snapshot == nil {
		return true
	}
	return snapshot.CapHash == "" &&
		snapshot.ClientCapsSource == "" &&
		snapshot.ClientFamily == "" &&
		snapshot.PreferredHLSEngine == "" &&
		snapshot.DeviceType == "" &&
		!snapshot.RuntimeProbeUsed &&
		snapshot.RuntimeProbeVersion == 0 &&
		snapshot.DeviceContext == nil &&
		snapshot.NetworkContext == nil
}

func cloneClientDeviceContext(in *capabilities.DeviceContext) *model.PlaybackClientDeviceContext {
	if in == nil {
		return nil
	}
	return &model.PlaybackClientDeviceContext{
		Brand:        strings.TrimSpace(in.Brand),
		Device:       strings.TrimSpace(in.Device),
		Manufacturer: strings.TrimSpace(in.Manufacturer),
		Model:        strings.TrimSpace(in.Model),
		OSName:       strings.TrimSpace(in.OSName),
		OSVersion:    strings.TrimSpace(in.OSVersion),
		Platform:     strings.TrimSpace(in.Platform),
		Product:      strings.TrimSpace(in.Product),
		SDKInt:       in.SDKInt,
	}
}

func cloneClientNetworkContext(in *capabilities.NetworkContext) *model.PlaybackClientNetworkContext {
	if in == nil {
		return nil
	}
	out := &model.PlaybackClientNetworkContext{
		DownlinkKbps: in.DownlinkKbps,
		Kind:         strings.TrimSpace(in.Kind),
	}
	if in.InternetValidated != nil {
		v := *in.InternetValidated
		out.InternetValidated = &v
	}
	if in.Metered != nil {
		v := *in.Metered
		out.Metered = &v
	}
	return out
}
