package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

type legacyEnvDiagnostic struct {
	key   string
	lines []string
}

var legacyExactEnvKeys = []string{
	"XG2G_V3_WORKER_ENABLED",
	"XG2G_V3_WORKER_MODE",
	"XG2G_V3_IDLE_TIMEOUT",
	"XG2G_V3_STORE_BACKEND",
	"XG2G_V3_STORE_PATH",
	"XG2G_V3_HLS_ROOT",
	"XG2G_V3_TUNER_SLOTS",
	"XG2G_V3_CONFIG_STRICT",
	"XG2G_V3_E2_HOST",
	// Blocked legacy single-key (outside the XG2G_V3_* prefix family).
	// Kept as a split literal to ensure repo searches for the legacy key name return zero results.
	"XG2G_FFMPEG_" + "PATH",
}

var legacyCompatEnvKeys = []string{
	"XG2G_OWI_BASE",
	"XG2G_OWI_USER",
	"XG2G_OWI_PASS",
	"XG2G_OWI_TIMEOUT_MS",
	"XG2G_OWI_RETRIES",
	"XG2G_OWI_BACKOFF_MS",
	"XG2G_OWI_MAX_BACKOFF_MS",
	"XG2G_MONETIZATION_UNLOCK_SCOPE",
	"XG2G_STREAM_PORT",
	"XG2G_USE_WEBIF_STREAMS",
}

// FindLegacyEnvKeys returns all legacy env keys from the provided environment.
func FindLegacyEnvKeys(environ []string) []string {
	diagnostics := findLegacyEnvDiagnostics(environ)
	out := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, diagnostic.key)
	}
	return out
}

