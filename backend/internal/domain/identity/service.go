// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
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

func (s *Service) Config() Config {
	return s.cfg
}

// HasUsers returns true if at least one user exists in the identity store.
func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	return len(users) > 0, nil
}

// GenerateBootstrapToken generates a persistent, single-use 15-minute setup token in SQLite.
func (s *Service) GenerateBootstrapToken(ctx context.Context) (string, error) {
	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return "", err
	}
	if hasUsers {
		return "", ErrAdminAlreadyExists
	}

	tokenBytes := make([]byte, 20)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token bytes: %w", err)
	}
	rawToken := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(tokenBytes)

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	now := s.now()
	expiresAt := now.Add(15 * time.Minute)
	if err := s.store.PutBootstrapToken(ctx, tokenHash, now, expiresAt); err != nil {
		return "", fmt.Errorf("failed to persist bootstrap token: %w", err)
	}
	return rawToken, nil
}

// ValidateAndConsumeBootstrapToken verifies and consumes a single-use bootstrap token from SQLite.
func (s *Service) ValidateAndConsumeBootstrapToken(ctx context.Context, rawToken string) (bool, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return false, nil
	}
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	return s.store.ConsumeBootstrapToken(ctx, tokenHash, s.now())
}

// BeginBootstrapRegistration initiates the initial admin WebAuthn registration ceremony.
// NOTE: It does NOT write any user or recovery codes to SQLite yet. State is kept transient.
func (s *Service) BeginBootstrapRegistration(ctx context.Context, username, displayName string) (*webauthn.CreationOptions, error) {
	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return nil, err
	}
	if hasUsers {
		return nil, ErrAdminAlreadyExists
	}

	if username == "" {
		username = "admin"
	}
	if displayName == "" {
		displayName = "Administrator"
	}

	now := s.now()
	tempUserID := "usr_" + generateRandomHex(8)
	userDesc := webauthn.UserDescriptor{
		ID:          tempUserID,
		Name:        username,
		DisplayName: displayName,
	}

	opts, session, err := webauthn.BeginRegistration(userDesc, s.cfg.RPID, s.cfg.RPName, s.cfg.PasskeyChallengeTTL, now)
	if err != nil {
		return nil, err
	}

	session.Username = username
	session.DisplayName = displayName
	session.IsBootstrap = true

	s.saveChallenge(opts.Challenge, *session)
	return opts, nil
}

// BeginPasskeyRegistration initiates WebAuthn credential registration for an existing authenticated user.
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
// If this was a bootstrap registration, it atomically creates the Admin user, Passkey, Recovery Codes, and initial Session.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, resp webauthn.AttestationResponse, nickname, userAgent, ip string) (*PasskeyCredential, *BootstrapResult, error) {
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

	now := s.now()
	attRes, err := webauthn.FinishRegistration(resp, session, s.cfg.RPID, s.cfg.ExpectedOrigin, now)
	if err != nil {
		return nil, nil, err
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

	if session.IsBootstrap {
		// Atomic First-Admin Commit:
		// 1. Create Admin User
		adminUser := &User{
			ID:          session.UserID,
			Username:    session.Username,
			DisplayName: session.DisplayName,
			Role:        RoleAdmin,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		// 2. Generate 10 Recovery Codes
		rawCodes, recRecords, err := GenerateRecoveryCodes(adminUser.ID, 10, now)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate recovery codes: %w", err)
		}

		// 3. Create initial WebSession
		sessionID, err := generateSessionToken()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate initial session id: %w", err)
		}
		webSess := &WebSession{
			SessionID:    sessionID,
			UserID:       adminUser.ID,
			UserAgent:    userAgent,
			LastIP:       ip,
			CreatedAt:    now,
			ExpiresAt:    now.Add(s.cfg.SessionTTL),
			LastActiveAt: now,
		}

		// 4. Atomic SQLite Commit
		if err := s.store.CommitInitialAdminBootstrap(ctx, adminUser, cred, recRecords, webSess); err != nil {
			return nil, nil, fmt.Errorf("atomic admin bootstrap commit failed: %w", err)
		}

		res := &BootstrapResult{
			User:          *adminUser,
			Credential:    *cred,
			RecoveryCodes: rawCodes,
			SessionID:     webSess.SessionID,
			ExpiresAt:     webSess.ExpiresAt,
		}
		return cred, res, nil
	}

	if err := s.store.PutPasskey(ctx, cred); err != nil {
		return nil, nil, fmt.Errorf("failed to store passkey credential: %w", err)
	}

	return cred, nil, nil
}

