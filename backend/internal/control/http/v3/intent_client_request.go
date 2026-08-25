package v3

import (
	"encoding/json"
	"maps"

	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func hashV3Capabilities(caps *PlaybackCapabilities) string {
	if caps == nil {
		return ""
	}
	var infoCaps v3playbackinfo.PlaybackCapabilities
	if b, err := json.Marshal(caps); err == nil {
		if err := json.Unmarshal(b, &infoCaps); err == nil {
			return v3playbackinfo.HashV3Capabilities(&infoCaps)
		}
	}
	return ""
}

func normalizeIntentClientCaps(caps *PlaybackCapabilities, userAgent string) *capabilities.PlaybackCapabilities {
	if caps == nil {
		return nil
	}
	var infoCaps v3playbackinfo.PlaybackCapabilities
	if b, err := json.Marshal(caps); err == nil {
		_ = json.Unmarshal(b, &infoCaps)
	}
	internal := v3playbackinfo.MapV3CapsToInternal(&infoCaps, userAgent)
	if internal == nil {
		return nil
	}
	canonical := capabilities.ResolveRuntimeProbeCapabilities(*internal)
	return &canonical
}

func normalizeIntentParams(params *map[string]string, clientCaps *capabilities.PlaybackCapabilities, capHash string) map[string]string {
	out := make(map[string]string)
	if params != nil {
		maps.Copy(out, *params)
	}
	// Client family and device type are server conclusions, so a value a client
	// put in `params` cannot stand: it used to, and a client that named a family
	// there chose its own playback policy. They are overwritten, not merged.
	delete(out, model.CtxKeyClientFamily)
	delete(out, model.CtxKeyDeviceType)

	if clientCaps != nil {
		if clientFamily := normalize.Token(clientCaps.ClientFamilyFallback); clientFamily != "" {
			out[model.CtxKeyClientFamily] = clientFamily
		}
		if preferredEngine := normalize.Token(clientCaps.PreferredHLSEngine); preferredEngine != "" {
			out[model.CtxKeyPreferredEngine] = preferredEngine
		}
		if deviceType := normalize.Token(clientCaps.DeviceType); deviceType != "" {
			out[model.CtxKeyDeviceType] = deviceType
		}
	}
	if capHash != "" {
		out["capHash"] = capHash
	}
	return out
}
