package decision

import "github.com/ManuGH/xg2g/internal/normalize"

const (
	hostPerformanceClassLow    = "low"
	hostPerformanceClassMedium = "medium"
	hostPerformanceClassHigh   = "high"
	hostPerformanceClassUltra  = "ultra"
	hostBenchmarkClassWeak     = "weak"
	hostBenchmarkClassModerate = "moderate"
	hostBenchmarkClassStrong   = "strong"
	bitrateConfidenceLow       = "low"
	bitrateConfidenceMedium    = "medium"
	bitrateConfidenceHigh      = "high"
)

func normalizeHostPerformanceClass(raw string) string {
	switch normalize.Token(raw) {
	case hostPerformanceClassLow, hostPerformanceClassMedium, hostPerformanceClassHigh, hostPerformanceClassUltra:
		return normalize.Token(raw)
	default:
		return ""
	}
}

func normalizeHostBenchmarkClass(raw string) string {
	switch normalize.Token(raw) {
	case hostBenchmarkClassWeak, hostBenchmarkClassModerate, hostBenchmarkClassStrong:
		return normalize.Token(raw)
	default:
		return ""
	}
}

func normalizeBitrateConfidence(raw string) string {
	switch normalize.Token(raw) {
	case bitrateConfidenceLow, bitrateConfidenceMedium, bitrateConfidenceHigh:
		return normalize.Token(raw)
	default:
		return ""
	}
}