// AcknowledgeRecoveryCodes marks recovery codes as acknowledged and closes the bootstrap state.
func (s *Service) AcknowledgeRecoveryCodes(ctx context.Context, userID string) error {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.Role != RoleAdmin {
		return ErrBootstrapUnauthorized
	}
	return s.store.AcknowledgeRecoveryCodes(ctx, userID, s.now())
}

// GetBootstrapStatus returns the current state of system initialization.
func (s *Service) GetBootstrapStatus(ctx context.Context) (BootstrapState, error) {
	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return "", err
	}
	if !hasUsers {
		return BootstrapStateSetupRequired, nil
	}

	meta, err := s.store.GetBootstrapMeta(ctx)
	if err != nil {
		return "", err
	}
	if meta != nil && meta.BootstrapClosed && meta.RecoveryCodesAcknowledgedAt != nil {
		return BootstrapStateReady, nil
	}
	return BootstrapStateSetupInProgress, nil
}

// IsPublicReady checks all invariants required before xg2g can accept public traffic:
// 1. At least 1 admin user exists
// 2. Initial admin has at least 1 passkey registered
// 3. Recovery codes exist
// 4. Recovery codes have been acknowledged by the admin
// IsIdentityReady checks if the core identity invariants are satisfied:
// 1. At least 1 admin user exists
// 2. Admin has registered passkey
// 3. Recovery codes exist
// 4. Recovery codes acknowledged
// 5. Bootstrap is closed
func (s *Service) IsIdentityReady(ctx context.Context) (bool, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	var adminUser *User
	for _, u := range users {
		if u.Role == RoleAdmin {
			adminUser = &u
			break
		}
	}
	if adminUser == nil {
		return false, nil
	}

	passkeys, err := s.store.ListPasskeysByUser(ctx, adminUser.ID)
	if err != nil {
		return false, err
	}
	if len(passkeys) == 0 {
		return false, nil
	}

	recCodes, err := s.store.ListRecoveryCodesByUser(ctx, adminUser.ID)
	if err != nil {
		return false, err
	}
	if len(recCodes) == 0 {
		return false, nil
	}

	meta, err := s.store.GetBootstrapMeta(ctx)
	if err != nil {
		return false, err
	}
	if meta == nil || !meta.BootstrapClosed || meta.RecoveryCodesAcknowledgedAt == nil {
		return false, nil
	}

	return true, nil
}

// IsPublicReady checks if Identity is Ready AND public HTTPS/Origin configuration is valid.
func (s *Service) IsPublicReady(ctx context.Context) (bool, error) {
	identityReady, err := s.IsIdentityReady(ctx)
	if err != nil || !identityReady {
		return false, err
	}

	// Validate Public HTTPS & Origin Contract:
	rpID := strings.TrimSpace(s.cfg.RPID)
	if rpID == "" || rpID == "localhost" || rpID == "127.0.0.1" || rpID == "0.0.0.0" || rpID == "::" {
		return false, nil
	}

	expectedOrigin := strings.TrimSpace(s.cfg.ExpectedOrigin)
	if !strings.HasPrefix(expectedOrigin, "https://") {
		return false, nil
	}

	return true, nil
}

