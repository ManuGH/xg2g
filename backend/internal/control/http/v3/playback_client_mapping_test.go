package v3

import (
	"encoding/json"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapPlaybackClientSnapshot_ExposesStableDetailedWhitelist(t *testing.T) {
	metered := false
	internetValidated := true

	dto := mapPlaybackClientSnapshot(&model.PlaybackClientSnapshot{
		CapturedAtUnix:      1700000000,
		CapHash:             "cap-hash-1",
		ClientCapsSource:    "runtime_plus_family",
		ClientFamily:        "chromium_hlsjs",
		PreferredHLSEngine:  "hlsjs",
		DeviceType:          "web",
		RuntimeProbeUsed:    true,
		RuntimeProbeVersion: 2,
		DeviceContext: &model.PlaybackClientDeviceContext{
			Brand:        "apple",
			Product:      "mac14,5",
			Device:       "desktop",
			Platform:     "browser",
			Manufacturer: "apple",
			Model:        "macbookpro",
			OSName:       "macos",
			OSVersion:    "15.4",
			SDKInt:       14,
		},
		NetworkContext: &model.PlaybackClientNetworkContext{
			Kind:              "wifi",
			DownlinkKbps:      54000,
			Metered:           &metered,
			InternetValidated: &internetValidated,
		},
	})
	require.NotNil(t, dto)

	raw, err := json.Marshal(dto)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.ElementsMatch(t, []string{
		"capturedAtMs",
		"capHash",
		"clientCapsSource",
		"clientFamily",
		"preferredHlsEngine",
		"deviceType",
		"runtimeProbeUsed",
		"runtimeProbeVersion",
		"deviceContext",
		"networkContext",
	}, jsonKeys(payload))

	deviceContext, ok := payload["deviceContext"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{
		"brand",
		"product",
		"device",
		"platform",
		"manufacturer",
		"model",
		"osName",
		"osVersion",
		"sdkInt",
	}, jsonKeys(deviceContext))

	networkContext, ok := payload["networkContext"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{
		"kind",
		"downlinkKbps",
		"metered",
		"internetValidated",
	}, jsonKeys(networkContext))
}

func TestMapPlaybackClientSummary_ExposesCompactStableWhitelist(t *testing.T) {
	metered := false
	internetValidated := true

	dto := mapPlaybackClientSummary(&model.PlaybackClientSnapshot{
		CapturedAtUnix:      1700000000,
		CapHash:             "cap-hash-1",
		ClientCapsSource:    "runtime_plus_family",
		ClientFamily:        "chromium_hlsjs",
		PreferredHLSEngine:  "hlsjs",
		DeviceType:          "web",
		RuntimeProbeUsed:    true,
		RuntimeProbeVersion: 2,
		DeviceContext: &model.PlaybackClientDeviceContext{
			Brand:        "apple",
			Product:      "mac14,5",
			Device:       "desktop",
			Platform:     "browser",
			Manufacturer: "apple",
			Model:        "macbookpro",
			OSName:       "macos",
			OSVersion:    "15.4",
			SDKInt:       14,
		},
		NetworkContext: &model.PlaybackClientNetworkContext{
			Kind:              "wifi",
			DownlinkKbps:      54000,
			Metered:           &metered,
			InternetValidated: &internetValidated,
		},
	})
	require.NotNil(t, dto)

	raw, err := json.Marshal(dto)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.ElementsMatch(t, []string{
		"capHash",
		"clientCapsSource",
		"clientFamily",
		"preferredHlsEngine",
		"deviceType",
		"platform",
		"osName",
		"osVersion",
		"model",
		"networkKind",
		"runtimeProbeVersion",
	}, jsonKeys(payload))

	assert.NotContains(t, payload, "capturedAtMs")
	assert.NotContains(t, payload, "runtimeProbeUsed")
	assert.NotContains(t, payload, "deviceContext")
	assert.NotContains(t, payload, "networkContext")
	assert.NotContains(t, payload, "downlinkKbps")
	assert.NotContains(t, payload, "metered")
	assert.NotContains(t, payload, "internetValidated")
}

func jsonKeys(in map[string]any) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	return keys
}
