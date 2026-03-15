// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

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
