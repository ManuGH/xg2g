package verification_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/verification"
)

func TestFileStore_Roundtrip(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "state.json")

	store, err := verification.NewFileStore(path)
	require.NoError(t, err)

	state := verification.DriftState{
		Detected:  true,
		LastCheck: time.Date(2026, 1, 25, 12, 0, 0, 0, time.UTC),
		Mismatches: []verification.Mismatch{
			{
				Kind:     verification.KindRuntime,
				Key:      "runtime.ffmpeg.version",
				Expected: "7.1.3",
				Actual:   "6.x",
			},
		},
	}

	ctx := context.Background()
	err = store.Set(ctx, state)
	require.NoError(t, err)

	// Verify persistence file content
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "runtime.ffmpeg.version")

	// Create new store instance to test load
	store2, err := verification.NewFileStore(path)
	require.NoError(t, err)

	got, ok := store2.Get(ctx)
	assert.True(t, ok)
	assert.Equal(t, state.LastCheck.Unix(), got.LastCheck.Unix()) // Compare unix to avoid monotonic clock diffs
	assert.Equal(t, state.Mismatches, got.Mismatches)
}

func TestFileStore_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "atomic.json")

	store, err := verification.NewFileStore(path)
	require.NoError(t, err)

	// Simulate concurrent reads during write could be verified with stress test,
	// but here we verify the file validity.

	hugeVal := make([]byte, 1024)
	for i := range hugeVal {
		hugeVal[i] = 'a'
	}

	state := verification.DriftState{
		Mismatches: []verification.Mismatch{
			{Kind: verification.KindConfig, Key: "long", Expected: string(hugeVal), Actual: "short"},
		},
	}

	err = store.Set(context.Background(), state)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Verify truncation happened
	var stored verification.DriftState
	err = json.Unmarshal(data, &stored)
	require.NoError(t, err)

	assert.Less(t, len(stored.Mismatches[0].Expected), 300)
	assert.Contains(t, stored.Mismatches[0].Expected, "...(truncated)")
}

func TestFileStore_Sanitization(t *testing.T) {
	store, err := verification.NewFileStore(filepath.Join(t.TempDir(), "test.json"))
	require.NoError(t, err)

	badState := verification.DriftState{
		Mismatches: []verification.Mismatch{
			{Kind: "invalid_kind", Key: "foo", Expected: "bar", Actual: "baz"},
		},
	}

	err = store.Set(context.Background(), badState)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mismatch kind")
}
