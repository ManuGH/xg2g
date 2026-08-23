// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidSessionToken = errors.New("invalid session token")
	ErrInvalidSessionTTL   = errors.New("invalid session ttl")
)

// SessionTokenStore maps opaque session IDs to bearer tokens or principals.
type SessionTokenStore interface {
	CreateSession(token string, ttl time.Duration) (string, error)
	CreateSessionWithPrincipal(token string, principal *Principal, ttl time.Duration) (string, error)
	ResolveSessionToken(sessionID string) (string, bool)
	ResolveSessionPrincipal(sessionID string) (*Principal, bool)
	InvalidateSession(sessionID string)
}

type sessionEntry struct {
	token     string
	principal *Principal
	expiresAt time.Time
}

// InMemorySessionTokenStore keeps auth sessions in process memory.
type InMemorySessionTokenStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry
}

func NewInMemorySessionTokenStore() *InMemorySessionTokenStore {
	return &InMemorySessionTokenStore{
		sessions: make(map[string]sessionEntry),
	}
}

func (s *InMemorySessionTokenStore) CreateSession(token string, ttl time.Duration) (string, error) {
	return s.CreateSessionWithPrincipal(token, nil, ttl)
}

func (s *InMemorySessionTokenStore) CreateSessionWithPrincipal(token string, principal *Principal, ttl time.Duration) (string, error) {
	if token == "" && principal == nil {
		return "", ErrInvalidSessionToken
	}
	if ttl <= 0 {
		return "", ErrInvalidSessionTTL
	}

	sessionID, err := newSessionID()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.sessions[sessionID] = sessionEntry{
		token:     token,
		principal: principal,
		expiresAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()

	return sessionID, nil
}

func (s *InMemorySessionTokenStore) ResolveSessionPrincipal(tokenOrSessionID string) (*Principal, bool) {
	if tokenOrSessionID == "" {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Direct match by session ID
	if entry, ok := s.sessions[tokenOrSessionID]; ok {
		if time.Now().After(entry.expiresAt) {
			return nil, false
		}
		if entry.principal != nil {
			return entry.principal, true
		}
	}

	// 2. Match by mapped token
	for _, entry := range s.sessions {
		if entry.token == tokenOrSessionID {
			if time.Now().After(entry.expiresAt) {
				return nil, false
			}
			if entry.principal != nil {
				return entry.principal, true
			}
		}
	}

	return nil, false
}

func (s *InMemorySessionTokenStore) ResolveSessionToken(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}

	s.mu.RLock()
	entry, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}

	if time.Now().After(entry.expiresAt) {
		s.InvalidateSession(sessionID)
		return "", false
	}

	return entry.token, true
}

func (s *InMemorySessionTokenStore) InvalidateSession(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

func newSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
