package channels

import (
	"os"
	"strings"
	"testing"
)

// L21: Save writes channels.json atomically (unique temp in the SAME directory + rename) so
// a torn write cannot brick startup. This exercises the happy path end-to-end (SetEnabled →
// Save → Load round-trip) and asserts the atomic temp does not linger.
func TestManager_SaveAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	if err := m.SetEnabled("chan-1", false); err != nil { // disable → persists via Save
		t.Fatalf("set enabled: %v", err)
	}

	m2 := NewManager(dir)
	if err := m2.Load(); err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if m2.IsEnabled("chan-1") {
		t.Fatal("chan-1 must be disabled after a Save/Load round-trip")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("atomic temp file left behind after Save: %q", e.Name())
		}
	}
}
