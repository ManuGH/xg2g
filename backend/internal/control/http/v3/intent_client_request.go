package v3

import (
	"encoding/json"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func hashV3Capabilities(caps *PlaybackCapabilities) string {
	if caps == nil {
		return ""
	}
	if capBytes, err := json.Marshal(caps); err == nil {
		var capMap map[string]any
		if err := json.Unmarshal(capBytes, &capMap); err == nil {
			if hash, err := normalize.MapHash(capMap); err == nil {
				return hash
			}
		}
	}
	return ""
}

func normalizeIntentClientCaps(caps *PlaybackCapabilities) *capabilities.PlaybackCapabilities {
	internal := mapV3CapsToInternal(caps)
	if internal == nil {
		return nil
	}
	canonical := capabilities.ResolveRuntimeProbeCapabilities(*internal)
	return &canonical
}

func normalizeIntentParams(params *map[string]string, clientCaps *capabilities.PlaybackCapabilities, capHash string) map[string]string {
	out := make(map[string]string)
	if params != nil {
		for key, value := range *params {
			out[key] = value
		}
	}
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
