// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "strings"

// ClientSurface is what kind of thing is playing: an application the project
// ships, or a page in somebody's browser.
//
// The distinction decides how much a capability claim is worth. A native client
// enumerates decoders it actually has; a browser reports what its media stack
// is willing to guess about a codec string. Treating those as the same kind of
// evidence is what let `ios_safari_native` stand for both.
type ClientSurface string

const (
	SurfaceNativeApp ClientSurface = "native_app"
	SurfaceBrowser   ClientSurface = "browser"
)

// ClientPlatform is the operating system the client runs on.
type ClientPlatform string

const (
	PlatformIOS       ClientPlatform = "ios"
	PlatformIPadOS    ClientPlatform = "ipados"
	PlatformMacOS     ClientPlatform = "macos"
	PlatformTVOS      ClientPlatform = "tvos"
	PlatformAndroid   ClientPlatform = "android"
	PlatformAndroidTV ClientPlatform = "android_tv"
	PlatformWindows   ClientPlatform = "windows"
	PlatformLinux     ClientPlatform = "linux"
	PlatformUnknown   ClientPlatform = "unknown"
)

// BrowserEngine is the rendering engine behind a browser surface.
type BrowserEngine string

const (
	EngineWebKit  BrowserEngine = "webkit"
	EngineBlink   BrowserEngine = "blink"
	EngineGecko   BrowserEngine = "gecko"
	EngineUnknown BrowserEngine = "unknown"
)

// ClientIdentity is what a client declares about itself: two facts, no
// conclusions. Everything downstream — family, capability defaults, playback
// policy — is derived from it here, on the server.
type ClientIdentity struct {
	Platform      ClientPlatform
	Surface       ClientSurface
	BrowserEngine BrowserEngine
}

// ClassifyClient resolves a declared identity into the family the policy
// matrix is keyed on.
//
// # Why the server does this
//
// Client families used to arrive on the wire as `clientFamilyFallback`, which
// meant every client decided what policy applied to it. The native iOS app
// declared `ios_safari_native` — Safari on iOS — and so was handed browser
// policy: conservative capability defaults, browser packaging preferences, and
// a decision path that assumes MediaSource rather than a demuxer the app
// carries itself. Nothing detected the mismatch because there was nothing to
// detect it against; the string was authoritative by virtue of being sent.
//
// Moving the mapping here makes the classification a server decision that a
// client cannot override, and makes adding a client family a change to this
// function rather than to a string constant in an app.
//
// `userAgent` participates only as a last resort, for a browser that declared
// no engine. It never overrides a declared identity.
func ClassifyClient(identity ClientIdentity, userAgent string) string {
	switch identity.Surface {
	case SurfaceNativeApp:
		return classifyNative(identity.Platform)
	case SurfaceBrowser:
		return classifyBrowser(identity.Platform, identity.BrowserEngine, userAgent)
	}
	// An unstated surface is not assumed to be either. The most conservative
	// browser profile is the safe answer: it claims the least.
	return ClientChromiumHLSJS
}

func classifyNative(platform ClientPlatform) string {
	switch platform {
	case PlatformIOS, PlatformIPadOS:
		return ClientIOSNative
	case PlatformTVOS:
		return ClientAppleTVNative
	case PlatformAndroidTV:
		return ClientAndroidTVNative
	case PlatformAndroid:
		return ClientAndroidNative
	case PlatformMacOS:
		// No macOS app ships yet. Until one does, a client claiming to be one
		// gets the desktop browser profile rather than an invented family.
		return ClientSafariNative
	default:
		return ClientChromiumHLSJS
	}
}

func classifyBrowser(platform ClientPlatform, engine BrowserEngine, userAgent string) string {
	if engine == "" || engine == EngineUnknown {
		engine = engineFromUserAgent(userAgent)
	}

	switch platform {
	case PlatformIOS, PlatformIPadOS:
		// Every iOS browser is WebKit underneath, so the platform decides here
		// and the declared engine cannot contradict it.
		return ClientIOSSafari
	case PlatformAndroidTV:
		return ClientAndroidTVBrowser
	}

	switch engine {
	case EngineWebKit:
		return ClientSafariNative
	case EngineGecko:
		return ClientFirefoxHLSJS
	default:
		return ClientChromiumHLSJS
	}
}

// engineFromUserAgent is the fallback for a browser that declared no engine.
//
// Deliberately narrow: it answers only the question the enum asks, and it is
// reached only when the client left the field empty. Widening it would rebuild
// the user-agent sniffing this change exists to retire.
func engineFromUserAgent(userAgent string) BrowserEngine {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "firefox"):
		return EngineGecko
	case strings.Contains(ua, "safari") &&
		!strings.Contains(ua, "chrome") &&
		!strings.Contains(ua, "chromium") &&
		!strings.Contains(ua, "android"):
		return EngineWebKit
	default:
		return EngineBlink
	}
}

// DeviceTypeForFamily is the coarse device category the decision trace and the
// capability registry record.
//
// It used to arrive from the client as `deviceType`, alongside the family, with
// nothing keeping the two consistent — a client could call itself a TV and a
// desktop browser in the same request. Deriving it from the family makes that
// unrepresentable.
func DeviceTypeForFamily(family string) string {
	switch family {
	case ClientIOSNative, ClientIOSSafari:
		return "mobile"
	case ClientAppleTVNative, ClientAndroidTVNative, ClientAndroidTVBrowser:
		return "tv"
	case ClientAndroidNative:
		return "mobile"
	default:
		return "desktop"
	}
}

// IsBrowserFamily reports whether a family's capability claims come from a
// browser rather than from an application that enumerates its own decoders.
func IsBrowserFamily(family string) bool {
	switch family {
	case ClientSafariNative, ClientIOSSafari, ClientFirefoxHLSJS,
		ClientAndroidTVBrowser, ClientChromiumHLSJS:
		return true
	default:
		return false
	}
}

// IsNativeFamily is the complement of IsBrowserFamily over the known families.
func IsNativeFamily(family string) bool {
	switch family {
	case ClientIOSNative, ClientAppleTVNative, ClientAndroidNative, ClientAndroidTVNative:
		return true
	default:
		return false
	}
}

// IsIOSFamily covers everything running on iOS or iPadOS, app and browser
// alike. Hardware-decode policy is a property of the silicon, so it applies to
// both; splitting the families must not split a rule that was never about the
// surface.
func IsIOSFamily(family string) bool {
	switch family {
	case ClientIOSNative, ClientIOSSafari:
		return true
	default:
		return false
	}
}

// IsAppleFamily covers both Apple surfaces. Several policies apply to "an Apple
// client" regardless of whether it is the app or Safari — fMP4 packaging, for
// one — and spelling that out at each site is how the two came to be conflated.
func IsAppleFamily(family string) bool {
	switch family {
	case ClientIOSNative, ClientIOSSafari, ClientAppleTVNative, ClientSafariNative:
		return true
	default:
		return false
	}
}
