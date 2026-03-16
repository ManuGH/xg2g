// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/require"
)

func TestProblemSpecForAPIError_UsesRegistryProblemType(t *testing.T) {
	got := problemSpecForAPIError(ErrV3StoreNotInitialized, "")

	require.Equal(t, "error/v3_unavailable", got.problemType)
	require.Equal(t, problemcode.CodeV3Unavailable, got.code)
	require.Equal(t, "v3 store not initialized", got.title)
}

func TestProblemSpecForCode_SessionGoneUsesCustomProblemType(t *testing.T) {
	got := problemSpecForCode(problemcode.CodeSessionGone, "", "terminal")

	require.Equal(t, "urn:xg2g:error:session:gone", got.problemType)
	require.Equal(t, problemcode.CodeSessionGone, got.code)
	require.Equal(t, "Session Gone", got.title)
	require.Equal(t, "terminal", got.detail)
}

func TestV3APIErrors_NoInlineLiteralsOutsideRegistry(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	inlineAPIError := regexp.MustCompile(`&APIError\s*\{`)
	for _, path := range files {
		if filepath.Base(path) == "error_registry.go" || filepath.Ext(path) != ".go" {
			continue
		}
		if matched, _ := filepath.Match("*_test.go", filepath.Base(path)); matched {
			continue
		}

		data, readErr := os.ReadFile(filepath.Clean(path))
		require.NoError(t, readErr)

		if inlineAPIError.Match(data) {
			t.Fatalf("%s contains inline APIError literal; use newRegisteredAPIError or registry-backed vars", path)
		}
	}
}
