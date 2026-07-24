// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackplanner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestSignAndVerifyPlaybackIntent(t *testing.T) {
	secret := []byte("secret_key_32_bytes_long_123456789")
	now := time.Now()

	profile := &playbackprofile.TargetPlaybackProfile{
		Video: playbackprofile.VideoTarget{Mode: "copy", Codec: "h264"},
		Audio: playbackprofile.AudioTarget{Mode: "copy", Codec: "aac"},
	}

	intent := &PlaybackIntent{
		Mode:          IntentModeVOD,
		SessionID:     "sess_123",
		RefID:         "rec_456",
		VariantHash:   "var_hash",
		TargetProfile: profile,
		ExpiresAt:     now.Add(1 * time.Hour),
	}

	t.Run("successful sign and verify", func(t *testing.T) {
		sig, err := SignPlaybackIntent(intent, secret)
		require.NoError(t, err)
		require.NotEmpty(t, sig)

		intent.Signature = sig
		err = VerifyPlaybackIntent(intent, secret, now)
		assert.NoError(t, err)
	})

	t.Run("tampered signature fails", func(t *testing.T) {
		intent.Signature = "bad_signature"
		err := VerifyPlaybackIntent(intent, secret, now)
		assert.ErrorIs(t, err, ErrInvalidIntentSignature)
	})

	t.Run("expired intent fails", func(t *testing.T) {
		expiredIntent := &PlaybackIntent{
			Mode:        IntentModeLive,
			RefID:       "channel_1",
			VariantHash: "var_1",
			ExpiresAt:   now.Add(-10 * time.Minute),
		}
		sig, err := SignPlaybackIntent(expiredIntent, secret)
		require.NoError(t, err)
		expiredIntent.Signature = sig

		err = VerifyPlaybackIntent(expiredIntent, secret, now)
		assert.ErrorIs(t, err, ErrIntentExpired)
	})
}
