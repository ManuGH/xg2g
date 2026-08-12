// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
)

var (
	ErrNoCredentialsAvailable = errors.New("no passkey credentials available for user")
	ErrChallengeExpired       = errors.New("passkey challenge expired or not found")
	ErrInvalidSessionToken    = errors.New("invalid session token")
)

const (
	maxConcurrentChallenges = 1000
)

type Config struct {
	RPID                string
	RPName              string
	ExpectedOrigin      string
	SessionTTL          time.Duration
	PasskeyChallengeTTL time.Duration
}

type Service struct {
	cfg   Config
	store Store
	now   func() time.Time

	mu          sync.RWMutex
	challenges  map[string]webauthn.SessionData
	stopCleanup chan struct{}
}

func NewService(cfg Config, s Store) *Service {
	if cfg.RPID == "" {
		cfg.RPID = "localhost"
	}
	if cfg.RPName == "" {
		cfg.RPName = "xg2g"
	}
	if cfg.ExpectedOrigin == "" {
		cfg.ExpectedOrigin = "https://localhost"
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 24 * time.Hour
	}
	if cfg.PasskeyChallengeTTL <= 0 {
		cfg.PasskeyChallengeTTL = 5 * time.Minute
	}

	svc := &Service{
		cfg:         cfg,
		store:       s,
		now:         func() time.Time { return time.Now().UTC() },
		challenges:  make(map[string]webauthn.SessionData),
		stopCleanup: make(chan struct{}),
	}
	svc.startCleanupLoop()
	return svc
}

func (s *Service) Close() error {
	select {
	case <-s.stopCleanup:
		// already closed
	default:
		close(s.stopCleanup)
	}
	return nil
}

func (s *Service) startCleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.mu.Lock()
				s.purgeExpiredChallengesLocked()
				s.mu.Unlock()
			case <-s.stopCleanup:
				ticker.Stop()
				return
			}
		}
	}()
}

func (s *Service) purgeExpiredChallengesLocked() {
	now := s.now()
	for k, v := range s.challenges {
		if now.After(v.ExpiresAt) {
			delete(s.challenges, k)
		}
	}
}

func (s *Service) saveChallenge(challenge string, session webauthn.SessionData) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredChallengesLocked()

	// If at max capacity, evict an entry to maintain bounds
	if len(s.challenges) >= maxConcurrentChallenges {
		for k := range s.challenges {
			delete(s.challenges, k)
			if len(s.challenges) < maxConcurrentChallenges {
				break
			}
		}
	}

	s.challenges[challenge] = session
}

func (s *Service) popChallenge(challenge string) (webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.challenges[challenge]
	if ok {
		delete(s.challenges, challenge)
	}
	return session, ok
}

func (s *Service) SetNowFunc(fn func() time.Time) {
	s.now = fn
}

func (s *Service) Store() Store {
	return s.store
}

// HasUsers returns true if at least one user exists in the identity store.
func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	return len(users) > 0, nil
}

// CreateInitialAdminUser creates an initial admin user only if 0 users exist.
func (s *Service) CreateInitialAdminUser(ctx context.Context, username, displayName string) (*User, []string, error) {
	if username == "" {
		username = "admin"
	}
	if displayName == "" {
		displayName = "Administrator"
	}

	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(users) > 0 {
		return nil, nil, ErrAdminAlreadyExists
	}

	now := s.now()
	adminUser := &User{
		ID:          "usr_" + generateRandomHex(8),
		Username:    username,
		DisplayName: displayName,
		Role:        RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.PutUser(ctx, adminUser); err != nil {
		return nil, nil, fmt.Errorf("failed to create default admin user: %w", err)
	}

	rawCodes, records, err := GenerateRecoveryCodes(adminUser.ID, 10, now)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate recovery codes: %w", err)
	}
	if err := s.store.PutRecoveryCodes(ctx, records); err != nil {
		return nil, nil, fmt.Errorf("failed to store recovery codes: %w", err)
	}

	return adminUser, rawCodes, nil
}

