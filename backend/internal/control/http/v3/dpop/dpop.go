// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrMissingDPoPHeader       = errors.New("dpop: missing DPoP header")
	ErrInvalidProofFormat      = errors.New("dpop: invalid JWT proof format")
	ErrUnsupportedAlgorithm    = errors.New("dpop: unsupported algorithm; only ES256 (P-256) is permitted")
	ErrInvalidTypeHeader       = errors.New("dpop: typ header parameter must be 'dpop+jwt'")
	ErrMissingJWK              = errors.New("dpop: jwk header parameter missing or invalid")
	ErrInvalidJWKCurve         = errors.New("dpop: jwk must be EC curve P-256")
	ErrProofExpired            = errors.New("dpop: proof iat is outside allowed clock skew window")
	ErrHTTPMethodMismatch      = errors.New("dpop: htm claim does not match HTTP request method")
	ErrHTTPURIMismatch         = errors.New("dpop: htu claim does not match target HTTP URI")
	ErrAccessTokenHashMismatch = errors.New("dpop: ath claim does not match hash of provided access token")
	ErrReplayedJTI             = errors.New("dpop: proof jti has already been used (replay attack detected)")
	ErrInvalidSignature        = errors.New("dpop: signature verification failed")
)

// ProofHeader contains mandatory JOSE header claims for a DPoP proof.
type ProofHeader struct {
	Typ string         `json:"typ"`
	Alg string         `json:"alg"`
	JWK JWKECPublicKey `json:"jwk"`
}

// JWKECPublicKey represents an EC P-256 public key in JWK format.
type JWKECPublicKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// ProofPayload contains standard RFC 9449 DPoP claims.
type ProofPayload struct {
	JTI string `json:"jti"`
	HTM string `json:"htm"`
	HTU string `json:"htu"`
	IAT int64  `json:"iat"`
	ATH string `json:"ath,omitempty"`
}

// ProofClaims holds the verified claims and derived RFC 7638 thumbprint.
type ProofClaims struct {
	Header        ProofHeader
	Payload       ProofPayload
	JWKThumbprint string // RFC 7638 SHA-256 Base64URL thumbprint (jkt)
	PublicKey     *ecdsa.PublicKey
}

// ComputeJWKThumbprint computes the canonical RFC 7638 SHA-256 Base64URL thumbprint (jkt) for EC P-256.
// Invariant: Member keys must be ordered lexicographically: "crv", "kty", "x", "y".
func ComputeJWKThumbprint(jwk JWKECPublicKey) (string, error) {
	if jwk.Kty != "EC" || jwk.Crv != "P-256" || jwk.X == "" || jwk.Y == "" {
		return "", ErrInvalidJWKCurve
	}

	// Canonical JSON string with lexicographical key order (crv, kty, x, y)
	canonicalMap := map[string]string{
		"crv": jwk.Crv,
		"kty": jwk.Kty,
		"x":   jwk.X,
		"y":   jwk.Y,
	}

	// Build raw JSON explicitly preserving exact RFC 7638 key ordering
	rawJSON := fmt.Sprintf(`{"crv":%q,"kty":%q,"x":%q,"y":%q}`,
		canonicalMap["crv"], canonicalMap["kty"], canonicalMap["x"], canonicalMap["y"])

	hash := sha256.Sum256([]byte(rawJSON))
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// NormalizeHTU performs RFC 9449 §4.3 syntax and scheme-based normalization on an HTTP target URI.
// It removes query string and fragment, lowercases scheme/host, omits default ports (:443 for https, :80 for http),
// but preserves path semantics unchanged.
func NormalizeHTU(targetURL string) string {
	u, err := url.Parse(targetURL)
	if err != nil {
		return targetURL
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)

	// Remove default port suffixes
	if scheme == "https" && strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	} else if scheme == "http" && strings.HasSuffix(host, ":80") {
		host = strings.TrimSuffix(host, ":80")
	}

	path := u.Path
	if path == "" {
		path = "/"
	}

	return scheme + "://" + host + path
}

// ComputeAccessTokenHash (ath) computes the base64url(sha256(access_token)) hash claim per RFC 9449 §4.2.
func ComputeAccessTokenHash(accessToken string) string {
	hash := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// ReplayCache provides bounded in-memory LRU jti deduplication.
type ReplayCache struct {
	mu      sync.Mutex
	cache   map[string]time.Time
	maxSize int
}

func NewReplayCache(maxSize int) *ReplayCache {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &ReplayCache{
		cache:   make(map[string]time.Time),
		maxSize: maxSize,
	}
}

func (c *ReplayCache) AddIfNew(jti string, expiresAt time.Time, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. REPLAY CHECK FIRST: Replay detection MUST occur before any eviction!
	if exp, exists := c.cache[jti]; exists && now.Before(exp) {
		return false // Replay detected!
	}

	// 2. Clean expired entries if capacity threshold reached
	if len(c.cache) >= c.maxSize {
		for k, exp := range c.cache {
			if now.After(exp) {
				delete(c.cache, k)
			}
		}
	}

	// 3. HARD CAPACITY CAP ENFORCEMENT: Evict entry with oldest expiry if still at capacity
	if len(c.cache) >= c.maxSize {
		var oldestKey string
		var oldestExp time.Time
		first := true
		for k, exp := range c.cache {
			if first || exp.Before(oldestExp) {
				oldestKey = k
				oldestExp = exp
				first = false
			}
		}
		if oldestKey != "" {
			delete(c.cache, oldestKey)
		}
	}

	c.cache[jti] = expiresAt
	return true
}

func (c *ReplayCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cache)
}

