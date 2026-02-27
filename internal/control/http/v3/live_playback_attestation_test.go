package v3

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLivePlaybackDecisionTokenAccepted(t *testing.T) {
	s := &Server{
		liveDecisionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
		liveDecisionTTL:        90 * time.Second,
	}

	token := s.attestLivePlaybackDecision("req-1", "user-1", "1:0:1:100:200:300:0:0:0:0:", "hlsjs")
	require.NotEmpty(t, token)
	assert.True(t, s.verifyLivePlaybackDecision(token, "user-1", "1:0:1:100:200:300:0:0:0:0:", "hlsjs"))
}

func TestLivePlaybackDecisionTokenTamperedRejected(t *testing.T) {
	s := &Server{
		liveDecisionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
		liveDecisionTTL:        90 * time.Second,
	}

	token := s.attestLivePlaybackDecision("req-2", "user-2", "1:0:1:101:201:301:0:0:0:0:", "native_hls")
	require.NotEmpty(t, token)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	parts[2] = strings.Repeat("A", len(parts[2]))
	tampered := strings.Join(parts, ".")

	assert.False(t, s.verifyLivePlaybackDecision(tampered, "user-2", "1:0:1:101:201:301:0:0:0:0:", "native_hls"))
}

func TestLivePlaybackDecisionTokenExpiredRejected(t *testing.T) {
	s := &Server{
		liveDecisionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
		liveDecisionTTL:        90 * time.Second,
	}

	token := s.attestLivePlaybackDecision("req-3", "user-3", "1:0:1:102:202:302:0:0:0:0:", "transcode")
	require.NotEmpty(t, token)

	expired := rewriteLivePlaybackDecisionToken(t, s, token, func(claims *livePlaybackDecisionClaims) {
		now := time.Now().UTC().Unix()
		claims.IssuedAt = now - 30
		claims.ExpiresAt = now - 1
	})

	assert.False(t, s.verifyLivePlaybackDecision(expired, "user-3", "1:0:1:102:202:302:0:0:0:0:", "transcode"))
}

func TestLivePlaybackDecisionTokenClaimMismatchRejected(t *testing.T) {
	s := &Server{
		liveDecisionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
		liveDecisionTTL:        90 * time.Second,
	}

	token := s.attestLivePlaybackDecision("req-4", "user-4", "1:0:1:103:203:303:0:0:0:0:", "hlsjs")
	require.NotEmpty(t, token)

	assert.False(t, s.verifyLivePlaybackDecision(token, "user-other", "1:0:1:103:203:303:0:0:0:0:", "hlsjs"))
	assert.False(t, s.verifyLivePlaybackDecision(token, "user-4", "1:0:1:999:203:303:0:0:0:0:", "hlsjs"))
	assert.False(t, s.verifyLivePlaybackDecision(token, "user-4", "1:0:1:103:203:303:0:0:0:0:", "native_hls"))
}

func TestLivePlaybackDecisionTokenWithKeyRotationAcceptedWithinWindow(t *testing.T) {
	const (
		activeSecret = "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
		legacySecret = "abcdefghijklmnopqrstuvwxyz0123456789ABCDE2"
	)
	now := time.Now().UTC()
	ring := resolveLiveDecisionKeyring(config.AppConfig{
		PlaybackDecisionSecret:         activeSecret,
		PlaybackDecisionKeyID:          "kid-active",
		PlaybackDecisionPreviousKeys:   []string{"kid-legacy:" + legacySecret},
		PlaybackDecisionRotationWindow: 5 * time.Minute,
	}, now)

	s := &Server{
		liveDecisionKeyring: ring,
		liveDecisionTTL:     90 * time.Second,
	}

	token := buildLivePlaybackDecisionToken(t, []byte(legacySecret), livePlaybackDecisionClaims{
		Principal:  "user-rotate",
		ServiceRef: "1:0:1:104:204:304:0:0:0:0:",
		Mode:       "hlsjs",
		KeyID:      "kid-legacy",
		IssuedAt:   now.Unix() - 5,
		ExpiresAt:  now.Unix() + 60,
	})

	assert.True(t, s.verifyLivePlaybackDecision(token, "user-rotate", "1:0:1:104:204:304:0:0:0:0:", "hlsjs"))
}

