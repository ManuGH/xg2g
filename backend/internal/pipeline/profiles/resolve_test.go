package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
)

func TestResolve_AutoSafari(t *testing.T) {
	// Real UA from curl log
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	// Existing behavior (no cap, no GPU, auto hwaccel)
	spec := Resolve("", safariUA, 0, nil, GPUBackendNone, HWAccelAuto)
	assert.Equal(t, "safari", spec.Name)

	spec2 := Resolve("auto", safariUA, 0, nil, GPUBackendNone, HWAccelAuto)
	assert.Equal(t, "safari", spec2.Name)
}

func TestResolve_SmartScan(t *testing.T) {
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	// 1. Progressive -> Copy (Direct Remux)
	// Even if GPU is available, progressive should use Copy for efficiency/quality
	progCap := &scan.Capability{Interlaced: false}
	specProg := Resolve("auto", safariUA, 0, progCap, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, false, specProg.TranscodeVideo, "Progressive should assume safe for copy")
	assert.Equal(t, "mpegts", specProg.Container)
	assert.Equal(t, 192, specProg.AudioBitrateK, "Audio should be normalized for Safari")
	assert.Equal(t, ports.RuntimeModeCopy, specProg.PolicyModeHint)

	// 2. Interlaced + GPU -> Transcode VAAPI
	interCap := &scan.Capability{Interlaced: true}
	specGPU := Resolve("auto", safariUA, 0, interCap, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, true, specGPU.TranscodeVideo, "Interlaced should force transcode")
	assert.Equal(t, true, specGPU.Deinterlace)
	assert.Equal(t, "mpegts", specGPU.Container)
	assert.Equal(t, "vaapi", specGPU.HWAccel)
	assert.Equal(t, "h264", specGPU.VideoCodec)
	assert.Equal(t, 20, specGPU.VideoQP)
	assert.Equal(t, ports.RuntimeModeHQ25, specGPU.PolicyModeHint)

	// 3. Interlaced + No GPU -> Transcode CPU
	specCPU := Resolve("auto", safariUA, 0, interCap, GPUBackendNone, HWAccelAuto)
	assert.Equal(t, true, specCPU.TranscodeVideo)
	assert.Equal(t, true, specCPU.Deinterlace)
	assert.Equal(t, "mpegts", specCPU.Container)
	assert.Equal(t, "", specCPU.HWAccel)
	assert.Equal(t, "libx264", specCPU.VideoCodec)
	assert.Equal(t, "veryfast", specCPU.Preset)
	assert.Equal(t, 20, specCPU.VideoCRF)
	assert.Equal(t, ports.RuntimeModeHQ25, specCPU.PolicyModeHint)
}

func TestResolve_SafariWithoutUserAgentUsesFMP4ForNativeProgressiveClients(t *testing.T) {
	spec := Resolve("safari", "", 0, &scan.Capability{Interlaced: false}, GPUBackendNone, HWAccelAuto)

	assert.False(t, spec.TranscodeVideo)
	assert.Equal(t, "fmp4", spec.Container)
	assert.Equal(t, 192, spec.AudioBitrateK)
}

func TestResolve_UnknownCap(t *testing.T) {
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	// Unknown capability -> Conservative Interlaced assumption (Safety first)
	specUnknown := Resolve("auto", safariUA, 0, nil, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, true, specUnknown.TranscodeVideo)
	assert.Equal(t, true, specUnknown.Deinterlace)
}

