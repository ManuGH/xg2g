// internal/mapper/normalize.go
package mapper

import (
	"regexp"
	"strings"
)

func NormalizeChannelName(name string) string {
	// Lowercase, Trim
	normalized := strings.ToLower(strings.TrimSpace(name))
	
	// Entferne HD/UHD/Region-Suffixe mit Regex
	re := regexp.MustCompile(`\s+(hd|uhd|4k|austria|österreich|oesterreich|at|de|ch)$`)
	normalized = re.ReplaceAllString(normalized, "")
	
	// Kollabiere Mehrfach-Spaces
	re = regexp.MustCompile(`\s+`)
	normalized = re.ReplaceAllString(normalized, " ")
	
	return strings.TrimSpace(normalized)
}