// BeginPasskeyRegistration initiates WebAuthn credential registration.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, userID string) (*webauthn.CreationOptions, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	userDesc := webauthn.UserDescriptor{
		ID:          user.ID,
		Name:        user.Username,
		DisplayName: user.DisplayName,
	}
	opts, session, err := webauthn.BeginRegistration(userDesc, s.cfg.RPID, s.cfg.RPName, s.cfg.PasskeyChallengeTTL, now)
	if err != nil {
		return nil, err
	}

	s.saveChallenge(opts.Challenge, *session)
	return opts, nil
}

// FinishPasskeyRegistration validates attestation and stores the new Passkey.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, resp webauthn.AttestationResponse, nickname string) (*PasskeyCredential, error) {
	clientDataBytes, err := base64.RawURLEncoding.DecodeString(resp.ClientDataJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode clientDataJSON: %w", err)
	}

	var clientData webauthn.ClientDataJSON
	if err := parseJSON(clientDataBytes, &clientData); err != nil {
		return nil, err
	}

	session, ok := s.popChallenge(clientData.Challenge)
	if !ok {
		return nil, ErrChallengeExpired
	}

	now := s.now()
	attRes, err := webauthn.FinishRegistration(resp, session, s.cfg.RPID, s.cfg.ExpectedOrigin, now)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(nickname) == "" {
		nickname = "Passkey"
	}

	cred := &PasskeyCredential{
		ID:              attRes.CredentialID,
		UserID:          session.UserID,
		PublicKey:       attRes.PublicKeyDER,
		AttestationType: attRes.AttestationType,
		AAGUID:          attRes.AAGUID,
		SignCount:       attRes.SignCount,
		BackupEligible:  attRes.BackupEligible,
		BackupState:     attRes.BackupState,
		Transports:      attRes.Transports,
		Nickname:        nickname,
		CreatedAt:       now,
	}

	if err := s.store.PutPasskey(ctx, cred); err != nil {
		return nil, fmt.Errorf("failed to store passkey credential: %w", err)
	}

	return cred, nil
}

// BeginPasskeyLogin initiates WebAuthn login assertion.
func (s *Service) BeginPasskeyLogin(ctx context.Context, username string) (*webauthn.RequestOptions, error) {
	var allowedCreds []webauthn.CredentialDescriptor
	if username != "" {
		user, err := s.store.GetUserByUsername(ctx, username)
		if err != nil && !errors.Is(err, ErrStoreNotFound) {
			return nil, err
		}
		if user != nil {
			creds, err := s.store.ListPasskeysByUser(ctx, user.ID)
			if err != nil {
				return nil, err
			}
			allowedCreds = make([]webauthn.CredentialDescriptor, len(creds))
			for i, c := range creds {
				allowedCreds[i] = webauthn.CredentialDescriptor{
					Type:       "public-key",
					ID:         c.ID,
					Transports: c.Transports,
				}
			}
		}
	}

	now := s.now()
	opts, session, err := webauthn.BeginLogin(allowedCreds, s.cfg.RPID, s.cfg.PasskeyChallengeTTL, now)
	if err != nil {
		return nil, err
	}

	s.saveChallenge(opts.Challenge, *session)
	return opts, nil
}