func TestResolve_SafariDirtyExplicit(t *testing.T) {
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	specCPU := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendNone, HWAccelAuto)
	assert.Equal(t, "safari_dirty", specCPU.Name)
	assert.True(t, specCPU.TranscodeVideo)
	assert.True(t, specCPU.Deinterlace)
	assert.Equal(t, "mpegts", specCPU.Container)
	assert.Equal(t, "libx264", specCPU.VideoCodec)
	assert.Equal(t, "fast", specCPU.Preset)
	assert.Equal(t, 16, specCPU.VideoCRF)
	assert.Equal(t, 14000, specCPU.VideoMaxRateK)
	assert.Equal(t, 28000, specCPU.VideoBufSizeK)
	assert.Equal(t, ports.RuntimeModeSafe, specCPU.PolicyModeHint)

	specAutoGPU := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, "safari_dirty", specAutoGPU.Name)
	assert.Equal(t, "", specAutoGPU.HWAccel, "auto mode should stay on CPU unless safari_dirty GPU is explicitly enabled")
	assert.Equal(t, "libx264", specAutoGPU.VideoCodec)

	t.Setenv("XG2G_SAFARI_DIRTY_USE_GPU", "true")
	specGPUOptIn := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, "safari_dirty", specGPUOptIn.Name)
	assert.Equal(t, "vaapi", specGPUOptIn.HWAccel)
	assert.Equal(t, "h264", specGPUOptIn.VideoCodec)
	assert.Equal(t, 20, specGPUOptIn.VideoQP)
	assert.Equal(t, 20000, specGPUOptIn.VideoMaxRateK)
	assert.Equal(t, 40000, specGPUOptIn.VideoBufSizeK)

	specNative := Resolve("safari_dirty", "", 0, nil, GPUBackendNone, HWAccelAuto)
	assert.Equal(t, "fmp4", specNative.Container)
}

func TestResolve_SafariDirtyHWAccelModes(t *testing.T) {
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	t.Setenv("XG2G_SAFARI_DIRTY_HWACCEL_MODE", "encode_only")
	specEncodeOnly := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, "vaapi_encode_only", specEncodeOnly.HWAccel)
	assert.Equal(t, "h264", specEncodeOnly.VideoCodec)
	assert.Equal(t, 20, specEncodeOnly.VideoQP)
	assert.Equal(t, 20000, specEncodeOnly.VideoMaxRateK)
	assert.Equal(t, 40000, specEncodeOnly.VideoBufSizeK)

	t.Setenv("XG2G_SAFARI_DIRTY_HWACCEL_MODE", "full")
	specFull := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelAuto)
	assert.Equal(t, "vaapi", specFull.HWAccel)
	assert.Equal(t, "h264", specFull.VideoCodec)

	t.Setenv("XG2G_SAFARI_DIRTY_HWACCEL_MODE", "invalid")
	specInvalid := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelAuto)
	assert.Empty(t, specInvalid.HWAccel)
	assert.Equal(t, "libx264", specInvalid.VideoCodec)

	t.Setenv("XG2G_SAFARI_DIRTY_HWACCEL_MODE", "encode_only")
	specForced := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelForce)
	assert.Equal(t, "vaapi", specForced.HWAccel, "hwaccel=force should request the full VAAPI path")

	specDisabled := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelOff)
	assert.Empty(t, specDisabled.HWAccel, "hwaccel=off should disable safari_dirty GPU modes")
	assert.Equal(t, "libx264", specDisabled.VideoCodec)
}

func TestResolve_SafariDirtyEnvOverrides(t *testing.T) {
	t.Setenv("XG2G_SAFARI_DIRTY_CRF", "15")
	t.Setenv("XG2G_SAFARI_DIRTY_PRESET", "medium")
	t.Setenv("XG2G_SAFARI_DIRTY_VAAPI_QP", "19")
	t.Setenv("XG2G_SAFARI_DIRTY_MAXRATE_K", "18000")
	t.Setenv("XG2G_SAFARI_DIRTY_BUFSIZE_K", "36000")
	t.Setenv("XG2G_SAFARI_DIRTY_AUDIO_BITRATE_K", "224")

	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"
	spec := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendNone, HWAccelAuto)

	assert.Equal(t, 15, spec.VideoCRF)
	assert.Equal(t, "medium", spec.Preset)
	assert.Equal(t, 18000, spec.VideoMaxRateK)
	assert.Equal(t, 36000, spec.VideoBufSizeK)
	assert.Equal(t, 224, spec.AudioBitrateK)

	specGPU := Resolve("safari_dirty", safariUA, 0, nil, GPUBackendVAAPI, HWAccelForce)
	assert.Equal(t, "vaapi", specGPU.HWAccel)
	assert.Equal(t, 19, specGPU.VideoQP)
	assert.Equal(t, 18000, specGPU.VideoMaxRateK)
	assert.Equal(t, 36000, specGPU.VideoBufSizeK)
}

