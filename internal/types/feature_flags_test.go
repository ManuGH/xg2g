// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
//
//nolint:revive,nolintlint // Package name "types" is standard for shared type definitions
package types

import "testing"

func TestFeatureFlag_String(t *testing.T) {
	tests := []struct {
		name string
		flag FeatureFlag
		want string
	}{
		{"audio transcoding", FeatureFlagAudioTranscoding, "AUDIO_TRANSCODING"},
		{"GPU transcode", FeatureFlagGPUTranscode, "GPU_TRANSCODE"},
		{"EPG", FeatureFlagEPG, "EPG"},
		{"API v2", FeatureFlagAPIv2, "APIv2"},
		{"telemetry", FeatureFlagTelemetry, "TELEMETRY"},
		{"metrics", FeatureFlagMetrics, "METRICS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.flag.String(); got != tt.want {
				t.Errorf("FeatureFlag.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFeatureFlag_EnvVarName(t *testing.T) {
	tests := []struct {
		name string
		flag FeatureFlag
		want string
	}{
		{"audio transcoding", FeatureFlagAudioTranscoding, "XG2G_AUDIO_TRANSCODING_ENABLED"},
		{"GPU transcode", FeatureFlagGPUTranscode, "XG2G_GPU_TRANSCODE_ENABLED"},
		{"EPG", FeatureFlagEPG, "XG2G_EPG_ENABLED"},
		{"API v2", FeatureFlagAPIv2, "XG2G_APIv2_ENABLED"},
		{"telemetry", FeatureFlagTelemetry, "XG2G_TELEMETRY_ENABLED"},
		{"metrics", FeatureFlagMetrics, "XG2G_METRICS_ENABLED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.flag.EnvVarName(); got != tt.want {
				t.Errorf("FeatureFlag.EnvVarName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFeatureFlag_IsValid(t *testing.T) {
	tests := []struct {
		name string
		flag FeatureFlag
		want bool
	}{
		{"audio transcoding valid", FeatureFlagAudioTranscoding, true},
		{"GPU transcode valid", FeatureFlagGPUTranscode, true},
		{"EPG valid", FeatureFlagEPG, true},
		{"API v2 valid", FeatureFlagAPIv2, true},
		{"telemetry valid", FeatureFlagTelemetry, true},
		{"metrics valid", FeatureFlagMetrics, true},
		{"invalid empty", FeatureFlag(""), false},
		{"invalid unknown", FeatureFlag("UNKNOWN"), false},
		{"invalid lowercase", FeatureFlag("epg"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.flag.IsValid(); got != tt.want {
				t.Errorf("FeatureFlag.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllFeatureFlags(t *testing.T) {
	flags := AllFeatureFlags()

	if len(flags) != 6 {
		t.Errorf("AllFeatureFlags() returned %d flags, want 6", len(flags))
	}

	// Verify all expected flags are present
	expected := []FeatureFlag{
		FeatureFlagAudioTranscoding,
		FeatureFlagGPUTranscode,
		FeatureFlagEPG,
		FeatureFlagAPIv2,
		FeatureFlagTelemetry,
		FeatureFlagMetrics,
	}

	for _, exp := range expected {
		found := false
		for _, flag := range flags {
			if flag == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllFeatureFlags() missing %v", exp)
		}
	}
}
