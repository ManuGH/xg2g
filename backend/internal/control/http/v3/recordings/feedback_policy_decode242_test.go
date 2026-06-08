// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package recordings

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/stretchr/testify/require"
)

// Code 242 is the HLS.js black-render decode failure. The windowed confidence
// engine must classify it as a decode warning (delegated to exported
// capreg.IsDecodeWarningCode), otherwise repeated black-screening never sets
// ConstraintNoProbeUp and the engine can keep probing up.
func TestConfidenceIsDecodeWarningCode_CountsBlackRender242(t *testing.T) {
	t.Run("capreg.IsDecodeWarningCode", func(t *testing.T) {
		require.True(t, capreg.IsDecodeWarningCode(103), "103 generic decode warning")
		require.True(t, capreg.IsDecodeWarningCode(242), "242 HLS.js black-render must count as decode")
		require.False(t, capreg.IsDecodeWarningCode(104), "104 is a network warning, not decode")
		require.False(t, capreg.IsDecodeWarningCode(0))
	})

	t.Run("via confidence engine", func(t *testing.T) {
		now := time.Now().UTC()
		obs := []capreg.PlaybackObservation{
			{ObservedAt: now.Add(-1 * time.Second), Outcome: "warning", FeedbackCode: 103},
		}
		windows := buildPlaybackConfidenceWindowsFromObservations(obs, runtimepolicy.WindowFeatures{}, now)
		total := 0
		for _, w := range windows {
			total += w.DecodeWarnings
		}
		require.Equal(t, 1, total, "103 decode warning must increment DecodeWarnings")
	})
}

func TestBuildConfidenceWindows_BlackRender242DrivesDecodeWarnings(t *testing.T) {
	now := time.Now().UTC()
	obs := []capreg.PlaybackObservation{
		{ObservedAt: now.Add(-1 * time.Second), Outcome: "warning", FeedbackCode: 242},
	}

	windows := buildPlaybackConfidenceWindowsFromObservations(obs, runtimepolicy.WindowFeatures{}, now)

	total := 0
	for _, w := range windows {
		total += w.DecodeWarnings
	}
	require.Equal(t, 1, total, "a 242 black-render warning must increment DecodeWarnings")
}