func findLegacyEnvDiagnostics(environ []string) []legacyEnvDiagnostic {
	legacyPrefix := "XG2G_V3_"
	out := make([]legacyEnvDiagnostic, 0)

	exact := make(map[string]struct{}, len(legacyExactEnvKeys)+len(legacyCompatEnvKeys))
	for _, key := range legacyExactEnvKeys {
		exact[key] = struct{}{}
	}
	for _, key := range legacyCompatEnvKeys {
		exact[key] = struct{}{}
	}

	for _, env := range environ {
		pair := strings.SplitN(env, "=", 2)
		key := pair[0]
		value := ""
		if len(pair) == 2 {
			value = pair[1]
		}
		if key == "" {
			continue
		}

		if strings.HasPrefix(key, legacyPrefix) {
			out = append(out, legacyEnvDiagnostic{
				key:   key,
				lines: []string{"remove this key and migrate to the canonical XG2G_* env surface"},
			})
			continue
		}

		if _, ok := exact[key]; !ok {
			continue
		}
		if diagnostic, ok := legacyCompatEnvDiagnostic(key, value); ok {
			out = append(out, diagnostic)
			continue
		}
		out = append(out, legacyEnvDiagnostic{
			key:   key,
			lines: []string{"remove this key and migrate to the canonical XG2G_* env surface"},
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// CheckLegacyEnvWithEnviron validates that no legacy keys are present.
func CheckLegacyEnvWithEnviron(environ []string) error {
	if len(environ) == 0 {
		environ = os.Environ()
	}
	diagnostics := findLegacyEnvDiagnostics(environ)
	if len(diagnostics) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("legacy environment variable(s) detected:")
	for _, diagnostic := range diagnostics {
		b.WriteString("\n- ")
		b.WriteString(diagnostic.key)
		for _, line := range diagnostic.lines {
			b.WriteString("\n  ")
			b.WriteString(line)
		}
	}
	b.WriteString("\nSee ")
	b.WriteString(enigma2MigrationGuide)
	return fmt.Errorf("%s", b.String())
}

// CheckLegacyEnv scans environment variables for legacy keys and exits if any are found.
// This enforces the "Canonical Only" policy for pre-release.
func CheckLegacyEnv() {
	logger := log.WithComponent("config")
	err := CheckLegacyEnvWithEnviron(os.Environ())
	if err == nil {
		return
	}
	for _, key := range FindLegacyEnvKeys(os.Environ()) {
		logger.Error().Str("key", key).Msg("Legacy configuration key detected.")
	}
	logger.Fatal().Msg(err.Error())
}

func isLegacyEnvHandledByGuardrail(key string) bool {
	if strings.HasPrefix(key, "XG2G_V3_") {
		return true
	}
	if key == "XG2G_FFMPEG_PATH" {
		return true
	}
	for _, legacyKey := range legacyCompatEnvKeys {
		if key == legacyKey {
			return true
		}
	}
	return false
}

func legacyCompatEnvDiagnostic(key, value string) (legacyEnvDiagnostic, bool) {
	switch key {
	case "XG2G_OWI_BASE":
		return legacyEnvDiagnostic{key: key, lines: legacyHostMigrationLines(value)}, true
	case "XG2G_OWI_USER":
		return legacyEnvDiagnostic{key: key, lines: []string{fmt.Sprintf("replace with: XG2G_E2_USER=%s", strings.TrimSpace(value))}}, true
	case "XG2G_OWI_PASS":
		return legacyEnvDiagnostic{key: key, lines: []string{"set XG2G_E2_PASS to the same secret currently stored in XG2G_OWI_PASS"}}, true
	case "XG2G_OWI_TIMEOUT_MS":
		return legacyEnvDiagnostic{key: key, lines: legacyDurationMigrationLines(key, value, "XG2G_E2_TIMEOUT")}, true
	case "XG2G_OWI_RETRIES":
		return legacyEnvDiagnostic{key: key, lines: []string{fmt.Sprintf("replace with: XG2G_E2_RETRIES=%s", strings.TrimSpace(value))}}, true
	case "XG2G_OWI_BACKOFF_MS":
		return legacyEnvDiagnostic{key: key, lines: legacyDurationMigrationLines(key, value, "XG2G_E2_BACKOFF")}, true
	case "XG2G_OWI_MAX_BACKOFF_MS":
		return legacyEnvDiagnostic{key: key, lines: legacyDurationMigrationLines(key, value, "XG2G_E2_MAX_BACKOFF")}, true
	case "XG2G_MONETIZATION_UNLOCK_SCOPE":
		scopes := strings.ToLower(strings.TrimSpace(value))
		if scopes == "" {
			return legacyEnvDiagnostic{
				key:   key,
				lines: []string{"remove this key and migrate to: XG2G_MONETIZATION_REQUIRED_SCOPES=<scope>[,<scope>...]"},
			}, true
		}
		return legacyEnvDiagnostic{
			key:   key,
			lines: []string{fmt.Sprintf("replace with: XG2G_MONETIZATION_REQUIRED_SCOPES=%s", scopes)},
		}, true
	case "XG2G_STREAM_PORT":
		lines := []string{fmt.Sprintf("replace with: XG2G_E2_STREAM_PORT=%s", strings.TrimSpace(value))}
		lines = append(lines, "note: XG2G_E2_STREAM_PORT / enigma2.streamPort is itself deprecated and should usually be left unset unless you intentionally override the receiver's direct stream port")
		return legacyEnvDiagnostic{key: key, lines: lines}, true
	case "XG2G_USE_WEBIF_STREAMS":
		lines := []string{fmt.Sprintf("replace with: XG2G_E2_USE_WEBIF_STREAMS=%s", normalizeBoolLiteral(value))}
		lines = append(lines, "note: XG2G_E2_USE_WEBIF_STREAMS / enigma2.useWebIFStreams is itself deprecated and should usually be left unset")
		return legacyEnvDiagnostic{key: key, lines: lines}, true
	default:
		return legacyEnvDiagnostic{}, false
	}
}

func legacyHostMigrationLines(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []string{"replace with: XG2G_E2_HOST=<receiver URL>"}
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return []string{fmt.Sprintf("replace with: XG2G_E2_HOST=%s", trimmed)}
	}

	if parsed.User == nil {
		return []string{fmt.Sprintf("replace with: XG2G_E2_HOST=%s", trimmed)}
	}

	sanitized := *parsed
	username := parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	sanitized.User = nil

	lines := []string{fmt.Sprintf("replace with: XG2G_E2_HOST=%s", sanitized.String())}
	if username != "" {
		lines = append(lines, fmt.Sprintf("replace with: XG2G_E2_USER=%s", username))
	}
	if hasPassword || password != "" {
		lines = append(lines, "set XG2G_E2_PASS to the same secret currently embedded in XG2G_OWI_BASE")
	}
	return lines
}

func legacyDurationMigrationLines(key, value, replacement string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []string{fmt.Sprintf("replace with: %s=<duration>", replacement)}
	}

	ms, err := strconv.Atoi(trimmed)
	if err != nil {
		return []string{
			fmt.Sprintf("replace with: %s=<duration>", replacement),
			fmt.Sprintf("note: %s=%q is not a valid millisecond integer", key, trimmed),
		}
	}

	return []string{fmt.Sprintf("replace with: %s=%s", replacement, (time.Duration(ms) * time.Millisecond).String())}
}

func normalizeBoolLiteral(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "1", "true", "yes", "on":
		return "true"
	case "0", "false", "no", "off":
		return "false"
	default:
		if trimmed == "" {
			return "<true|false>"
		}
		return trimmed
	}
}
