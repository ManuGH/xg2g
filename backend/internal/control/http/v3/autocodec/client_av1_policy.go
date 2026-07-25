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
	allowed, _ := ClientAV1PlaybackVerdict(caps, clientFamily, disabled)
	return allowed
}

// AV1 rejection reasons. These are stable tokens meant for logs and traces:
// when AV1 does not get picked, exactly one of these says why. Without them the
// only way to find out is to read this file and guess which guard fired —
// which is how a client-side capability filter once masqueraded as a
// server-side profile problem through eight consecutive "fixes".
const (
	AV1RejectOperatorDisabled = "operator_disabled"
	AV1RejectClientNoAV1Codec = "client_no_av1_codec"
	AV1RejectNoFMP4Container  = "client_no_fmp4_container"
	AV1RejectNoRuntimeSignal  = "no_runtime_av1_signal"
	AV1RejectKnownTVNoAV1     = "known_tv_model_without_av1"
	AV1RejectAppleModel       = "apple_model_predates_av1"
	AV1RejectAppleOSTooOld    = "apple_os_below_av1_minimum"
	AV1RejectNotAppleMobile   = "not_an_apple_mobile_device"
	AV1RejectAndroidTooOld    = "android_below_av1_minimum"
	AV1RejectSignalTooWeak    = "av1_signal_neither_smooth_nor_power_efficient"
)

// ClientAV1PlaybackVerdict is the reasoned form of the guard. It returns the
// same decision plus a stable reason token when AV1 is refused (empty when
// allowed), so callers can log and surface WHY rather than only that it failed.
func ClientAV1PlaybackVerdict(caps capabilities.PlaybackCapabilities, clientFamily string, disabled bool) (bool, string) {
	if disabled {
		return false, AV1RejectOperatorDisabled
	}
	canonical := capabilities.CanonicalizeCapabilities(caps)
	if !playbackCapabilitiesHaveCodec(canonical.VideoCodecs, "av1") {
		// The client never offered AV1. Note this is reachable even on hardware
		// that decodes AV1 fine, if the client filtered its own codec list
		// before sending it — check videoCodecSignals for the raw verdicts.
		return false, AV1RejectClientNoAV1Codec
	}
	if !playbackCapabilitiesHaveCodec(canonical.Containers, "fmp4") {
		return false, AV1RejectNoFMP4Container
	}
	if !hasRuntimeAV1Signal(canonical) {
		return false, AV1RejectNoRuntimeSignal
	}
	if hasKnownTVNoAV1Model(canonical.DeviceContext) {
		return false, AV1RejectKnownTVNoAV1
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
		return appleMobileAV1Verdict(canonical)
	case playbackprofile.ClientSafariNative:
		return appleDesktopAV1Verdict(canonical)
	case playbackprofile.ClientAndroidTVBrowser:
		return androidTVBrowserAV1Verdict(canonical)
	default:
		return androidOrGenericAV1Verdict(canonical)
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

// appleModelAV1Verdict consults the curated Apple model tables. Apple ships AV1
// decode per silicon generation (A17 Pro / M3 and up), and the OS minimum alone
// cannot tell an M2 from an M4 — both run macOS 14+. The tables settle it when
// the device identifies itself.
//
// Known-AV1 is checked BEFORE known-pre-AV1 on purpose: model strings overlap
// ("iPhone 15 Pro A17 Pro" matches both the "iphone15pro" AV1 marker and the
// "iphone15" pre-AV1 marker), and the more specific AV1 verdict must win.
//
// decided=false means the device did not identify itself well enough to judge —
// browsers commonly send only {platform, osName}. Those fall through to the
// runtime probe rather than being vetoed on a missing field.
func appleModelAV1Verdict(ctx *capabilities.DeviceContext) (allowed bool, decided bool) {
	if hasKnownAppleAV1Model(ctx) {
		return true, true
	}
	if hasKnownApplePreAV1Model(ctx) {
		return false, true
	}
	return false, false
}

func appleMobileAV1Verdict(caps capabilities.PlaybackCapabilities) (bool, string) {
	if !isAppleMobileOS(caps.DeviceContext) && !looksLikeAppleMobile(caps) {
		return false, AV1RejectNotAppleMobile
	}
	if !minimumMajorVersion(caps.DeviceContext, 17) {
		return false, AV1RejectAppleOSTooOld
	}
	if allowed, decided := appleModelAV1Verdict(caps.DeviceContext); decided && !allowed {
		return false, AV1RejectAppleModel
	}
	if !hasRuntimeAV1Signal(caps) {
		return false, AV1RejectNoRuntimeSignal
	}
	return true, ""
}

func appleDesktopAV1Verdict(caps capabilities.PlaybackCapabilities) (bool, string) {
	if caps.DeviceContext == nil || normalize.Token(caps.DeviceContext.OSName) != "macos" {
		if !hasRuntimeAV1Signal(caps) {
			return false, AV1RejectNoRuntimeSignal
		}
		return true, ""
	}
	if !minimumMajorVersion(caps.DeviceContext, 14) {
		return false, AV1RejectAppleOSTooOld
	}
	if allowed, decided := appleModelAV1Verdict(caps.DeviceContext); decided && !allowed {
		return false, AV1RejectAppleModel
	}
	if !hasRuntimeAV1Signal(caps) {
		return false, AV1RejectNoRuntimeSignal
	}
	return true, ""
}

func androidOrGenericAV1Verdict(caps capabilities.PlaybackCapabilities) (bool, string) {
	if caps.DeviceContext != nil && normalize.Token(caps.DeviceContext.OSName) == "android" {
		if androidSDK(caps.DeviceContext) >= 34 || majorVersion(caps.DeviceContext.OSVersion) >= 14 {
			if !av1SignalIsAtLeastSmooth(caps) {
				return false, AV1RejectSignalTooWeak
			}
			return true, ""
		}
		// Android 10+ can expose AV1, but on Android 10-13 it is too device
		// specific to trust a plain "supported" bit. Require the browser/native
		// probe to report a smooth or power-efficient decode path.
		if !av1SignalIsPowerEfficientOrSmooth(caps) {
			return false, AV1RejectSignalTooWeak
		}
		return true, ""
	}
	if !av1SignalIsPowerEfficientOrSmooth(caps) {
		return false, AV1RejectSignalTooWeak
	}
	return true, ""
}

func androidTVBrowserAV1Verdict(caps capabilities.PlaybackCapabilities) (bool, string) {
	if hasKnownTVAV1Model(caps.DeviceContext) {
		if !av1SignalIsPowerEfficientOrSmooth(caps) {
			return false, AV1RejectSignalTooWeak
		}
		return true, ""
	}
	if caps.DeviceContext == nil || normalize.Token(caps.DeviceContext.OSName) != "android" {
		return false, AV1RejectAndroidTooOld
	}
	if androidSDK(caps.DeviceContext) < 34 && majorVersion(caps.DeviceContext.OSVersion) < 14 {
		return false, AV1RejectAndroidTooOld
	}
	if !av1SignalIsPowerEfficientOrSmooth(caps) {
		return false, AV1RejectSignalTooWeak
	}
	return true, ""
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
	case "ios", "ipados", "tvos":
		return true
	default:
		return false
	}
}

func looksLikeAppleMobile(caps capabilities.PlaybackCapabilities) bool {
	switch normalize.Token(caps.DeviceType) {
	case "iphone", "ipad", "ios", "ios_safari", "mobile", "appletv":
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