// GenerateEmergencyRecoveryCodes generates fresh recovery codes from local CLI, atomically replacing old codes.
func (s *Service) GenerateEmergencyRecoveryCodes(ctx context.Context, username string) ([]string, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rawCodes, records, err := GenerateRecoveryCodes(user.ID, 10, now)
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplaceRecoveryCodesForUser(ctx, user.ID, records); err != nil {
		return nil, err
	}
	return rawCodes, nil
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

// VerifyPasskeyAssertion validates a WebAuthn assertion signature WITHOUT issuing an xg2g_session browser cookie.
// Used for Android native app & Android TV device enrollment.
func (s *Service) VerifyPasskeyAssertion(ctx context.Context, resp webauthn.AssertionResponse) (*User, *PasskeyCredential, error) {
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

	_ = s.store.UpdatePasskey(ctx, cred.ID, newSignCount, isBackup, now)

	// INVARIANT: Return user & credential without issuing any browser session cookie
	return user, cred, nil
}

type DeviceGrantResult struct {
	TokenType    string `json:"token_type"`    // Always "DPoP"
	AccessToken  string `json:"access_token"`  // Opaque 32-byte token
	RefreshToken string `json:"refresh_token"` // Opaque 32-byte refresh token
	ExpiresIn    int    `json:"expires_in"`    // Seconds (e.g. 900 for 15 min)
	DeviceID     string `json:"device_id"`
	Scope        string `json:"scope"`
}

// IssueDeviceGrant registers/updates the Android device and issues a DPoP-bound Grant & Access Token.
func (s *Service) IssueDeviceGrant(ctx context.Context, userID, deviceName, platform string, jwk JWKECPublicKey, scopes string) (*DeviceGrantResult, error) {
	jkt, err := ComputeJWKThumbprint(jwk)
	if err != nil {
		return nil, fmt.Errorf("failed to compute RFC 7638 JWK thumbprint: %w", err)
	}

	jwkBytes, _ := json.Marshal(jwk)
	now := s.now()

	deviceID := "dev_" + generateRandomHex(12)
	dev := &Device{
		ID:            deviceID,
		UserID:        userID,
		DeviceName:    deviceName,
		Platform:      platform,
		PublicKeyJWK:  string(jwkBytes),
		JWKThumbprint: jkt,
		CreatedAt:     now,
		LastSeenAt:    now,
	}

	// Check if device with this thumbprint already exists for user
	existingDev, err := s.store.GetDeviceByThumbprint(ctx, jkt)
	if err == nil && existingDev != nil {
		dev.ID = existingDev.ID
		dev.CreatedAt = existingDev.CreatedAt
	}

	if err := s.store.PutDevice(ctx, dev); err != nil {
		return nil, fmt.Errorf("failed to register device: %w", err)
	}

	familyID := "fam_" + generateRandomHex(16)
	grantID := "grant_" + generateRandomHex(16)
	grant := &DeviceGrant{
		ID:        grantID,
		DeviceID:  dev.ID,
		UserID:    userID,
		FamilyID:  familyID,
		GrantType: "passkey_device_grant",
		CreatedAt: now,
	}

	rawRefreshToken := "rt_" + generateRandomHex(32)
	refreshHash := hashToken(rawRefreshToken)

	initialToken := &RefreshTokenFamily{
		TokenHash:  refreshHash,
		FamilyID:   familyID,
		DeviceID:   dev.ID,
		Generation: 0,
		CreatedAt:  now,
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
	}

	if err := s.store.PutDeviceGrant(ctx, grant, initialToken); err != nil {
		return nil, fmt.Errorf("failed to commit device grant: %w", err)
	}

	// Issue DPoP-bound Access Token (15 Min TTL)
	rawAccessToken := "at_dpop_" + generateRandomHex(32)
	accessHash := hashToken(rawAccessToken)

	if scopes == "" {
		scopes = "api playback"
	}

	accessTokenObj := &DPoPAccessToken{
		TokenHash: accessHash,
		DeviceID:  dev.ID,
		UserID:    userID,
		BoundJKT:  jkt,
		Scopes:    scopes,
		CreatedAt: now,
		ExpiresAt: now.Add(15 * time.Minute),
	}

	if err := s.store.PutDPoPAccessToken(ctx, accessTokenObj); err != nil {
		return nil, fmt.Errorf("failed to persist DPoP access token: %w", err)
	}

	return &DeviceGrantResult{
		TokenType:    "DPoP",
		AccessToken:  rawAccessToken,
		RefreshToken: rawRefreshToken,
		ExpiresIn:    900,
		DeviceID:     dev.ID,
		Scope:        scopes,
	}, nil
}

// RotateDeviceRefreshToken rotates the refresh token generation and issues a fresh DPoP Access Token.
func (s *Service) RotateDeviceRefreshToken(ctx context.Context, rawRefreshToken, proofJWKThumbprint string) (*DeviceGrantResult, error) {
	now := s.now()
	oldHash := hashToken(rawRefreshToken)

	// 1. Pre-fetch refresh token family record to identify device BEFORE modifying DB state
	family, err := s.store.GetRefreshTokenFamily(ctx, oldHash)
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			return nil, ErrInvalidSessionToken
		}
		return nil, err
	}

	// 2. Fetch associated device
	dev, err := s.store.GetDevice(ctx, family.DeviceID)
	if err != nil {
		return nil, err
	}

	// 3. SECURITY FIX (DoS Prevention): Verify DPoP Key Binding BEFORE rotating token!
	// If an attacker presents a stolen refresh token with an unauthorized DPoP key,
	// REJECT IMMEDIATELY with ErrDPoPBindingMismatch WITHOUT altering SQLite state!
	if dev.JWKThumbprint != proofJWKThumbprint {
		return nil, ErrDPoPBindingMismatch
	}

	newRawRefreshToken := "rt_" + generateRandomHex(32)
	newRefreshHash := hashToken(newRawRefreshToken)

	// 4. Perform atomic rotation only after DPoP binding has been confirmed
	_, err = s.store.RotateRefreshToken(ctx, oldHash, newRefreshHash, now.Add(30*24*time.Hour), now)
	if err != nil {
		return nil, err
	}

	rawAccessToken := "at_dpop_" + generateRandomHex(32)
	accessHash := hashToken(rawAccessToken)

	accessTokenObj := &DPoPAccessToken{
		TokenHash: accessHash,
		DeviceID:  dev.ID,
		UserID:    dev.UserID,
		BoundJKT:  dev.JWKThumbprint,
		Scopes:    "api playback",
		CreatedAt: now,
		ExpiresAt: now.Add(15 * time.Minute),
	}

	if err := s.store.PutDPoPAccessToken(ctx, accessTokenObj); err != nil {
		return nil, err
	}

	return &DeviceGrantResult{
		TokenType:    "DPoP",
		AccessToken:  rawAccessToken,
		RefreshToken: newRawRefreshToken,
		ExpiresIn:    900,
		DeviceID:     dev.ID,
		Scope:        "api playback",
	}, nil
}

