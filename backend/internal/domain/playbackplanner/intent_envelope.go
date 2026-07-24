// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackplanner

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

var (
	ErrInvalidIntentSignature = errors.New("invalid playback intent signature")
	ErrIntentExpired          = errors.New("playback intent has expired")
	ErrMissingSigningKey      = errors.New("signing key cannot be empty")
)

// IntentMode distinguishes live playback intents from VOD recording intents.
type IntentMode string

const (
	IntentModeLive IntentMode = "live"
	IntentModeVOD  IntentMode = "vod"
)

// PlaybackIntent represents a unified, planner-issued playback decision envelope.
type PlaybackIntent struct {
	Mode          IntentMode                             `json:"mode"`
	SessionID     string                                 `json:"session_id,omitempty"`
	RefID         string                                 `json:"ref_id"`
	VariantHash   string                                 `json:"variant_hash"`
	TargetProfile *playbackprofile.TargetPlaybackProfile `json:"target_profile"`
	ExpiresAt     time.Time                              `json:"expires_at"`
	Signature     string                                 `json:"signature,omitempty"`
}

// SignPlaybackIntent computes the HMAC-SHA256 signature for a PlaybackIntent.
func SignPlaybackIntent(intent *PlaybackIntent, secretKey []byte) (string, error) {
	if len(secretKey) == 0 {
		return "", ErrMissingSigningKey
	}
	if intent == nil {
		return "", fmt.Errorf("intent cannot be nil")
	}

	payload, err := canonicalPayload(intent)
	if err != nil {
		return "", fmt.Errorf("marshal canonical payload: %w", err)
	}

	mac := hmac.New(sha256.New, secretKey)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyPlaybackIntent checks the signature and expiration of a PlaybackIntent.
func VerifyPlaybackIntent(intent *PlaybackIntent, secretKey []byte, now time.Time) error {
	if intent == nil {
		return fmt.Errorf("intent cannot be nil")
	}
	if len(secretKey) == 0 {
		return ErrMissingSigningKey
	}
	if !now.IsZero() && !intent.ExpiresAt.IsZero() && now.After(intent.ExpiresAt) {
		return ErrIntentExpired
	}

	expectedSig, err := SignPlaybackIntent(intent, secretKey)
	if err != nil {
		return fmt.Errorf("compute signature: %w", err)
	}

	if !hmac.Equal([]byte(intent.Signature), []byte(expectedSig)) {
		return ErrInvalidIntentSignature
	}

	return nil
}

func canonicalPayload(intent *PlaybackIntent) ([]byte, error) {
	type canonicalIntent struct {
		Mode          IntentMode                             `json:"mode"`
		SessionID     string                                 `json:"session_id,omitempty"`
		RefID         string                                 `json:"ref_id"`
		VariantHash   string                                 `json:"variant_hash"`
		TargetProfile *playbackprofile.TargetPlaybackProfile `json:"target_profile"`
		ExpiresAtUnix int64                                  `json:"expires_at_unix"`
	}

	c := canonicalIntent{
		Mode:          intent.Mode,
		SessionID:     intent.SessionID,
		RefID:         intent.RefID,
		VariantHash:   intent.VariantHash,
		TargetProfile: intent.TargetProfile,
		ExpiresAtUnix: intent.ExpiresAt.Unix(),
	}

	return json.Marshal(c)
}
