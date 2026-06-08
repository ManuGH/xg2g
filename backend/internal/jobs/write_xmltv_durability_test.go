// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/require"
)

// writeXMLTV must atomically replace an existing file with a COMPLETE document —
// never a 0-byte or partial file, and never leave temp/pending leftovers.
//
// The actual durability fix (fsync lands on the real data, not an orphaned temp
// inode) is by construction: the content is written into renameio's PendingFile,
// whose CloseAtomicallyReplace fsyncs it — see writeXMLTV. That fsync behavior is
// not unit-observable (would need a real power loss); this test locks the
// atomicity + completeness contract, and epg.TestWriteXMLTVTo_PropagatesWriterError
// locks that a partial write surfaces as an error so the commit is skipped.
func TestWriteXMLTV_AtomicReplaceProducesCompleteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guide.xml")
	// pre-existing OLD file that the durable write must fully replace
	require.NoError(t, os.WriteFile(path, []byte("OLD-INCOMPLETE"), 0o600))

	tv := epg.TV{
		Generator: "xg2g",
		Channels:  []epg.Channel{{ID: "c1", DisplayName: []string{"Chan1"}}},
		Programs: []epg.Programme{
			{Start: "202501010000 +0000", Stop: "202501010100 +0000", Channel: "c1", Title: epg.Title{Text: "Show1"}},
		},
	}

	require.NoError(t, writeXMLTV(context.Background(), path, tv))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, data, "target must never be 0-byte")

	s := string(data)
	require.Contains(t, s, "<?xml")
	require.Contains(t, s, "<tv")
	require.Contains(t, s, "Show1")
	require.NotContains(t, s, "OLD-INCOMPLETE", "old content must be fully replaced, never partially overwritten")

	// renameio committed atomically: only the target remains, no temp/pending leftovers
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp/pending leftovers beside the target")
	require.Equal(t, "guide.xml", entries[0].Name())
}
