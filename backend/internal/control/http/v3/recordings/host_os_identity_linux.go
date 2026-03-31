//go:build linux

package recordings

import (
	"os"
	"strings"
)

func loadHostOSIdentity() (string, string) {
	payload, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux", ""
	}

	values := make(map[string]string)
	for _, rawLine := range strings.Split(string(payload), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}

	name := firstNonEmptyHostValue(values["ID"], values["NAME"], values["PRETTY_NAME"], "linux")
	version := firstNonEmptyHostValue(values["VERSION_ID"], values["VERSION"])
	return name, version
}

func firstNonEmptyHostValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
