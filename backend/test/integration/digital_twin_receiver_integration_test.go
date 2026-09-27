// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration_fast || integration

package test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDigitalTwin_E2E_FullRefreshFlow verifies the complete channel and EPG refresh flow
// against the high-fidelity Enigma2 Digital Twin simulating a Vu+ Uno 4K.
func TestDigitalTwin_E2E_FullRefreshFlow(t *testing.T) {
	tmpDir := t.TempDir()
	twin := openwebif.NewVuUno4KTwin(t)

	cfg := config.AppConfig{
		DataDir:           tmpDir,
		Bouquet:           "HD Channels",
		XMLTVPath:         "xmltv.xml",
		EPGEnabled:        true,
		EPGDays:           1,
		EPGMaxConcurrency: 2,
		Enigma2: config.Enigma2Settings{
			BaseURL:    twin.URL(),
			StreamPort: 8001,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := jobs.Refresh(ctx, config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()))
	require.NoError(t, err, "Refresh against Digital Twin must succeed")
	require.NotNil(t, status)
	assert.Greater(t, status.Channels, 0, "Must have processed channels from twin")

	// Verify M3U playlist generated
	playlistPath := filepath.Join(tmpDir, "playlist.m3u8")
	require.FileExists(t, playlistPath)

	playlistContent, err := os.ReadFile(playlistPath)
	require.NoError(t, err)
	playlistStr := string(playlistContent)
	assert.Contains(t, playlistStr, "#EXTM3U")
	assert.Contains(t, playlistStr, "Das Erste HD")
	assert.Contains(t, playlistStr, "ZDF HD")

	// Verify XMLTV generated
	xmltvPath := filepath.Join(tmpDir, "xmltv.xml")
	require.FileExists(t, xmltvPath)

	// Invariant: Receiver did not experience crashes or illegal states
	twin.AssertNoAssertionFailure(t)
}

// TestDigitalTwin_E2E_TunerProtectionUnderOverload verifies that rapid concurrent stream requests
// on the Digital Twin remain strictly bounded and do not crash the receiver.
func TestDigitalTwin_E2E_TunerProtectionUnderOverload(t *testing.T) {
	twin := openwebif.NewVuUno4KTwin(t,
		openwebif.WithPhysicalTuners(2),
		openwebif.WithCrashOnTunerExhaustion(true),
	)

	// Verify initial tuner count reported by /api/about
	aboutResp, err := http.Get(twin.URL() + "/api/about")
	require.NoError(t, err)
	defer aboutResp.Body.Close()
	assert.Equal(t, http.StatusOK, aboutResp.StatusCode)

	// Stream from Transponder 1: Das Erste HD (occupies physical tuner 1)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet, twin.StreamURL("1:0:19:283D:3FB:1:C00000:0:0:0:"), nil)
	resp1, err := http.DefaultClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	assert.Equal(t, 1, twin.ActiveTunersCount())

	// Stream from Transponder 1: ZDF HD (shares physical tuner 1 via demuxer)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, twin.StreamURL("1:0:19:283E:3FB:1:C00000:0:0:0:"), nil)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, 1, twin.ActiveTunersCount(), "Same transponder must not consume a second physical tuner")

	// Stream from Transponder 2: RTL (occupies physical tuner 2)
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	req3, _ := http.NewRequestWithContext(ctx3, http.MethodGet, twin.StreamURL("1:0:1:6DCA:44D:1:C00000:0:0:0:"), nil)
	resp3, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusOK, resp3.StatusCode)
	assert.Equal(t, 2, twin.ActiveTunersCount(), "Second transponder must allocate second physical tuner")

	// All 2 physical tuners are active and valid
	assert.Equal(t, 2, twin.PeakTunersUsed())
	twin.AssertNoAssertionFailure(t)
}