// FinishPasskeyLogin validates the assertion signature, updates sign_count, and issues a WebSession.
func (s *Service) FinishPasskeyLogin(ctx context.Context, resp webauthn.AssertionResponse, userAgent, ip string) (*WebSession, *User, error) {
	clientDataBytes, err := base64.RawURLEncoding.DecodeString(resp.ClientDataJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode clientDataJSON: %w", err)
	}

	var clientData webauthn.ClientDataJSON
	if err := parseJSON(clientDataBytes, &clientData); err != nil {
		return nil, nil, err
	}

	session, ok := s.popChallenge(clientData.Challenge)
	if !ok {
		return nil, nil, ErrChallengeExpired
	}

	cred, err := s.store.GetPasskey(ctx, resp.CredentialID)
	if err != nil {
		return nil, nil, ErrCredentialNotFound
	}

	user, err := s.store.GetUser(ctx, cred.UserID)
	if err != nil {
		return nil, nil, ErrUserNotFound
	}

	now := s.now()
	credState := webauthn.CredentialState{
		ID:             cred.ID,
		PublicKeyDER:   cred.PublicKey,
		SignCount:      cred.SignCount,
		BackupEligible: cred.BackupEligible,
		BackupState:    cred.BackupState,
	}

	newSignCount, isBackup, err := webauthn.FinishLogin(resp, credState, session, s.cfg.RPID, s.cfg.ExpectedOrigin, now)
	if err != nil {
		return nil, nil, err
	}

	// Update credential metadata in DB
	_ = s.store.UpdatePasskey(ctx, cred.ID, newSignCount, isBackup, now)

	// Issue new WebSession
	webSess, err := s.issueSession(ctx, user.ID, userAgent, ip, now)
	if err != nil {
		return nil, nil, err
	}

	return webSess, user, nil
}

// LoginWithRecoveryCode authenticates via a single-use recovery code.
func (s *Service) LoginWithRecoveryCode(ctx context.Context, username, code, userAgent, ip string) (*WebSession, *User, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, nil, ErrUserNotFound
	}

	now := s.now()
	codeHash := HashRecoveryCode(code)

	if err := s.store.ConsumeRecoveryCode(ctx, user.ID, codeHash, now); err != nil {
		return nil, nil, err
	}

	webSess, err := s.issueSession(ctx, user.ID, userAgent, ip, now)
	if err != nil {
		return nil, nil, err
	}

	return webSess, user, nil
}

// ValidateWebSession retrieves and touches an active session (roaming tolerant).
func (s *Service) ValidateWebSession(ctx context.Context, sessionID, userAgent, ip string) (*WebSession, *User, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil, ErrInvalidSessionToken
	}

	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			return nil, nil, ErrInvalidSessionToken
		}
		return nil, nil, err
	}

	now := s.now()
	if !sess.IsActive(now) {
		if sess.RevokedAt != nil {
			return nil, nil, ErrSessionRevoked
		}
		return nil, nil, ErrSessionExpired
	}

	user, err := s.store.GetUser(ctx, sess.UserID)
	if err != nil {
		return nil, nil, err
	}

	// Touch session (updates last_active_at and logs IP/UA without locking)
	_ = s.store.TouchSession(ctx, sessionID, now, userAgent, ip)

	return sess, user, nil
}

// RevokeWebSession explicitly logs out a session.
func (s *Service) RevokeWebSession(ctx context.Context, sessionID string) error {
	return s.store.RevokeSession(ctx, sessionID, s.now())
}

// RevokeOtherWebSessions revokes all sessions belonging to userID except currentSessionID.
func (s *Service) RevokeOtherWebSessions(ctx context.Context, userID, currentSessionID string) error {
	return s.store.RevokeOtherSessions(ctx, userID, currentSessionID, s.now())
}

func (s *Service) issueSession(ctx context.Context, userID, userAgent, ip string, now time.Time) (*WebSession, error) {
	sessionID, err := generateSessionToken()
	if err != nil {
		return nil, err
	}

	sess := &WebSession{
		SessionID:    sessionID,
		UserID:       userID,
		UserAgent:    userAgent,
		LastIP:       ip,
		CreatedAt:    now,
		ExpiresAt:    now.Add(s.cfg.SessionTTL),
		LastActiveAt: now,
	}

	if err := s.store.PutSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("failed to persist web session: %w", err)
	}

	return sess, nil
}

func generateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func generateRandomHex(byteCount int) string {
	buf := make([]byte, byteCount)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func parseJSON(data []byte, out any) error {
	return json.Unmarshal(data, out)
}
