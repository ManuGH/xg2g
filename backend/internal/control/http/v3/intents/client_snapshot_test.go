package intents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStartClientSnapshot_PersistsOnlyWhitelistedFields(t *testing.T) {
	metered := false
	internetValidated := true

	snapshot := buildStartClientSnapshot(Intent{
		Params: map[string]string{
			model.CtxKeyClientFamily:    "chromium_hlsjs",
			model.CtxKeyPreferredEngine: "hlsjs",
			model.CtxKeyDeviceType:      "web",
		},
		ClientCaps: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"ts"},
			VideoCodecs:         []string{"h264", "hevc"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "h264", Supported: true},
			},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			HLSEngines:           []string{"native", "hlsjs"},
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			AllowTranscode:       boolPtr(false),
			SupportsRange:        boolPtr(true),
			MaxVideo: &capabilities.MaxVideo{
				Width:  1920,
				Height: 1080,
				Fps:    60,
			},
			DeviceType: "web",
			DeviceContext: &capabilities.DeviceContext{
				Brand:          "Apple",
				Product:        "Mac14,5",
				Device:         "desktop",
				Platform:       "browser",
				Manufacturer:   "Apple",
				Model:          "MacBookPro",
				BrowserName:    "Safari",
				BrowserVersion: "17.4",
				OSName:         "macos",
				OSVersion:      "15.4",
				PlatformClass:  "macos_safari",
				SDKInt:         0,
			},
			NetworkContext: &capabilities.NetworkContext{
				Kind:              "wifi",
				DownlinkKbps:      54000,
				Metered:           &metered,
				InternetValidated: &internetValidated,
			},
		},
		ClientCapHash: "cap-hash-1",
	}, time.Unix(1700000000, 0))

	require.NotNil(t, snapshot)

	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.ElementsMatch(t, []string{
		"capturedAtUnix",
		"capHash",
		"clientCapsSource",
		"clientFamily",
		"preferredHlsEngine",
		"deviceType",
		"runtimeProbeUsed",
		"runtimeProbeVersion",
		"deviceContext",
		"networkContext",
	}, jsonMapKeys(payload))

	deviceContext, ok := payload["deviceContext"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{
		"brand",
		"product",
		"device",
		"platform",
		"manufacturer",
		"model",
		"browserName",
		"browserVersion",
		"osName",
		"osVersion",
		"platformClass",
	}, jsonMapKeys(deviceContext))

	networkContext, ok := payload["networkContext"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{
		"kind",
		"downlinkKbps",
		"metered",
		"internetValidated",
	}, jsonMapKeys(networkContext))
}

func TestBuildStartClientSnapshot_DoesNotPersistRawCapabilityPayload(t *testing.T) {
	snapshot := buildStartClientSnapshot(Intent{
		ClientCaps: &capabilities.PlaybackCapabilities{
			CapabilitiesVersion: 3,
			Containers:          []string{"ts"},
			VideoCodecs:         []string{"h264", "hevc"},
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "h264", Supported: true},
			},
			AudioCodecs:          []string{"aac"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			HLSEngines:           []string{"native", "hlsjs"},
			PreferredHLSEngine:   "hlsjs",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "chromium_hlsjs",
			AllowTranscode:       boolPtr(false),
			SupportsRange:        boolPtr(true),
			MaxVideo: &capabilities.MaxVideo{
				Width:  1920,
				Height: 1080,
				Fps:    60,
			},
			DeviceType: "web",
		},
		ClientCapHash: "cap-hash-1",
	}, time.Unix(1700000000, 0))

	require.NotNil(t, snapshot)

	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	jsonBody := string(raw)

	assert.NotContains(t, jsonBody, `"containers"`)
	assert.NotContains(t, jsonBody, `"container"`)
	assert.NotContains(t, jsonBody, `"videoCodecs"`)
	assert.NotContains(t, jsonBody, `"videoCodecSignals"`)
	assert.NotContains(t, jsonBody, `"audioCodecs"`)
	assert.NotContains(t, jsonBody, `"supportsHls"`)
	assert.NotContains(t, jsonBody, `"supportsRange"`)
	assert.NotContains(t, jsonBody, `"allowTranscode"`)
	assert.NotContains(t, jsonBody, `"maxVideo"`)
	assert.NotContains(t, jsonBody, `"hlsEngines"`)
}

func jsonMapKeys(in map[string]any) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	return keys
}

func boolPtr(v bool) *bool {
	return &v
}