// ValidateDPoPAccessToken retrieves and checks a DPoP access token, verifying its cryptographic jkt binding.
func (s *Service) ValidateDPoPAccessToken(ctx context.Context, rawAccessToken, proofJWKThumbprint string) (*DPoPAccessToken, *User, error) {
	accessHash := hashToken(rawAccessToken)
	token, err := s.store.GetDPoPAccessToken(ctx, accessHash)
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			return nil, nil, ErrInvalidSessionToken
		}
		return nil, nil, err
	}

	now := s.now()
	if token.RevokedAt != nil && !token.RevokedAt.IsZero() {
		return nil, nil, ErrSessionRevoked
	}
	if now.After(token.ExpiresAt) {
		return nil, nil, ErrSessionExpired
	}

	// CRITICAL INVARIANT: Verify DPoP Access Token jkt binding
	if token.BoundJKT != proofJWKThumbprint {
		return nil, nil, ErrDPoPBindingMismatch
	}

	user, err := s.store.GetUser(ctx, token.UserID)
	if err != nil {
		return nil, nil, err
	}

	return token, user, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ----------------- Household v1 Methods -----------------

func (s *Service) resolveUser(ctx context.Context, userIDOrUsername string) (*User, error) {
	u, err := s.store.GetUser(ctx, userIDOrUsername)
	if err == nil && u != nil {
		return u, nil
	}
	uByUsername, errUsername := s.store.GetUserByUsername(ctx, userIDOrUsername)
	if errUsername == nil && uByUsername != nil {
		return uByUsername, nil
	}
	return nil, err
}

