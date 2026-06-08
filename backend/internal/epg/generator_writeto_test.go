// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteXMLTVTo_WritesCompleteParseableDocument(t *testing.T) {
	tv := TV{
		Generator: "xg2g",
		Channels:  []Channel{{ID: "c1", DisplayName: []string{"Chan1"}}},
		Programs: []Programme{
			{Start: "202501010000 +0000", Stop: "202501010100 +0000", Channel: "c1", Title: Title{Text: "Show1"}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, WriteXMLTVTo(&buf, tv))

	s := buf.String()
	require.Contains(t, s, "<?xml")
	require.Contains(t, s, `<!DOCTYPE tv`)
	require.Contains(t, s, `id="c1"`)
	require.Contains(t, s, "Show1")

	// the document round-trips through the XML decoder without error
	var got TV
	require.NoError(t, xml.NewDecoder(&buf).Decode(&got))
	require.Len(t, got.Channels, 1)
	require.Len(t, got.Programs, 1)
}

// failOnNthWrite fails on the Nth Write call onward, simulating a disk error.
// WriteXMLTVTo buffers via bufio and flushes once, so the underlying writer sees
// a single write — n=1 makes that flush fail, exercising the error path.
type failOnNthWrite struct {
	n, calls int
}

func (f *failOnNthWrite) Write(p []byte) (int, error) {
	f.calls++
	if f.calls >= f.n {
		return 0, errors.New("simulated write failure")
	}
	return len(p), nil
}

func TestWriteXMLTVTo_PropagatesWriterError(t *testing.T) {
	// A write failure MUST surface as an error so the caller (renameio
	// CloseAtomicallyReplace) never commits a partial / 0-byte file.
	tv := TV{
		Channels: []Channel{{ID: "c1", DisplayName: []string{"Chan1"}}},
		Programs: []Programme{{Start: "1", Stop: "2", Channel: "c1", Title: Title{Text: "x"}}},
	}
	require.Error(t, WriteXMLTVTo(&failOnNthWrite{n: 1}, tv))
}
