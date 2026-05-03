package autocodec

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestClientAV1PlaybackAllowed_PlatformMatrix(t *testing.T) {
	smooth := true
	powerEfficient := true
	supportedOnly := false

	base := func() capabilities.PlaybackCapabilities {
		return capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			AudioCodecs:          []string{"aac"},
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			ClientFamilyFallback: playbackprofile.ClientChromiumHLSJS,
			VideoCodecSignals: []capabilities.VideoCodecSignal{
				{Codec: "av1", Supported: true, Smooth: &smooth, PowerEfficient: &powerEfficient},
			},
		}
	}

	tests := []struct {
		name   string
		family string
		mutate func(*capabilities.PlaybackCapabilities)
		want   bool
	}{
		{
			name:   "iphone a17 pro on ios 17 is allowed",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 Pro A17 Pro"}
			},
			want: true,
		},
		{
			name:   "iphone a16 stays off even when runtime reports generic av1",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "18.1", Model: "iPhone 14 Pro A16"}
			},
			want: false,
		},
		{
			name:   "iphone 15 non-pro stays off even on ios 17",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone 15 A16"}
			},
			want: false,
		},
		{
			name:   "iphone 15 pro max hw machine identifier is allowed",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "17.5", Model: "iPhone16,2"}
			},
			want: true,
		},
		{
			name:   "iphone 16e is allowed",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "18.3", Model: "iPhone 16e A18"}
			},
			want: true,
		},
		{
			name:   "iphone air is allowed",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "26.0", Model: "iPhone Air A19 Pro"}
			},
			want: true,
		},
		{
			name:   "iphone a17 pro below ios 17 stays off",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "iphone"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ios", OSVersion: "16.7", Model: "iPhone 15 Pro A17 Pro"}
			},
			want: false,
		},
		{
			name:   "ipad m4 on ipados is allowed",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "ipad"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ipados", OSVersion: "18.0", Model: "iPad Pro M4"}
			},
			want: true,
		},
		{
			name:   "ipad air 13 m3 is allowed",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "ipad"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ipados", OSVersion: "18.4", Model: "iPad Air 13-inch M3"}
			},
			want: true,
		},
		{
			name:   "ipad a16 stays off",
			family: playbackprofile.ClientIOSSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "ipad"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "ipados", OSVersion: "18.4", Model: "iPad A16"}
			},
			want: false,
		},
		{
			name:   "mac m3 on macos 14 is allowed",
			family: playbackprofile.ClientSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "macos", OSVersion: "14.4", Model: "MacBook Air M3"}
			},
			want: true,
		},
		{
			name:   "mac studio m3 ultra is allowed",
			family: playbackprofile.ClientSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "macos", OSVersion: "15.4", Model: "Mac Studio M3 Ultra"}
			},
			want: true,
		},
		{
			name:   "mac m2 stays off despite runtime av1",
			family: playbackprofile.ClientSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "macos", OSVersion: "14.4", Model: "MacBook Pro M2"}
			},
			want: false,
		},
		{
			name:   "mac m3 below macos 14 stays off",
			family: playbackprofile.ClientSafariNative,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "macos", OSVersion: "13.6", Model: "MacBook Air M3"}
			},
			want: false,
		},
		{
			name: "android 14 allows smooth runtime av1",
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "14", SDKInt: 34, Model: "Pixel"}
			},
			want: true,
		},
		{
			name: "android 13 requires smooth or power-efficient signal",
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "13", SDKInt: 33, Model: "Android TV"}
				c.VideoCodecSignals = []capabilities.VideoCodecSignal{{Codec: "av1", Supported: true, Smooth: &supportedOnly}}
			},
			want: false,
		},
		{
			name:   "android tv browser on shield android 11 blocks av1 despite smooth browser signal",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "NVIDIA", Model: "SHIELD Android TV"}
			},
			want: false,
		},
		{
			name: "shield stays blocked even when client family is missing",
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "NVIDIA", Product: "mdarcy", Device: "foster", Model: "SHIELD Android TV"}
			},
			want: false,
		},
		{
			name:   "unknown android tv browser on android 11 blocks av1 despite smooth signal",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Model: "Generic Android TV"}
			},
			want: false,
		},
		{
			name:   "fire tv 4k max 2nd gen allows av1 from known build model",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "Amazon", Model: "AFTKRT"}
			},
			want: true,
		},
		{
			name:   "fire tv cube 3rd gen allows av1 from known build model on older android base",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "9", SDKInt: 28, Manufacturer: "Amazon", Model: "AFTGAZL"}
			},
			want: true,
		},
		{
			name:   "vega fire tv allows av1 from known build model without android os name",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "vegaos", OSVersion: "1.1", Manufacturer: "Amazon", Model: "AFTCL001"}
			},
			want: true,
		},
		{
			name:   "xiaomi tv stick 4k allows av1 from known model",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "Xiaomi", Model: "MDZ-27-AA Xiaomi TV Stick 4K"}
			},
			want: true,
		},
		{
			name:   "known fire tv still requires smooth or power-efficient av1 signal",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "11", SDKInt: 30, Manufacturer: "Amazon", Model: "AFTKRT"}
				c.VideoCodecSignals = []capabilities.VideoCodecSignal{{Codec: "av1", Supported: true, Smooth: &supportedOnly}}
			},
			want: false,
		},
		{
			name:   "android tv browser on android 14 allows only strong runtime av1",
			family: playbackprofile.ClientAndroidTVBrowser,
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceType = "android_tv"
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "14", SDKInt: 34, Model: "Android TV"}
			},
			want: true,
		},
		{
			name: "windows vm allows only explicit smooth runtime av1",
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "windows", OSVersion: "11", Model: "vm"}
			},
			want: true,
		},
		{
			name: "linux vm blocks supported-only av1",
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.DeviceContext = &capabilities.DeviceContext{OSName: "linux", Model: "kvm"}
				c.VideoCodecSignals = []capabilities.VideoCodecSignal{{Codec: "av1", Supported: true}}
			},
			want: false,
		},
		{
			name: "family fallback without runtime source is never enough",
			mutate: func(c *capabilities.PlaybackCapabilities) {
				c.ClientCapsSource = capabilities.ClientCapsSourceFamilyFallback
				c.DeviceContext = &capabilities.DeviceContext{OSName: "android", OSVersion: "14", SDKInt: 34}
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			caps := base()
			if tt.mutate != nil {
				tt.mutate(&caps)
			}
			if got := ClientAV1PlaybackAllowed(caps, tt.family); got != tt.want {
				t.Fatalf("ClientAV1PlaybackAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
