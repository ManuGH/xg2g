// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

// NormalizeClientFamilyID canonicalizes a family identifier.
//
// Clients no longer send one — `PlaybackClientIdentity` is the wire input and
// ClassifyClient turns it into a family — so this now serves two internal
// callers: operator tooling that takes a family on the command line, and rows
// already written into the capability registry.
//
// `ios_safari_native` is one of those rows. It meant Safari on iOS, and the
// native app reported it too; anything still carrying it can only be resolved
// as the browser, because that is what the recorded capabilities describe.
func NormalizeClientFamilyID(value string) string {
	switch normalize.Token(value) {
	case "safari", ClientSafariNative:
		return ClientSafariNative
	case "ios_safari_native", ClientIOSSafari:
		return ClientIOSSafari
	case "ios", "apple_native", ClientIOSNative:
		return ClientIOSNative
	case "tvos", ClientAppleTVNative:
		return ClientAppleTVNative
	case "firefox", ClientFirefoxHLSJS:
		return ClientFirefoxHLSJS
	case "android_tv", "android_tv_hlsjs", "shield_browser", ClientAndroidTVBrowser:
		return ClientAndroidTVBrowser
	case ClientAndroidTVNative:
		return ClientAndroidTVNative
	case "android", ClientAndroidNative:
		return ClientAndroidNative
	case "chromium", "chrome", "edge", ClientChromiumHLSJS:
		return ClientChromiumHLSJS
	default:
		return normalize.Token(value)
	}
}