func TestLivePlaybackDecisionTokenWithKeyRotationExpiredWindowRejected(t *testing.T) {
	const (
		activeSecret = "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
		legacySecret = "abcdefghijklmnopqrstuvwxyz0123456789ABCDE2"
	)
	rotationStarted := time.Now().UTC().Add(-10 * time.Minute)
	ring := resolveLiveDecisionKeyring(config.AppConfig{
		PlaybackDecisionSecret:         activeSecret,
		PlaybackDecisionKeyID:          "kid-active",
		PlaybackDecisionPreviousKeys:   []string{"kid-legacy:" + legacySecret},
		PlaybackDecisionRotationWindow: 2 * time.Minute,
	}, rotationStarted)

	s := &Server{
		liveDecisionKeyring: ring,
		liveDecisionTTL:     90 * time.Second,
	}
	now := time.Now().UTC()
	token := buildLivePlaybackDecisionToken(t, []byte(legacySecret), livePlaybackDecisionClaims{
		Principal:  "user-expired",
		ServiceRef: "1:0:1:105:205:305:0:0:0:0:",
		Mode:       "hlsjs",
		KeyID:      "kid-legacy",
		IssuedAt:   now.Unix() - 5,
		ExpiresAt:  now.Unix() + 60,
	})

	assert.False(t, s.verifyLivePlaybackDecision(token, "user-expired", "1:0:1:105:205:305:0:0:0:0:", "hlsjs"))
}

func TestLivePlaybackDecisionTokenUnknownKidRejected(t *testing.T) {
	const activeSecret = "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
	s := &Server{
		liveDecisionKeyring: resolveLiveDecisionKeyring(config.AppConfig{
			PlaybackDecisionSecret:         activeSecret,
			PlaybackDecisionKeyID:          "kid-active",
			PlaybackDecisionRotationWindow: 5 * time.Minute,
		}, time.Now().UTC()),
		liveDecisionTTL: 90 * time.Second,
	}
	now := time.Now().UTC()
	token := buildLivePlaybackDecisionToken(t, []byte(activeSecret), livePlaybackDecisionClaims{
		Principal:  "user-kid",
		ServiceRef: "1:0:1:106:206:306:0:0:0:0:",
		Mode:       "hlsjs",
		KeyID:      "kid-missing",
		IssuedAt:   now.Unix() - 5,
		ExpiresAt:  now.Unix() + 60,
	})

	assert.False(t, s.verifyLivePlaybackDecision(token, "user-kid", "1:0:1:106:206:306:0:0:0:0:", "hlsjs"))
}

func rewriteLivePlaybackDecisionToken(t *testing.T, s *Server, token string, mutate func(*livePlaybackDecisionClaims)) string {
	t.Helper()

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	require.Equal(t, liveDecisionTokenVersion, parts[0])

	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var claims livePlaybackDecisionClaims
	require.NoError(t, json.Unmarshal(payloadRaw, &claims))
	mutate(&claims)

	updatedPayloadRaw, err := json.Marshal(claims)
	require.NoError(t, err)

	updatedPayload := base64.RawURLEncoding.EncodeToString(updatedPayloadRaw)
	sig := s.liveDecisionSignature(updatedPayload)
	require.NotEmpty(t, sig)
	updatedSig := base64.RawURLEncoding.EncodeToString(sig)

	return liveDecisionTokenVersion + "." + updatedPayload + "." + updatedSig
}

func buildLivePlaybackDecisionToken(t *testing.T, signingKey []byte, claims livePlaybackDecisionClaims) string {
	t.Helper()
	payloadRaw, err := json.Marshal(claims)
	require.NoError(t, err)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	sig := liveDecisionSignatureWithKey(encodedPayload, signingKey)
	require.NotEmpty(t, sig)
	encodedSig := base64.RawURLEncoding.EncodeToString(sig)
	return liveDecisionTokenVersion + "." + encodedPayload + "." + encodedSig
}
