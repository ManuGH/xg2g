package intents

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
)

func TestAdaptStartProfileForNetworkContext_LANBypass(t *testing.T) {
	intent := Intent{
		ClientCaps: &capabilities.PlaybackCapabilities{
			NetworkContext: &capabilities.NetworkContext{
				Kind:         "lan",
				DownlinkKbps: 2000,
			},
		},
	}
	spec := model.ProfileSpec{
		Name:          "high",
		VideoMaxRateK: 8000,
	}
	got := adaptStartProfileForNetworkContext(intent, spec)
	assert.Equal(t, 8000, got.VideoMaxRateK, "lan should bypass adaptation for non-low profile")
}

func TestAdaptStartProfileForNetworkContext_MeasuredPublicLink(t *testing.T) {
	intent := Intent{
		ClientCaps: &capabilities.PlaybackCapabilities{
			NetworkContext: &capabilities.NetworkContext{
				Kind:         "measured",
				DownlinkKbps: 2400,
			},
		},
	}
	spec := model.ProfileSpec{
		Name:          "bandwidth",
		VideoMaxRateK: 3000,
		AudioBitrateK: 160,
	}
	got := adaptStartProfileForNetworkContext(intent, spec)
	// 2400 * 0.75 = 1800 - 160 = 1640 kbps
	assert.Equal(t, 1640, got.VideoMaxRateK)
	assert.Equal(t, 3280, got.VideoBufSizeK)
	assert.Equal(t, 1280, got.VideoMaxWidth)
	assert.Equal(t, 26, got.VideoCRF)
	assert.True(t, got.TranscodeVideo)
}

func TestAdaptStartProfileForNetworkContext_VeryLowBandwidth(t *testing.T) {
	intent := Intent{
		ClientCaps: &capabilities.PlaybackCapabilities{
			NetworkContext: &capabilities.NetworkContext{
				Kind:         "measured",
				DownlinkKbps: 900,
			},
		},
	}
	spec := model.ProfileSpec{
		Name:          "bandwidth",
		VideoMaxRateK: 3000,
		AudioBitrateK: 160,
	}
	got := adaptStartProfileForNetworkContext(intent, spec)
	// 900 * 0.75 = 675 - 160 = 515 kbps
	assert.Equal(t, 515, got.VideoMaxRateK)
	assert.Equal(t, 1030, got.VideoBufSizeK)
	assert.Equal(t, 1280, got.VideoMaxWidth)
	assert.Equal(t, 28, got.VideoCRF)
}
