// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTranscodeSharpenFilter(t *testing.T) {
	t.Run("default is a clean CAS strength", func(t *testing.T) {
		assert.Equal(t, "cas=strength=0.50", transcodeSharpenFilter())
	})

	t.Run("zero disables sharpening", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0")
		assert.Equal(t, "", transcodeSharpenFilter())
	})

	t.Run("tunable strength", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0.7")
		assert.Equal(t, "cas=strength=0.70", transcodeSharpenFilter())
	})

	t.Run("clamped to 1.0 (above amplifies noise)", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "2.0")
		assert.Equal(t, "cas=strength=1.00", transcodeSharpenFilter())
	})
}
