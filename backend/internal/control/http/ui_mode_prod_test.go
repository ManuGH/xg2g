//go:build !dev

package http

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetermineUIModeProdBuild(t *testing.T) {
	require.Equal(t, UIModeProdStatic, DetermineUIMode("", "http://127.0.0.1:5173"))
	require.Equal(t, UIModeProdStatic, DetermineUIMode("/tmp/ui", ""))
}
