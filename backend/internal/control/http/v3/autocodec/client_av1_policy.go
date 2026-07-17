package autocodec

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

// ClientAV1PlaybackAllowed is the client-side decode guard for AV1 output.
// AV1 must be proven by a runtime/native probe and by device/OS policy; family
// fixtures alone are not enough because mobile AV1 support is device-specific.
func ClientAV1PlaybackAllowed(caps capabilities.PlaybackCapabilities, clientFamily string) bool {
	return ClientAV1PlaybackAllowedWithPolicy(caps, clientFamily, false)
}

// ClientAV1PlaybackAllowedWithPolicy is the pure form used by snapshot-bound
// planning adapters. The operational kill switch is captured before the
// request and supplied explicitly.
func ClientAV1PlaybackAllowedWithPolicy(caps capabilities.PlaybackCapabilities, clientFamily string, disabled bool) bool {
	if disabled {
		return false
	}
	canonical := capabilities.CanonicalizeCapabilities(caps)
	if !playbackCapabilitiesHaveCodec(canonical.VideoCodecs, "av1") {
		return false
	}
	if !playbackCapabilitiesHaveCodec(canonical.Containers, "fmp4") {
		return false
	}
	if !hasRuntimeAV1Signal(canonical) {
		return false
	}
	if hasKnownTVNoAV1Model(canonical.DeviceContext) {
		return false
	}

	family := normalize.Token(clientFamily)
	if family == "" {
		family = normalize.Token(canonical.ClientFamilyFallback)
	}
	if family == "" && normalize.Token(canonical.DeviceType) == "android_tv" {
		family = playbackprofile.ClientAndroidTVBrowser
	}
	switch family {
	case playbackprofile.ClientIOSSafariNative:
		return appleMobileAV1Allowed(canonical)
	case playbackprofile.ClientSafariNative:
		return appleDesktopAV1Allowed(canonical)
	case playbackprofile.ClientAndroidTVBrowser:
		return androidTVBrowserAV1Allowed(canonical)
	default:
		return androidOrGenericAV1Allowed(canonical)
	}
}

func hasRuntimeAV1Signal(caps capabilities.PlaybackCapabilities) bool {
	source := normalize.Token(caps.ClientCapsSource)
	if source != capabilities.ClientCapsSourceRuntime && source != capabilities.ClientCapsSourceRuntimePlusFam {
		return false
	}
	if !caps.RuntimeProbeUsed && len(caps.VideoCodecSignals) == 0 {
		return false
	}
	signal, ok := videoCodecSignal(caps, "av1")
	return ok && signal.Supported
}

func appleMobileAV1Allowed(caps capabilities.PlaybackCapabilities) bool {
	if !isAppleMobileOS(caps.DeviceContext) && !looksLikeAppleMobile(caps) {
		return false
	}
	if !minimumMajorVersion(caps.DeviceContext, 17) {
		return false
	}
	if hasKnownAppleAV1Model(caps.DeviceContext) {
		return true
	}
	if hasKnownApplePreAV1Model(caps.DeviceContext) {
		return false
	}
	return av1SignalIsAtLeastSmooth(caps)
}

func appleDesktopAV1Allowed(caps capabilities.PlaybackCapabilities) bool {
	if caps.DeviceContext == nil || normalize.Token(caps.DeviceContext.OSName) != "macos" {
		return av1SignalIsAtLeastSmooth(caps)
	}
	if !minimumMajorVersion(caps.DeviceContext, 14) {
		return false
	}
	if hasKnownAppleAV1Model(caps.DeviceContext) {
		return true
	}
	if hasKnownApplePreAV1Model(caps.DeviceContext) {
		return false
	}
	return av1SignalIsAtLeastSmooth(caps)
}

