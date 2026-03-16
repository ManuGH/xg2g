// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package problemcode

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMustResolve_UsesRegistryDefaults(t *testing.T) {
	got := MustResolve(CodeV3Unavailable, "")

	require.Equal(t, "error/v3_unavailable", got.ProblemType)
	require.Equal(t, CodeV3Unavailable, got.Code)
	require.Equal(t, "v3 control plane not enabled", got.Title)
}

func TestResolve_SessionGoneUsesCustomProblemType(t *testing.T) {
	got := Resolve(CodeSessionGone, "")

	require.Equal(t, "urn:xg2g:error:session:gone", got.ProblemType)
	require.Equal(t, CodeSessionGone, got.Code)
	require.Equal(t, "Session Gone", got.Title)
}

func TestResolve_UnknownCodeFallsBackToErrorPath(t *testing.T) {
	got := Resolve("CUSTOM_CODE", "")

	require.Equal(t, "error/custom_code", got.ProblemType)
	require.Equal(t, "CUSTOM_CODE", got.Code)
	require.Equal(t, "CUSTOM_CODE", got.Title)
}

func TestResolve_DecisionCodesUseCustomProblemType(t *testing.T) {
	got := MustResolve(CodeCapabilitiesInvalid, "")

	require.Equal(t, "recordings/capabilities-invalid", got.ProblemType)
	require.Equal(t, CodeCapabilitiesInvalid, got.Code)
	require.Equal(t, "Capabilities Invalid", got.Title)
}

func TestPublicEntries_ExcludeInternalJobCodes(t *testing.T) {
	codes := make([]string, 0, len(PublicEntries()))
	for _, entry := range PublicEntries() {
		codes = append(codes, entry.Code)
	}

	require.NotEmpty(t, codes)
	require.False(t, slices.Contains(codes, CodeJobConfigInvalid))
	require.True(t, slices.Contains(codes, CodeTranscodeStalled))
	require.True(t, slices.Contains(codes, CodeCapabilitiesMissing))
}

func TestPublicEntries_DescriptionsArePopulated(t *testing.T) {
	entries := PublicEntries()
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		require.NotEmpty(t, entry.Description, "description missing for %s", entry.Code)
		require.NotEmpty(t, entry.OperatorHint, "operator hint missing for %s", entry.Code)
		require.NotEmpty(t, entry.Severity, "severity missing for %s", entry.Code)
	}

	transcode := MustLookup(CodeTranscodeStalled)
	require.Contains(t, transcode.Description, "watchdog")
	require.Contains(t, transcode.OperatorHint, "FFmpeg")
	require.Equal(t, SeverityError, transcode.Severity)
	require.True(t, transcode.Retryable)
	require.Equal(t, "docs/ops/OBSERVABILITY.md", transcode.RunbookURL)

	invariant := MustLookup(CodeInvariantViolation)
	require.Equal(t, SeverityCritical, invariant.Severity)
	require.False(t, invariant.Retryable)
}
