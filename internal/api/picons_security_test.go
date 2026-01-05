// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePiconRefRejectsTraversal(t *testing.T) {
	tests := []string{
		"../x",
		"..%2f",
		"%2e%2e%2f",
		"/abs",
		"%2Fabs",
		"a/b",
		`a\b`,
		string([]byte{0x00, 0x2e}),
	}

	for _, input := range tests {
		_, err := parsePiconRef(input)
		require.Error(t, err, "expected rejection for %q", input)
	}
}

func TestParsePiconRefAcceptsSafe(t *testing.T) {
	ref, err := parsePiconRef("1:0:1:abcd_efgh")
	require.NoError(t, err)
	require.Equal(t, "1:0:1:abcd_efgh", ref)
}