func androidOrGenericAV1Allowed(caps capabilities.PlaybackCapabilities) bool {
	if caps.DeviceContext != nil && normalize.Token(caps.DeviceContext.OSName) == "android" {
		if androidSDK(caps.DeviceContext) >= 34 || majorVersion(caps.DeviceContext.OSVersion) >= 14 {
			return av1SignalIsAtLeastSmooth(caps)
		}
		// Android 10+ can expose AV1, but on Android 10-13 it is too device
		// specific to trust a plain "supported" bit. Require the browser/native
		// probe to report a smooth or power-efficient decode path.
		return av1SignalIsPowerEfficientOrSmooth(caps)
	}
	return av1SignalIsPowerEfficientOrSmooth(caps)
}

func androidTVBrowserAV1Allowed(caps capabilities.PlaybackCapabilities) bool {
	if hasKnownTVAV1Model(caps.DeviceContext) {
		return av1SignalIsPowerEfficientOrSmooth(caps)
	}
	if caps.DeviceContext == nil || normalize.Token(caps.DeviceContext.OSName) != "android" {
		return false
	}
	if androidSDK(caps.DeviceContext) < 34 && majorVersion(caps.DeviceContext.OSVersion) < 14 {
		return false
	}
	return av1SignalIsPowerEfficientOrSmooth(caps)
}

func av1SignalIsAtLeastSmooth(caps capabilities.PlaybackCapabilities) bool {
	signal, ok := videoCodecSignal(caps, "av1")
	return ok && signal.Supported && (boolPtrValue(signal.Smooth) || boolPtrValue(signal.PowerEfficient) || caps.RuntimeProbeUsed)
}

func av1SignalIsPowerEfficientOrSmooth(caps capabilities.PlaybackCapabilities) bool {
	signal, ok := videoCodecSignal(caps, "av1")
	return ok && signal.Supported && (boolPtrValue(signal.Smooth) || boolPtrValue(signal.PowerEfficient))
}

func videoCodecSignal(caps capabilities.PlaybackCapabilities, codec string) (capabilities.VideoCodecSignal, bool) {
	for _, signal := range caps.VideoCodecSignals {
		if normalize.Token(signal.Codec) == codec {
			return signal, true
		}
	}
	return capabilities.VideoCodecSignal{}, false
}

func boolPtrValue(v *bool) bool {
	return v != nil && *v
}

func isAppleMobileOS(ctx *capabilities.DeviceContext) bool {
	if ctx == nil {
		return false
	}
	switch normalize.Token(ctx.OSName) {
	case "ios", "ipados":
		return true
	default:
		return false
	}
}

func looksLikeAppleMobile(caps capabilities.PlaybackCapabilities) bool {
	switch normalize.Token(caps.DeviceType) {
	case "iphone", "ipad", "ios", "ios_safari", "mobile":
		return true
	default:
		return false
	}
}

func minimumMajorVersion(ctx *capabilities.DeviceContext, minMajor int) bool {
	if ctx == nil || strings.TrimSpace(ctx.OSVersion) == "" {
		return true
	}
	major := majorVersion(ctx.OSVersion)
	return major == 0 || major >= minMajor
}

func androidSDK(ctx *capabilities.DeviceContext) int {
	if ctx == nil {
		return 0
	}
	return ctx.SDKInt
}

