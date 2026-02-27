package v3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	defaultLivePlaybackDecisionTTL    = 2 * time.Minute
	liveDecisionTokenVersion          = "v1"
	liveDecisionAllowedClockSkew      = 15 * time.Second
	liveDecisionFallbackKeyLengthByte = 32
)

type livePlaybackDecisionClaims struct {
	Principal  string `json:"sub,omitempty"`
	ServiceRef string `json:"serviceRef"`
	Mode       string `json:"mode"`
	KeyID      string `json:"kid,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
}

func (s *Server) attestLivePlaybackDecision(requestID, principal, serviceRef, mode string) string {
	requestID = strings.TrimSpace(requestID)
	principal = strings.TrimSpace(principal)
	serviceRef = strings.TrimSpace(serviceRef)
	mode = normalize.Token(mode)
	signerKid, signingKey, ok := s.resolveLiveDecisionSigner()
	if serviceRef == "" || mode == "" || !ok {
		return ""
	}

	ttl := s.liveDecisionTTL
	if ttl <= 0 {
		ttl = defaultLivePlaybackDecisionTTL
	}

	now := time.Now().UTC()
	claims := livePlaybackDecisionClaims{
		Principal:  principal,
		ServiceRef: serviceRef,
		Mode:       mode,
		KeyID:      signerKid,
		RequestID:  requestID,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(ttl).Unix(),
	}

	payloadRaw, err := json.Marshal(claims)
	if err != nil {
		return ""
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	sig := liveDecisionSignatureWithKey(encodedPayload, signingKey)
	if len(sig) == 0 {
		return ""
	}

	encodedSig := base64.RawURLEncoding.EncodeToString(sig)
	return liveDecisionTokenVersion + "." + encodedPayload + "." + encodedSig
}

func (s *Server) verifyLivePlaybackDecision(token, principal, serviceRef, mode string) bool {
	token = strings.TrimSpace(token)
	principal = strings.TrimSpace(principal)
	serviceRef = strings.TrimSpace(serviceRef)
	mode = normalize.Token(mode)
	if token == "" || serviceRef == "" || mode == "" {
		return false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	if parts[0] != liveDecisionTokenVersion {
		return false
	}

	encodedPayload := parts[1]
	encodedSig := parts[2]

	payloadRaw, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return false
	}

	var claims livePlaybackDecisionClaims
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return false
	}

	sig, err := base64.RawURLEncoding.DecodeString(encodedSig)
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	if !s.verifyLiveDecisionSignature(sig, encodedPayload, claims.KeyID, now) {
		return false
	}

	nowUnix := now.Unix()
	if claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt {
		return false
	}
	if claims.ExpiresAt < nowUnix {
		return false
	}
	if claims.IssuedAt > nowUnix+int64(liveDecisionAllowedClockSkew/time.Second) {
		return false
	}
	if strings.TrimSpace(claims.Principal) != principal {
		return false
	}
	if strings.TrimSpace(claims.ServiceRef) != serviceRef {
		return false
	}
	return normalize.Token(claims.Mode) == mode
}

func (s *Server) liveDecisionSignature(encodedPayload string) []byte {
	_, signingKey, ok := s.resolveLiveDecisionSigner()
	if !ok {
		return nil
	}
	return liveDecisionSignatureWithKey(encodedPayload, signingKey)
}

func liveDecisionSignatureWithKey(encodedPayload string, signingKey []byte) []byte {
	if encodedPayload == "" || len(signingKey) == 0 {
		return nil
	}

	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}

func (s *Server) resolveLiveDecisionSigner() (kid string, key []byte, ok bool) {
	if kid, key, ok = s.liveDecisionKeyring.signingKey(); ok {
		return kid, key, true
	}
	if len(s.liveDecisionSigningKey) == 0 {
		return "", nil, false
	}
	return "", s.liveDecisionSigningKey, true
}

func (s *Server) resolveLiveDecisionVerificationKey(kid string, now time.Time) ([]byte, bool) {
	if key, ok := s.liveDecisionKeyring.lookupVerificationKey(kid, now); ok {
		return key, true
	}
	return nil, false
}

func (s *Server) resolveLiveDecisionLegacyVerificationKeys(now time.Time) [][]byte {
	keys := s.liveDecisionKeyring.legacyVerificationKeys(now)
	if len(keys) > 0 {
		return keys
	}
	if len(s.liveDecisionSigningKey) == 0 {
		return nil
	}
	return [][]byte{s.liveDecisionSigningKey}
}

func (s *Server) verifyLiveDecisionSignature(sig []byte, encodedPayload, claimKeyID string, now time.Time) bool {
	kid := normalizeLiveDecisionKeyID(claimKeyID)
	if kid != "" {
		key, ok := s.resolveLiveDecisionVerificationKey(kid, now)
		if !ok {
			return false
		}
		expectedSig := liveDecisionSignatureWithKey(encodedPayload, key)
		return len(expectedSig) > 0 && hmac.Equal(sig, expectedSig)
	}

	keys := s.resolveLiveDecisionLegacyVerificationKeys(now)
	for _, key := range keys {
		expectedSig := liveDecisionSignatureWithKey(encodedPayload, key)
		if len(expectedSig) > 0 && hmac.Equal(sig, expectedSig) {
			return true
		}
	}
	return false
}
