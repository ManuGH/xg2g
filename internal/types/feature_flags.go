// SPDX-License-Identifier: MIT

//nolint:revive // Package name "types" is standard for shared type definitions
//nolint:revive,nolintlint // Package name "types" is standard for shared type definitions
package types

// FeatureFlag represents a feature toggle in the application.
type FeatureFlag string

// Feature flag constants define all available feature toggles.
const (
	// FeatureFlagAudioTranscoding enables audio transcoding via FFmpeg.
	FeatureFlagAudioTranscoding FeatureFlag = "AUDIO_TRANSCODING"

	// FeatureFlagGPUTranscode enables GPU-accelerated video transcoding.
	FeatureFlagGPUTranscode FeatureFlag = "GPU_TRANSCODE"

	// FeatureFlagEPG enables Electronic Program Guide fetching.
	FeatureFlagEPG FeatureFlag = "EPG"

	// FeatureFlagAPIv2 enables experimental API v2 endpoints.
	FeatureFlagAPIv2 FeatureFlag = "APIv2"

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
	case FeatureFlagAudioTranscoding, FeatureFlagGPUTranscode, FeatureFlagEPG,
		FeatureFlagAPIv2, FeatureFlagTelemetry, FeatureFlagMetrics:
		return true
	default:
		return false
	}
}

// AllFeatureFlags returns all defined feature flags.
func AllFeatureFlags() []FeatureFlag {
	return []FeatureFlag{
		FeatureFlagAudioTranscoding,
		FeatureFlagGPUTranscode,
		FeatureFlagEPG,
		FeatureFlagAPIv2,
		FeatureFlagTelemetry,
		FeatureFlagMetrics,
	}
}
