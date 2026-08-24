// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackplanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVetoedCapability_DolbyForBrowsers(t *testing.T) {
	browser := ClientEvidence{Family: "safari_native", PreferredEngine: "hlsjs"}
	nativeApp := ClientEvidence{Family: "android_exoplayer", PreferredEngine: "exoplayer"}

	for _, codec := range []string{"ac3", "AC-3", "eac3", "ec-3", " ac3 "} {
		reason, vetoed := VetoedCapability("audio", codec, browser)
		assert.True(t, vetoed, "codec %q must be vetoed for browsers", codec)
		assert.Equal(t, ReasonBrowserCannotDecodeDolby, reason)
	}

	_, vetoed := VetoedCapability("audio", "ac3", nativeApp)
	assert.False(t, vetoed, "native app players keep their capability-driven Dolby copy")

	_, vetoed = VetoedCapability("audio", "aac", browser)
	assert.False(t, vetoed, "AAC is never vetoed")

	_, vetoed = VetoedCapability("video", "ac3", browser)
	assert.False(t, vetoed, "vetoes are scoped to their track kind")

	_, vetoed = VetoedCapability("audio", "", browser)
	assert.False(t, vetoed, "empty codec is never vetoed")
}

func TestIsBrowserClient(t *testing.T) {
	assert.True(t, IsBrowserClient(ClientEvidence{PreferredEngine: "hlsjs"}))
	assert.True(t, IsBrowserClient(ClientEvidence{Family: "safari_native"}))
	assert.True(t, IsBrowserClient(ClientEvidence{Family: "Chromium_HLSJS"}))
	assert.True(t, IsBrowserClient(ClientEvidence{Family: "firefox"}))
	assert.True(t, IsBrowserClient(ClientEvidence{Family: "ios_safari"}))
	assert.False(t, IsBrowserClient(ClientEvidence{Family: "android_exoplayer", PreferredEngine: "exoplayer"}))
	assert.False(t, IsBrowserClient(ClientEvidence{Family: "ios_native", PreferredEngine: "native_app"}))
	assert.False(t, IsBrowserClient(ClientEvidence{}))
}

func TestVetoedCapability_MP2ForAndroidNative(t *testing.T) {
	for _, family := range []string{"android_native", "android_tv_native", "android_exoplayer"} {
		reason, vetoed := VetoedCapability("audio", "mp2", ClientEvidence{Family: family})
		assert.True(t, vetoed, "MP2 must be vetoed for %s", family)
		assert.Equal(t, ReasonAndroidNativeCannotDecodeMP2, reason)
	}

	_, vetoed := VetoedCapability("audio", "mp2", ClientEvidence{Family: "ios_native"})
	assert.False(t, vetoed)
}