// CreateInvitation generates a 1-time setup invitation code for a new member or guest.
func (s *Service) CreateInvitation(ctx context.Context, createdByUserID string, role Role, displayName string) (string, *AccountInvitation, error) {
	adminUser, err := s.resolveUser(ctx, createdByUserID)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch admin user: %w", err)
	}

	mem, err := s.store.GetHouseholdMembership(ctx, "default_household", adminUser.ID)
	isAdmin := (err == nil && mem != nil && mem.Role == RoleAdmin) || adminUser.Role == RoleAdmin
	if !isAdmin {
		return "", nil, errors.New("only household admins can create invitations")
	}

	if role != RoleMember && role != RoleGuest {
		return "", nil, errors.New("invitation role must be member or guest")
	}

	codeBytes := make([]byte, 20)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", nil, err
	}
	rawCode := "xg2g_inv_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(codeBytes))
	codeHash := hashToken(rawCode)

	invID := "inv_" + generateRandomHex(12)
	now := s.now()
	inv := &AccountInvitation{
		ID:              invID,
		HouseholdID:     "default_household",
		CodeHash:        codeHash,
		Role:            role,
		DisplayName:     displayName,
		CreatedByUserID: adminUser.ID,
		ExpiresAt:       now.Add(7 * 24 * time.Hour),
	}

	if err := s.store.CreateInvitation(ctx, inv); err != nil {
		return "", nil, err
	}

	return rawCode, inv, nil
}

// RedeemInvitationWithPassword redeems a 1-time invitation using a username and Argon2id password.
func (s *Service) RedeemInvitationWithPassword(ctx context.Context, inviteCode, username, displayName, password string) (*AuthSessionResponse, error) {
	inviteCode = strings.TrimSpace(inviteCode)
	username = strings.ToLower(strings.TrimSpace(username))
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}

	if len(password) < 8 {
		return nil, ErrPasswordTooShort
	}

	codeHash := hashToken(inviteCode)
	inv, err := s.store.GetInvitationByCodeHash(ctx, codeHash)
	if err != nil {
		return nil, ErrInvitationNotFound
	}
	if inv.UsedAt != nil {
		return nil, ErrInvitationAlreadyUsed
	}

	passHash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	now := s.now()
	newUser := &User{
		ID:          "usr_" + generateRandomHex(12),
		Username:    username,
		DisplayName: displayName,
		Role:        inv.Role,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	mem, err := s.store.RedeemInvitationAtomic(ctx, inv.ID, codeHash, newUser, passHash, nil, now)
	if err != nil {
		return nil, err
	}
	_ = mem

	sessID, err := generateSessionToken()
	if err != nil {
		return nil, err
	}
	sess := &WebSession{
		SessionID:    sessID,
		UserID:       newUser.ID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(s.cfg.SessionTTL),
		LastActiveAt: now,
	}
	if err := s.store.PutSession(ctx, sess); err != nil {
		return nil, err
	}

	return &AuthSessionResponse{
		User:      *newUser,
		SessionID: sessID,
		ExpiresAt: sess.ExpiresAt,
	}, nil
}

// AuthenticateWithPassword authenticates a user via Argon2id password hash.
func (s *Service) AuthenticateWithPassword(ctx context.Context, username, password string) (*AuthSessionResponse, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, ErrInvalidPassword
	}

	hash, err := s.store.GetAccountPasswordHash(ctx, user.ID)
	if err != nil || hash == "" {
		return nil, ErrInvalidPassword
	}

	if !VerifyPassword(password, hash) {
		return nil, ErrInvalidPassword
	}

	now := s.now()
	sessID, err := generateSessionToken()
	if err != nil {
		return nil, err
	}
	sess := &WebSession{
		SessionID:    sessID,
		UserID:       user.ID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(s.cfg.SessionTTL),
		LastActiveAt: now,
	}
	if err := s.store.PutSession(ctx, sess); err != nil {
		return nil, err
	}

	return &AuthSessionResponse{
		User:      *user,
		SessionID: sessID,
		ExpiresAt: sess.ExpiresAt,
	}, nil
}

// CreateProfile creates a screen-level viewing profile & policy.
func (s *Service) CreateProfile(ctx context.Context, createdByUserID, name, avatarURL string, isChild bool, allowedBouquets, blockedChannels []string, maturityLevel int, exitPIN string) (*Profile, *ProfilePolicy, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, errors.New("profile name must not be empty")
	}

	creator, err := s.resolveUser(ctx, createdByUserID)
	creatorID := createdByUserID
	if err == nil && creator != nil {
		creatorID = creator.ID
	}

	now := s.now()
	profID := "prof_" + generateRandomHex(12)
	prof := &Profile{
		ID:              profID,
		HouseholdID:     "default_household",
		Name:            name,
		AvatarURL:       avatarURL,
		IsChild:         isChild,
		CreatedByUserID: creatorID,
		CreatedAt:       now,
	}

	var pinHash string
	if exitPIN != "" {
		h, err := HashProfilePIN(exitPIN)
		if err != nil {
			return nil, nil, err
		}
		pinHash = h
	}

	pol := &ProfilePolicy{
		ProfileID:       profID,
		AllowedBouquets: allowedBouquets,
		BlockedChannels: blockedChannels,
		MaturityLevel:   maturityLevel,
		ExitPINHash:     pinHash,
		DVRAllowed:      !isChild,
	}

	if err := s.store.PutProfile(ctx, prof, pol); err != nil {
		return nil, nil, err
	}

	return prof, pol, nil
}