func majorVersion(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var b strings.Builder
	for _, r := range raw {
		if !unicode.IsDigit(r) {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return 0
	}
	v, err := strconv.Atoi(b.String())
	if err != nil {
		return 0
	}
	return v
}

func hasKnownAppleAV1Model(ctx *capabilities.DeviceContext) bool {
	if ctx == nil {
		return false
	}
	token := appleModelToken(ctx)
	return tokenContainsAny(token, knownAppleAV1ModelMarkers)
}

func hasKnownApplePreAV1Model(ctx *capabilities.DeviceContext) bool {
	if ctx == nil {
		return false
	}
	token := appleModelToken(ctx)
	return tokenContainsAny(token, knownApplePreAV1ModelMarkers)
}

func hasKnownTVAV1Model(ctx *capabilities.DeviceContext) bool {
	if ctx == nil {
		return false
	}
	return tokenContainsAny(deviceContextToken(ctx), knownTVAV1ModelMarkers)
}

func hasKnownTVNoAV1Model(ctx *capabilities.DeviceContext) bool {
	if ctx == nil {
		return false
	}
	return tokenContainsAny(deviceContextToken(ctx), knownTVNoAV1ModelMarkers)
}

var knownAppleAV1ModelMarkers = []string{
	// Apple mobile chips with AV1 playback in current iPhone/iPad lines.
	"a17pro",
	"a18",
	"a19",

	// Apple Silicon Macs/iPads with AV1 decode media engines.
	"m3",
	"m4",
	"m5",

	// iPhone marketing names and native hw.machine identifiers.
	"iphone15pro",
	"iphone15promax",
	"iphone16",
	"iphone16plus",
	"iphone16pro",
	"iphone16promax",
	"iphone16e",
	"iphone17",
	"iphone17pro",
	"iphone17promax",
	"iphoneair",
	"iphone161", // iPhone 15 Pro
	"iphone162", // iPhone 15 Pro Max
	"iphone171",
	"iphone172",
	"iphone173",
	"iphone174",
	"iphone175",
	"iphone181",
	"iphone182",
	"iphone183",
	"iphone184",

	// iPad marketing names for the AV1-capable generations.
	"ipadairm3",
	"ipadair11inchm3",
	"ipadair13inchm3",
	"ipadprom4",
	"ipadpro11inchm4",
	"ipadpro13inchm4",
	"ipadminia17pro",

	// Mac product names are redundant with M3/M4/M5 but make sparse native
	// payloads explicit and keep tests readable.
	"macbookairm3",
	"macbookairm4",
	"macbookprom3",
	"macbookprom4",
	"imacm3",
	"imacm4",
	"macminim4",
	"macstudiom3ultra",
	"macstudiom4max",
}

var knownApplePreAV1ModelMarkers = []string{
	"a16",
	"a15",
	"a14",
	"a13",
	"a12",
	"m1",
	"m2",
	"iphone15", // non-Pro iPhone 15/15 Plus and iPhone15,* hw.machine IDs are A16.
	"iphone14",
	"iphone13",
	"iphone12",
	"iphone11",
	"iphonese",
	"ipada16",
	"ipadair4",
	"ipadair5",
	"ipadairm1",
	"ipadairm2",
	"ipadprom1",
	"ipadprom2",
}

var knownTVAV1ModelMarkers = []string{
	// Amazon official build models with hardware AV1 decode.
	"aftcl001",   // Fire TV Stick HD (2026)
	"aftma08c15", // Fire TV Stick 4K Plus (2025)
	"aftca002",   // Fire TV Stick 4K Select (2025)
	"aftkrt",     // Fire TV Stick 4K Max 2nd Gen (2023)
	"aftkm",      // Fire TV Stick 4K 2nd Gen (2023)
	"aftka",      // Fire TV Stick 4K Max 1st Gen (2021)
	"aftgazl",    // Fire TV Cube 3rd Gen (2022)

	// Xiaomi TV Stick 4K official AV1 model/name variants.
	"xiaomitvstick4k",
	"mitvstick4k",
	"mdz27aa",
	"mitvayfr0",
}

var knownTVNoAV1ModelMarkers = []string{
	// NVIDIA Shield TV 2019/Tegra X1+ is Android TV, but has no AV1 decoder.
	"nvidiashield",
	"shieldandroidtv",
	"mdarcy",
	"foster",
	"tegrax1",
}

func tokenContainsAny(token string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(token, marker) {
			return true
		}
	}
	return false
}

func appleModelToken(ctx *capabilities.DeviceContext) string {
	return deviceContextToken(ctx)
}

func deviceContextToken(ctx *capabilities.DeviceContext) string {
	parts := []string{ctx.Brand, ctx.Manufacturer, ctx.Product, ctx.Device, ctx.Model, ctx.Platform}
	var b strings.Builder
	for _, part := range parts {
		for _, r := range strings.ToLower(part) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
