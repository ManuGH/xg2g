// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"encoding/base64"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildArgs_StripsURLUserinfoFromArgv verifies that credentials embedded in
// the input URL are moved to the Authorization header and removed from the URL
// handed to ffmpeg's -i, so they cannot leak via /proc/<pid>/cmdline or a logged
// command line.
func TestBuildArgs_StripsURLUserinfoFromArgv(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "creds-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Source: ports.StreamSource{
			ID:   "http://user:secret@box.local/stream.ts",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	input, ok := valueAfter(args, "-i")
	require.True(t, ok, "expected an -i input arg")
	assert.Equal(t, "http://box.local/stream.ts", input, "-i URL must not carry userinfo")

	for _, a := range args {
		assert.NotContains(t, a, "secret", "no ffmpeg arg may contain the raw password")
	}

	headers, ok := valueAfter(args, "-headers")
	require.True(t, ok, "expected a -headers arg")
	want := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))
	assert.Contains(t, headers, want, "credentials must still be passed via the Authorization header")
}
