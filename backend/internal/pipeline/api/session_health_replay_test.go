package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sessionHealthReplayFixture struct {
	Name     string                       `json:"name"`
	Context  SessionPlaybackHealthContext `json:"context"`
	Trace    *model.PlaybackTrace         `json:"trace"`
	Expected struct {
		Health      SessionPlaybackHealth `json:"health"`
		ReasonCodes []string              `json:"reasonCodes"`
	} `json:"expected"`
}

func TestSessionPlaybackHealthReplayFixtures(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "session_health")
	entries, err := os.ReadDir(fixturesDir)
	require.NoError(t, err)

	var fixtureNames []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		fixtureNames = append(fixtureNames, entry.Name())
	}
	sort.Strings(fixtureNames)
	require.NotEmpty(t, fixtureNames)

	for _, fixtureName := range fixtureNames {
		t.Run(fixtureName, func(t *testing.T) {
			fixturePath := filepath.Join(fixturesDir, fixtureName)
			raw, err := os.ReadFile(fixturePath)
			require.NoError(t, err)

			var fixture sessionHealthReplayFixture
			require.NoError(t, json.Unmarshal(raw, &fixture))

			got := DeriveSessionPlaybackHealth(fixture.Trace, fixture.Context)

			assert.Equal(t, fixture.Expected.Health, got.Health)
			assert.Equal(t, fixture.Expected.ReasonCodes, got.ReasonCodes)
		})
	}
}
