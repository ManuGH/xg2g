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
	if caps == nil {
		return nil
	}
	var infoCaps v3playbackinfo.PlaybackCapabilities
	if b, err := json.Marshal(caps); err == nil {
		_ = json.Unmarshal(b, &infoCaps)
	}
	internal := v3playbackinfo.MapV3CapsToInternal(&infoCaps)
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