func TestResolve_LiveVideoLadderBridge(t *testing.T) {
	cases := []struct {
		name       string
		profile    string
		wantCRF    int
		wantPreset string
	}{
		{name: "safari cpu fallback keeps quality crf with live-safe preset", profile: ProfileSafari, wantCRF: 20, wantPreset: "veryfast"},
		{name: "safari_dvr uses compatible video ladder", profile: ProfileSafariDVR, wantCRF: 23, wantPreset: "fast"},
		{name: "dvr uses compatible video ladder", profile: ProfileDVR, wantCRF: 23, wantPreset: "fast"},
		{name: "repair uses repair video ladder", profile: ProfileRepair, wantCRF: 28, wantPreset: "veryfast"},
		{name: "h264_fmp4 cpu fallback uses repair video ladder", profile: ProfileH264FMP4, wantCRF: 28, wantPreset: "veryfast"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := Resolve(tc.profile, "Mozilla/5.0 Chrome/140.0.0.0 Safari/537.36", 0, &scan.Capability{Interlaced: true}, GPUBackendNone, HWAccelAuto)
			assert.True(t, spec.TranscodeVideo)
			assert.Equal(t, tc.wantCRF, spec.VideoCRF)
			assert.Equal(t, tc.wantPreset, spec.Preset)
		})
	}
}

func TestResolve_ProfileSafariCPUFallbackPresetCanBeOverridden(t *testing.T) {
	t.Setenv("XG2G_SAFARI_CPU_PRESET", "fast")

	spec := Resolve(ProfileSafari, "Mozilla/5.0 Version/17.0 Safari/605.1.15", 0, &scan.Capability{Interlaced: true}, GPUBackendNone, HWAccelAuto)
	assert.True(t, spec.TranscodeVideo)
	assert.Equal(t, 20, spec.VideoCRF)
	assert.Equal(t, "fast", spec.Preset)
}

func TestNormalizeRequestedProfileID_MapsPublicAliases(t *testing.T) {
	assert.Equal(t, ProfileHigh, NormalizeRequestedProfileID("compatible"))
	assert.Equal(t, ProfileHigh, NormalizeRequestedProfileID("quality"))
	assert.Equal(t, ProfileLow, NormalizeRequestedProfileID("bandwidth"))
	assert.Equal(t, ProfileCopy, NormalizeRequestedProfileID("direct"))
	assert.Equal(t, ProfileCopy, NormalizeRequestedProfileID("passthrough"))
	assert.Equal(t, ProfileRepair, NormalizeRequestedProfileID("repair"))
	assert.Equal(t, "generic", NormalizeRequestedProfileID("generic"))
}

func TestPublicProfileName_MapsLegacyInternalIDs(t *testing.T) {
	assert.Equal(t, PublicProfileCompatible, PublicProfileName(ProfileAuto))
	assert.Equal(t, PublicProfileCompatible, PublicProfileName(ProfileHigh))
	assert.Equal(t, PublicProfileBandwidth, PublicProfileName(ProfileLow))
	assert.Equal(t, PublicProfileCompatible, PublicProfileName(ProfileSafari))
	assert.Equal(t, PublicProfileRepair, PublicProfileName(ProfileSafariDirty))
	assert.Equal(t, PublicProfileQuality, PublicProfileName(ProfileSafariHEVCHW))
	assert.Equal(t, PublicProfileRepair, PublicProfileName(ProfileH264FMP4))
	assert.Equal(t, PublicProfileDirect, PublicProfileName(ProfileCopy))
	assert.Equal(t, PublicProfileRepair, PublicProfileName(ProfileRepair))
	assert.Equal(t, PublicProfileCompatible, PublicProfileName("auto"))
	assert.Equal(t, PublicProfileCompatible, PublicProfileName("universal"))
	assert.Equal(t, PublicProfileQuality, PublicProfileName("quality"))
	assert.Equal(t, PublicProfileRepair, PublicProfileName("repair"))
	assert.Equal(t, PublicProfileCompatible, PublicProfileName("generic"))
}
