// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//nolint:revive // Package name "types" is standard for shared type definitions
//nolint:revive,nolintlint // Package name "types" is standard for shared type definitions
package types

// FeatureFlag represents a feature toggle in the application.
type FeatureFlag string

// Feature flag constants define all available feature toggles.
const (
	// FeatureFlagAudioTranscoding enables audio transcoding via FFmpeg.
	FeatureFlagAudioTranscoding FeatureFlag = "AUDIO_TRANSCODING"

	// FeatureFlagEPG enables Electronic Program Guide fetching.
	FeatureFlagEPG FeatureFlag = "EPG"

	// FeatureFlagTelemetry enables OpenTelemetry distributed tracing.
	FeatureFlagTelemetry FeatureFlag = "TELEMETRY"

	// FeatureFlagMetrics enables Prometheus metrics collection.
	FeatureFlagMetrics FeatureFlag = "METRICS"
)

// String implements fmt.Stringer.
func (f FeatureFlag) String() string {
	return string(f)
}

// EnvVarName returns the environment variable name for this feature flag.
//
// Example: FeatureFlagEPG.EnvVarName() returns "XG2G_EPG_ENABLED"
func (f FeatureFlag) EnvVarName() string {
	return "XG2G_" + string(f) + "_ENABLED"
}

// IsValid checks whether the feature flag is defined.
func (f FeatureFlag) IsValid() bool {
	switch f {
	case FeatureFlagAudioTranscoding, FeatureFlagEPG,
		FeatureFlagTelemetry, FeatureFlagMetrics:
		return true
	default:
		return false
	}
}

// AllFeatureFlags returns all defined feature flags.
func AllFeatureFlags() []FeatureFlag {
	return []FeatureFlag{
		FeatureFlagAudioTranscoding,
		FeatureFlagEPG,
		FeatureFlagTelemetry,
		FeatureFlagMetrics,
	}
}
