package v3

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachResumeSummaries(t *testing.T) {
	cfg := config.AppConfig{
		DataDir: t.TempDir(),
	}
	s := NewServer(cfg, nil, nil)

	rs := resume.NewMemoryStore()
	// No bus or v3 store needed for this isolated test
	// Wire dependencies
	s.SetDependencies(
		nil,
		nil,
		rs,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	recID := "dGVzdC1yZWNvcmRpbmctbG9uZy1lbm91Z2g"
	principalID := "test-user-id"

	// 1. Seed Resume State
	err := rs.Put(context.Background(), principalID, recID, &resume.State{
		PosSeconds:      120,
		DurationSeconds: 3600,
		Finished:        false,
		UpdatedAt:       time.Now(),
		Fingerprint:     "fp",
	})
	require.NoError(t, err)

	// 2. Prepare Items
	idPtr := func(s string) *string { return &s }
	items := []RecordingItem{
		{
			RecordingId: idPtr(recID),
			Title:       idPtr("Matching Recording"),
		},
		{
			RecordingId: idPtr("bG9uZy1lbm91Z2gtdmFsaWQtaWQ"), // valid-length (>=16) but non-matching
			Title:       idPtr("Other Recording"),
		},
	}

	// 3. Attach
	// Note: We test the unexported method via internal package access
	s.attachResumeSummaries(context.Background(), principalID, items)

	// 4. Verify
	require.NotNil(t, items[0].Resume, "Resume should be attached to matching item")
	assert.Equal(t, int64(120), *items[0].Resume.PosSeconds)
	assert.Equal(t, int64(3600), *items[0].Resume.DurationSeconds)
	assert.Equal(t, false, *items[0].Resume.Finished)
	assert.NotEmpty(t, items[0].Resume.UpdatedAt, "updated_at should be set")

	require.Nil(t, items[1].Resume, "Resume should not be attached to non-matching item")
}
