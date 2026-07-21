// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package tokens

import (
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
)

// Service manages live playback attestation tokens and key rotation.
type Service struct {
	mu         sync.RWMutex
	jwtSecret  []byte
	keyring    liveDecisionKeyring
	signingKey []byte
	ttl        time.Duration
}

// NewService constructs a new attestation tokens service.
func NewService(cfg config.AppConfig) *Service {
	now := time.Now().UTC()
	keyring := resolveLiveDecisionKeyring(cfg, now)
	_, signingKey, _ := keyring.signingKey()
	return &Service{
		keyring:    keyring,
		signingKey: signingKey,
		ttl:        defaultLivePlaybackDecisionTTL,
	}
}

// SetJWTSecret configures or clears the override HMAC-SHA256 signing secret.
func (s *Service) SetJWTSecret(secret []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(secret) == 0 {
		s.jwtSecret = nil
		s.signingKey = nil
		return
	}
	s.jwtSecret = append([]byte(nil), secret...)
	s.signingKey = append([]byte(nil), secret...)
}

// UpdateConfig updates the keyring when configuration changes.
func (s *Service) UpdateConfig(cfg config.AppConfig) {
	now := time.Now().UTC()
	keyring := resolveLiveDecisionKeyring(cfg, now)
	_, signingKey, _ := keyring.signingKey()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyring = keyring
	if len(s.jwtSecret) == 0 {
		s.signingKey = signingKey
	}
}