// ValidatorConfig configures DPoP proof validation rules and replay cache parameters.
type ValidatorConfig struct {
	MaxClockSkew time.Duration // Max clock skew for iat validation (default: 5 min)
	ReplayTTL    time.Duration // Replay cache retention duration (default: 10 min)
	MaxCacheSize int           // Maximum LRU entries in replay cache (default: 10,000)
}

// Validator encapsulates DPoP proof validation.
type Validator struct {
	maxClockSkew time.Duration
	replayTTL    time.Duration
	replayCache  *ReplayCache
}

func NewValidator(cfg ValidatorConfig) *Validator {
	if cfg.MaxClockSkew <= 0 {
		cfg.MaxClockSkew = 5 * time.Minute
	}
	if cfg.ReplayTTL <= 0 {
		cfg.ReplayTTL = 10 * time.Minute
	}
	if cfg.MaxCacheSize <= 0 {
		cfg.MaxCacheSize = 10000
	}
	return &Validator{
		maxClockSkew: cfg.MaxClockSkew,
		replayTTL:    cfg.ReplayTTL,
		replayCache:  NewReplayCache(cfg.MaxCacheSize),
	}
}

func NewDefaultValidator() *Validator {
	return NewValidator(ValidatorConfig{})
}

// ValidateProof verifies a DPoP proof JWT according to RFC 9449.
func (v *Validator) ValidateProof(r *http.Request, proofJWT string, accessToken string, now time.Time) (*ProofClaims, error) {
	proofJWT = strings.TrimSpace(proofJWT)
	if proofJWT == "" {
		return nil, ErrMissingDPoPHeader
	}

	parts := strings.Split(proofJWT, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidProofFormat
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid header base64", ErrInvalidProofFormat)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload base64", ErrInvalidProofFormat)
	}

	signatureBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature base64", ErrInvalidProofFormat)
	}

	var header ProofHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: failed to parse header json", ErrInvalidProofFormat)
	}

	var payload ProofPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("%w: failed to parse payload json", ErrInvalidProofFormat)
	}

	// 1. Validate Header Claims
	if strings.ToLower(header.Typ) != "dpop+jwt" {
		return nil, ErrInvalidTypeHeader
	}
	if header.Alg != "ES256" {
		return nil, ErrUnsupportedAlgorithm
	}

	// 2. Validate & Parse Public Key
	pubKey, err := parseECP256JWK(header.JWK)
	if err != nil {
		return nil, err
	}

	thumbprint, err := ComputeJWKThumbprint(header.JWK)
	if err != nil {
		return nil, err
	}

	// 3. Verify Signature over "header.payload"
	signedData := []byte(parts[0] + "." + parts[1])
	hasher := sha256.Sum256(signedData)

	if !verifyES256Signature(pubKey, hasher[:], signatureBytes) {
		return nil, ErrInvalidSignature
	}

	// 4. Validate Claims
	if strings.ToUpper(payload.HTM) != strings.ToUpper(r.Method) {
		return nil, ErrHTTPMethodMismatch
	}

	reqURI := getFullRequestURL(r)
	if NormalizeHTU(payload.HTU) != NormalizeHTU(reqURI) {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrHTTPURIMismatch, NormalizeHTU(reqURI), NormalizeHTU(payload.HTU))
	}

	// 5. Validate Clock Skew (iat) against MaxClockSkew (default: 5 min)
	proofTime := time.Unix(payload.IAT, 0)
	if now.Sub(proofTime) > v.maxClockSkew || proofTime.Sub(now) > v.maxClockSkew {
		return nil, ErrProofExpired
	}

	// 6. Replay Check (jti) against ReplayTTL (default: 10 min)
	if strings.TrimSpace(payload.JTI) == "" {
		return nil, fmt.Errorf("%w: jti claim is empty", ErrInvalidProofFormat)
	}
	if !v.replayCache.AddIfNew(payload.JTI, now.Add(v.replayTTL), now) {
		return nil, ErrReplayedJTI
	}

	// 7. Validate Access Token Hash (ath) if Access Token provided
	if accessToken != "" {
		expectedATH := ComputeAccessTokenHash(accessToken)
		if payload.ATH != expectedATH {
			return nil, ErrAccessTokenHashMismatch
		}
	}

	return &ProofClaims{
		Header:        header,
		Payload:       payload,
		JWKThumbprint: thumbprint,
		PublicKey:     pubKey,
	}, nil
}

func parseECP256JWK(jwk JWKECPublicKey) (*ecdsa.PublicKey, error) {
	if jwk.Kty != "EC" || jwk.Crv != "P-256" {
		return nil, ErrInvalidJWKCurve
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil || len(xBytes) == 0 {
		return nil, fmt.Errorf("%w: invalid x coordinate", ErrMissingJWK)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil || len(yBytes) == 0 {
		return nil, fmt.Errorf("%w: invalid y coordinate", ErrMissingJWK)
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)

	// SECURITY GUARD: Verify point (x,y) lies on the P-256 curve
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, fmt.Errorf("%w: public key point (x,y) does not lie on P-256 curve", ErrInvalidJWKCurve)
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}, nil
}

func verifyES256Signature(pub *ecdsa.PublicKey, hash []byte, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	return ecdsa.Verify(pub, hash, r, s)
}

func getFullRequestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	return scheme + "://" + host + r.URL.Path
}