// GetEffectivePermissions computes EffectivePermissions = AccountRole ∩ ProfilePolicy ∩ AccessPolicy ∩ DeviceState.
func (s *Service) GetEffectivePermissions(ctx context.Context, userID, profileID string) (EffectivePermissions, error) {
	user, err := s.resolveUser(ctx, userID)
	if err != nil {
		return EffectivePermissions{}, err
	}

	mem, err := s.store.GetHouseholdMembership(ctx, "default_household", user.ID)
	role := user.Role
	if err == nil && mem != nil {
		role = mem.Role
	}

	var pol *ProfilePolicy
	if profileID != "" {
		_, p, err := s.store.GetProfile(ctx, profileID)
		if err == nil {
			pol = p
		}
	}

	access, _ := s.store.GetAccessPolicy(ctx, user.ID)

	return CalculateEffectivePermissions(user.ID, role, pol, access), nil
}

// CreateAccessPolicy sets or updates an access policy for an account.
func (s *Service) CreateAccessPolicy(ctx context.Context, policy *AccessPolicy) error {
	if policy.ID == "" {
		policy.ID = "acc_pol_" + generateRandomHex(12)
	}
	policy.CreatedAt = s.now()
	return s.store.PutAccessPolicy(ctx, policy)
}

// GetAccessPolicy fetches the active access policy for an account.
func (s *Service) GetAccessPolicy(ctx context.Context, accountID string) (*AccessPolicy, error) {
	return s.store.GetAccessPolicy(ctx, accountID)
}

// RevokeAccessPolicy revokes an access policy immediately.
func (s *Service) RevokeAccessPolicy(ctx context.Context, policyID string) error {
	return s.store.RevokeAccessPolicy(ctx, policyID, s.now())
}

// CreateApprovalRequest creates a pending child content or recording request.
func (s *Service) CreateApprovalRequest(ctx context.Context, profileID, requestType, resourceID, resourceName string, parentalRating int, scope string, expiresAt time.Time) (*ApprovalRequest, error) {
	prof, _, err := s.store.GetProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}

	if expiresAt.IsZero() {
		expiresAt = s.now().Add(24 * time.Hour)
	}

	req := &ApprovalRequest{
		ID:             "appr_" + generateRandomHex(12),
		HouseholdID:    prof.HouseholdID,
		ProfileID:      profileID,
		RequestType:    requestType,
		ResourceID:     resourceID,
		ResourceName:   resourceName,
		ParentalRating: parentalRating,
		Scope:          scope,
		Status:         "pending",
		CreatedAt:      s.now(),
		ExpiresAt:      expiresAt,
	}

	var notifs []*Notification
	var deliveries []*NotificationDelivery

	// Generate per-Admin Notification rows
	if members, mErr := s.store.ListHouseholdMemberships(ctx, prof.HouseholdID); mErr == nil {
		title := "Freigabe erforderlich"
		body := fmt.Sprintf("%s möchte '%s' (FSK %d) ansehen", prof.Name, resourceName, parentalRating)
		for _, m := range members {
			if m.Role == RoleAdmin {
				notif := &Notification{
					ID:             "notif_" + generateRandomHex(12),
					HouseholdID:    prof.HouseholdID,
					UserID:         m.UserID,
					Type:           "approval_request",
					Title:          title,
					Body:           body,
					ResourceID:     req.ID,
					ActionRequired: "approve_content",
					CreatedAt:      s.now(),
					ExpiresAt:      &expiresAt,
				}
				notifs = append(notifs, notif)

				// Queue WebPush deliveries for active subscriptions of this Admin
				if subs, sErr := s.store.ListPushSubscriptions(ctx, prof.HouseholdID, m.UserID); sErr == nil {
					for _, sub := range subs {
						channel := sub.Channel
						if channel == "" {
							channel = "webpush"
						}
						deliv := &NotificationDelivery{
							ID:             "deliv_" + generateRandomHex(12),
							NotificationID: notif.ID,
							Channel:        channel,
							EndpointID:     sub.Endpoint,
							Status:         "queued",
							AttemptCount:   0,
						}
						deliveries = append(deliveries, deliv)
					}
				}
			}
		}
	}

	if err := s.store.CreateApprovalRequestWithNotifications(ctx, req, notifs, deliveries); err != nil {
		return nil, fmt.Errorf("atomic approval creation failed: %w", err)
	}

	// Trigger async background push worker (decoupled from HTTP request context)
	go func() {
		_ = s.ProcessNotificationQueue(context.Background())
	}()

	return req, nil
}

