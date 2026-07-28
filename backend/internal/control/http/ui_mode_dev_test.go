//go:build dev

package http

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetermineUIModeDevBuild(t *testing.T) {
	require.Equal(t, UIModeDevDir, DetermineUIMode("/tmp/ui", ""))
	require.Equal(t, UIModeDevProxy, DetermineUIMode("", "http://127.0.0.1:5173"))
	require.Equal(t, UIModeDevProxy, DetermineUIMode(" \t", ""))
}
