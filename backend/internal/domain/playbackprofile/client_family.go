package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

// NormalizeClientFamilyID accepts canonical fixture ids and coarse legacy
// aliases used by older clients/native bridges.
func NormalizeClientFamilyID(value string) string {
	switch normalize.Token(value) {
	case "safari", ClientSafariNative:
		return ClientSafariNative
	case "ios_safari", ClientIOSSafariNative:
		return ClientIOSSafariNative
	case "firefox", ClientFirefoxHLSJS:
		return ClientFirefoxHLSJS
	case "android_tv", "android_tv_hlsjs", "shield_browser", ClientAndroidTVBrowser:
		return ClientAndroidTVBrowser
	case "chromium", "chrome", "edge", ClientChromiumHLSJS:
		return ClientChromiumHLSJS
	default:
		return normalize.Token(value)
	}
}
