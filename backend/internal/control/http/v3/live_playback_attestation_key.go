package v3

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
)

const (
	defaultLiveDecisionRotationWindow = 10 * time.Minute
	liveDecisionDerivedKeyIDPrefixLen = 16
)

type liveDecisionKey struct {
	key         []byte
	verifyUntil time.Time
}

func (k liveDecisionKey) active(now time.Time) bool {
	return k.verifyUntil.IsZero() || !now.After(k.verifyUntil)
}

type liveDecisionKeyring struct {
	signerKid string
	keys      map[string]liveDecisionKey
	order     []string
}

func (r *liveDecisionKeyring) addKey(kid string, key []byte, verifyUntil time.Time) {
	if strings.TrimSpace(kid) == "" || len(key) == 0 {
		return
	}
	if r.keys == nil {
		r.keys = make(map[string]liveDecisionKey, 2)
	}
	if _, exists := r.keys[kid]; !exists {
		r.order = append(r.order, kid)
	}
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	r.keys[kid] = liveDecisionKey{
		key:         keyCopy,
		verifyUntil: verifyUntil,
	}
}

func (r liveDecisionKeyring) signingKey() (kid string, key []byte, ok bool) {
	signerKid := strings.TrimSpace(r.signerKid)
	if signerKid == "" {
		return "", nil, false
	}
	material, found := r.keys[signerKid]
	if !found || len(material.key) == 0 {
		return "", nil, false
	}
	return signerKid, material.key, true
}

func (r liveDecisionKeyring) lookupVerificationKey(kid string, now time.Time) ([]byte, bool) {
	material, found := r.keys[strings.TrimSpace(kid)]
	if !found || len(material.key) == 0 || !material.active(now) {
		return nil, false
	}
	return material.key, true
}

func (r liveDecisionKeyring) legacyVerificationKeys(now time.Time) [][]byte {
	if len(r.order) == 0 {
		return nil
	}
	keys := make([][]byte, 0, len(r.order))
	for _, kid := range r.order {
		material, found := r.keys[kid]
		if !found || len(material.key) == 0 || !material.active(now) {
			continue
		}
		keys = append(keys, material.key)
	}
	return keys
}

func resolveLiveDecisionKeyring(cfg config.AppConfig, now time.Time) liveDecisionKeyring {
	ring := liveDecisionKeyring{}
	signingKey := resolveLiveDecisionSigningKey(cfg)
	if len(signingKey) == 0 {
		return ring
	}

	signerKid := normalizeLiveDecisionKeyID(cfg.PlaybackDecisionKeyID)
	if signerKid == "" {
		signerKid = deriveLiveDecisionKeyID(signingKey)
	}
	ring.signerKid = signerKid
	ring.addKey(signerKid, signingKey, time.Time{})

	rotationWindow := cfg.PlaybackDecisionRotationWindow
	if rotationWindow == 0 {
		rotationWindow = defaultLiveDecisionRotationWindow
	}
	if rotationWindow < 0 {
		rotationWindow = 0
	}
	if rotationWindow <= 0 {
		if len(cfg.PlaybackDecisionPreviousKeys) > 0 {
			log.L().Warn().Msg("api.playbackDecisionPreviousKeys configured but rotation window <= 0; previous keys ignored")
		}
		return ring
	}

	verifyUntil := now.Add(rotationWindow)
	for _, rawEntry := range cfg.PlaybackDecisionPreviousKeys {
		kid, previousKey := parseLiveDecisionKeyEntry(rawEntry)
		if len(previousKey) == 0 {
			log.L().Warn().Str("entry", strings.TrimSpace(rawEntry)).Msg("ignoring invalid live playback previous key entry")
			continue
		}
		if kid == "" {
			kid = deriveLiveDecisionKeyID(previousKey)
		}
		if kid == signerKid {
			if !bytes.Equal(previousKey, signingKey) {
				log.L().Warn().Str("kid", kid).Msg("ignoring playback previous key with duplicate kid")
			}
			continue
		}
		ring.addKey(kid, previousKey, verifyUntil)
	}
	return ring
}

func resolveLiveDecisionSigningKey(cfg config.AppConfig) []byte {
	if secret := strings.TrimSpace(cfg.PlaybackDecisionSecret); secret != "" {
		return []byte(secret)
	}

	// Backward-compatible fallback for deployments that already manage API tokens centrally.
	if secret := strings.TrimSpace(cfg.APIToken); secret != "" {
		return []byte(secret)
	}

	// Last-resort fallback keeps single-instance/dev usable but is not restart-stable.
	key := make([]byte, liveDecisionFallbackKeyLengthByte)
	if _, err := rand.Read(key); err != nil {
		log.L().Error().Err(err).Msg("live playback attestation signing key unavailable")
		return nil
	}
	log.L().Warn().Msg("api.playbackDecisionSecret is not configured; using ephemeral live playback attestation key")
	return key
}

func parseLiveDecisionKeyEntry(raw string) (kid string, secret []byte) {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return "", nil
	}
	idx := strings.Index(entry, ":")
	if idx < 0 {
		return "", []byte(entry)
	}
	kid = normalizeLiveDecisionKeyID(entry[:idx])
	secretValue := strings.TrimSpace(entry[idx+1:])
	if secretValue == "" {
		return "", nil
	}
	return kid, []byte(secretValue)
}

func deriveLiveDecisionKeyID(secret []byte) string {
	if len(secret) == 0 {
		return ""
	}
	sum := sha256.Sum256(secret)
	hexValue := hex.EncodeToString(sum[:])
	if len(hexValue) < liveDecisionDerivedKeyIDPrefixLen {
		return "k" + hexValue
	}
	return "k" + hexValue[:liveDecisionDerivedKeyIDPrefixLen]
}

func normalizeLiveDecisionKeyID(raw string) string {
	kid := strings.ToLower(strings.TrimSpace(raw))
	if kid == "" {
		return ""
	}
	for _, ch := range kid {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '_' || ch == '-' || ch == '.' {
			continue
		}
		return ""
	}
	return kid
}
