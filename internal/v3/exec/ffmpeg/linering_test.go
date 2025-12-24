// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineRing(t *testing.T) {
	r := NewLineRing(3)

	// writes
	_, _ = fmt.Fprintf(r, "line1\n")
	_, _ = fmt.Fprintf(r, "line2\n")

	last := r.LastN(10)
	assert.Equal(t, []string{"line1", "line2"}, last)

	_, _ = fmt.Fprintf(r, "line3\n")
	last = r.LastN(10)
	assert.Equal(t, []string{"line1", "line2", "line3"}, last)

	// Wrap
	_, _ = fmt.Fprintf(r, "line4\n")
	last = r.LastN(10)
	assert.Equal(t, []string{"line2", "line3", "line4"}, last)

	last = r.LastN(2)
	assert.Equal(t, []string{"line3", "line4"}, last)
}

func TestLineRing_Partial(t *testing.T) {
	r := NewLineRing(5)
	_, _ = r.Write([]byte("foo\nbar\n"))

	last := r.LastN(10)
	assert.Equal(t, []string{"foo", "bar"}, last)
}
