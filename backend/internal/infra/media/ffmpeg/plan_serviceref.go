package ffmpeg

import (
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"net/url"
	"strings"
)

func normalizeServiceRef(raw string) string {
	ref := strings.TrimSpace(raw)
	ref = strings.Trim(ref, "/")
	if ref == "" {
		return ""
	}
	if !isLikelyServiceRef(ref) {
		return ""
	}
	ref = strings.TrimRight(ref, ":")
	if isHexColonServiceRef(ref) {
		return strings.ToUpper(ref)
	}
	return ref
}

func extractServiceRefFromURL(inputURL string) string {
	u, err := url.Parse(strings.TrimSpace(inputURL))
	if err != nil {
		return ""
	}
	if ref := normalizeServiceRef(u.Query().Get("ref")); ref != "" {
		return ref
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return normalizeServiceRef(path)
}

func isLikelyServiceRef(value string) bool {
	return strings.Count(value, ":") >= 5
}

func isHexColonServiceRef(ref string) bool {
	if ref == "" || !strings.Contains(ref, ":") {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch == ':':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func safariRuntimeServiceRef(spec ports.StreamSpec, inputURL string) string {
	if ref := normalizeServiceRef(spec.Source.ID); ref != "" {
		return ref
	}
	return extractServiceRefFromURL(inputURL)
}

func shouldForceSafariCopyForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	if spec.Profile.DisableSafariForceCopy {
		return false
	}
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}

	return serviceRefEnvContains("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", targetRef)
}

func shouldForceSafariHQForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}
	return serviceRefEnvContains("XG2G_SAFARI_HQ_SERVICE_REFS", targetRef)
}

func shouldForceAnySafariHQForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	return shouldForceSafariHQForServiceRef(spec, inputURL) ||
		shouldForceSafariHQ25ForServiceRef(spec, inputURL) ||
		shouldForceSafariHQ50ForServiceRef(spec, inputURL)
}

func shouldForceSafariHQ25ForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}
	return serviceRefEnvContains("XG2G_SAFARI_HQ25_SERVICE_REFS", targetRef)
}

func shouldForceSafariHQ50ForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}
	return serviceRefEnvContains("XG2G_SAFARI_HQ50_SERVICE_REFS", targetRef)
}

func safariHQRuntimeMode(profile ports.ProfileSpec) ports.RuntimeMode {
	if shouldForce25FPSForSafariHQ(profile) {
		return ports.RuntimeModeHQ25
	}
	return ports.RuntimeModeHQ50
}

func shouldUseProgressiveSafariHQ(profile ports.ProfileSpec) bool {
	hint := profile.PolicyModeHint
	if hint == "" || hint == ports.RuntimeModeUnknown {
		hint = profiles.RuntimeModeHintFromProfile(profile)
	}
	return hint == ports.RuntimeModeCopy || hint == ports.RuntimeModeCopyHardened
}

func shouldForce25FPSForSafariHQ(profile ports.ProfileSpec) bool {
	if profile.ForceSafariHQ25 {
		return true
	}
	if profile.EffectiveRuntimeMode == ports.RuntimeModeHQ50 || profile.PolicyModeHint == ports.RuntimeModeHQ50 {
		return false
	}
	return !shouldUseProgressiveSafariHQ(profile)
}

func shouldHardenSafariCopyBitstream(spec ports.StreamSpec, inputURL string) bool {
	return shouldForceSafariCopyForServiceRef(spec, inputURL)
}

func serviceRefEnvContains(envKey, targetRef string) bool {
	raw := strings.TrimSpace(config.ParseString(envKey, ""))
	if raw == "" || targetRef == "" {
		return false
	}
	for _, candidate := range strings.Split(raw, ",") {
		if normalizeServiceRef(candidate) == targetRef {
			return true
		}
	}
	return false
}