// ListApprovalRequests lists approval requests by status (e.g. "pending").
func (s *Service) ListApprovalRequests(ctx context.Context, householdID, status string) ([]ApprovalRequest, error) {
	if householdID == "" {
		householdID = "default_household"
	}
	return s.store.ListApprovalRequests(ctx, householdID, status)
}

// ApproveRequest approves a pending approval request.
func (s *Service) ApproveRequest(ctx context.Context, requestID, adminUserID string) error {
	return s.store.SettleApprovalRequest(ctx, requestID, "approved", adminUserID, s.now())
}

// DenyRequest denies a pending approval request.
func (s *Service) DenyRequest(ctx context.Context, requestID, adminUserID string) error {
	return s.store.SettleApprovalRequest(ctx, requestID, "denied", adminUserID, s.now())
}

// ----------------- Notification Service Methods -----------------

func (s *Service) ListNotifications(ctx context.Context, householdID, userID string, unreadOnly bool) ([]Notification, error) {
	return s.store.ListNotifications(ctx, householdID, userID, unreadOnly, 50)
}

func (s *Service) MarkNotificationRead(ctx context.Context, id, userID string) error {
	return s.store.MarkNotificationRead(ctx, id, userID, s.now())
}

func (s *Service) MarkAllNotificationsRead(ctx context.Context, householdID, userID string) error {
	return s.store.MarkAllNotificationsRead(ctx, householdID, userID, s.now())
}

func (s *Service) DeleteNotification(ctx context.Context, id, userID string) error {
	return s.store.DeleteNotification(ctx, id, userID)
}

func (s *Service) SavePushSubscription(ctx context.Context, sub *PushSubscription) error {
	if sub.ID == "" {
		sub.ID = "sub_" + generateRandomHex(12)
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = s.now()
	}
	return s.store.SavePushSubscription(ctx, sub)
}

// PutHouseholdResourcePolicy updates concurrency limits for a household.
func (s *Service) PutHouseholdResourcePolicy(ctx context.Context, policy *HouseholdResourcePolicy) error {
	if policy.HouseholdID == "" {
		policy.HouseholdID = "default_household"
	}
	policy.UpdatedAt = s.now()
	return s.store.PutHouseholdResourcePolicy(ctx, policy)
}

// GetHouseholdResourcePolicy retrieves current concurrency limits for a household.
func (s *Service) GetHouseholdResourcePolicy(ctx context.Context, householdID string) (*HouseholdResourcePolicy, error) {
	if householdID == "" {
		householdID = "default_household"
	}
	return s.store.GetHouseholdResourcePolicy(ctx, householdID)
}

// PutRecordingProfileAccess sets profile access visibility for virtual recording library items.
func (s *Service) PutRecordingProfileAccess(ctx context.Context, recordingID string, profileIDs []string) error {
	return s.store.PutRecordingProfileAccess(ctx, recordingID, profileIDs)
}

// GetRecordingProfileAccess retrieves profile access list for a recording.
func (s *Service) GetRecordingProfileAccess(ctx context.Context, recordingID string) ([]string, error) {
	return s.store.GetRecordingProfileAccess(ctx, recordingID)
}

// RevokeAllUserSessions revokes all active web sessions for a target user.
func (s *Service) RevokeAllUserSessions(ctx context.Context, userID string) error {
	return s.store.RevokeAllUserSessions(ctx, userID, s.now())
}
