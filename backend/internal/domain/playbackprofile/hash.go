package playbackprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CanonicalJSON returns the canonical JSON representation of the target profile.
func (p TargetPlaybackProfile) CanonicalJSON() ([]byte, error) {
	return json.Marshal(CanonicalizeTarget(p))
}

// Hash returns the stable SHA-256 hash of the canonical target profile.
func (p TargetPlaybackProfile) Hash() string {
	b, _ := p.CanonicalJSON()
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashTarget returns the stable SHA-256 hash of the canonical target profile.
func HashTarget(p TargetPlaybackProfile) string {
	return p.Hash()
}
