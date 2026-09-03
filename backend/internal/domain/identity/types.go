// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidUserID                    = errors.New("user id must not be empty")
	ErrInvalidUsername                  = errors.New("username must not be empty")
	ErrInvalidDisplayName               = errors.New("display name must not be empty")
	ErrInvalidRole                      = errors.New("invalid user role")
	ErrInvalidCredentialID              = errors.New("credential id must not be empty")
	ErrInvalidPublicKey                 = errors.New("public key must not be empty")
	ErrInvalidBackupState               = errors.New("invalid backup state: backup state cannot be true when backup eligible is false")
	ErrInvalidRecoveryCode              = errors.New("recovery code must not be empty")
	ErrInvalidSessionID                 = errors.New("session id must not be empty")
	ErrSessionExpired                   = errors.New("session is expired")
	ErrSessionRevoked                   = errors.New("session is revoked")
	ErrUserNotFound                     = errors.New("user not found")
	ErrCredentialNotFound               = errors.New("passkey credential not found")
	ErrRecoveryCodeNotFound             = errors.New("recovery code not found or already consumed")
	ErrAdminAlreadyExists               = errors.New("admin user already initialized; unauthenticated bootstrap is forbidden")
	ErrBootstrapUnauthorized            = errors.New("operator authorization required for initial admin bootstrap")
	ErrSetupNotAuthorized               = errors.New("setup not authorized: valid setup token or operator authorization required")
	ErrBootstrapTokenExpired            = errors.New("bootstrap token is expired or invalid")
	ErrBootstrapTokenInvalid            = errors.New("invalid bootstrap token")
	ErrRecoveryCodesAlreadyAcknowledged = errors.New("recovery codes have already been acknowledged")
	ErrRefreshTokenReplay               = errors.New("refresh token replay detected: entire device grant family revoked")
	ErrDPoPBindingMismatch              = errors.New("dpop key binding mismatch: access token bound_jkt does not match proof jwk thumbprint")
	ErrInvalidDeviceID                  = errors.New("no such device")
)

type BootstrapState string

const (
	BootstrapStateSetupRequired   BootstrapState = "setup_required"
	BootstrapStateSetupInProgress BootstrapState = "setup_in_progress"
	BootstrapStateReady           BootstrapState = "ready"
)

// BootstrapMeta tracks the system-wide bootstrap and recovery acknowledgment state.
type BootstrapMeta struct {
	InitialAdminID              string     `json:"initialAdminId,omitempty"`
	RecoveryCodesAcknowledgedAt *time.Time `json:"recoveryCodesAcknowledgedAt,omitempty"`
	BootstrapClosed             bool       `json:"bootstrapClosed"`
	CreatedAt                   time.Time  `json:"createdAt"`
	UpdatedAt                   time.Time  `json:"updatedAt"`
}

// BootstrapToken represents a persistent, one-time setup token created via CLI.
type BootstrapToken struct {
	TokenHash  string     `json:"tokenHash"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	ConsumedAt *time.Time `json:"consumedAt,omitempty"`
}

// BootstrapResult contains the result of the initial passkey registration ceremony.
type BootstrapResult struct {
	User          User              `json:"user"`
	Credential    PasskeyCredential `json:"credential"`
	RecoveryCodes []string          `json:"recoveryCodes"`
	SessionID     string            `json:"-"`
	ExpiresAt     time.Time         `json:"expiresAt"`
}

// AuthSessionResponse contains user metadata and active session expiry.
type AuthSessionResponse struct {
	User      User      `json:"user"`
	SessionID string    `json:"sessionId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleGuest  Role = "guest"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleMember, RoleGuest, RoleViewer:
		return true
	default:
		return false
	}
}

// User represents a human operator or household member identity.
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// NormalizeUsername returns the canonical representation of a username.
// It trims surrounding whitespace and converts to lowercase.
func NormalizeUsername(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func (u User) Normalize() (User, error) {
	u.ID = strings.TrimSpace(u.ID)
	u.Username = NormalizeUsername(u.Username)
	u.DisplayName = strings.TrimSpace(u.DisplayName)
	if u.ID == "" {
		return User{}, ErrInvalidUserID
	}
	if u.Username == "" {
		return User{}, ErrInvalidUsername
	}
	if u.DisplayName == "" {
		u.DisplayName = u.Username
	}
	if !u.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = u.CreatedAt
	}
	return u, nil
}

// PasskeyCredential represents a registered WebAuthn / FIDO2 credential.
type PasskeyCredential struct {
	ID              string     `json:"id"`              // Base64URL-encoded credential ID
	UserID          string     `json:"userId"`          // Owner user ID
	PublicKey       []byte     `json:"-"`               // DER-encoded SubjectPublicKeyInfo (SPKI)
	AttestationType string     `json:"attestationType"` // "none", "packed", "fido-u2f", etc.
	AAGUID          string     `json:"aaguid"`          // Hex-formatted 16-byte AAGUID
	SignCount       uint32     `json:"signCount"`       // Authenticator signature counter
	BackupEligible  bool       `json:"backupEligible"`  // WebAuthn L3 BE flag
	BackupState     bool       `json:"backupState"`     // WebAuthn L3 BS flag
	Transports      []string   `json:"transports"`      // e.g. ["internal", "hybrid", "usb"]
	Nickname        string     `json:"nickname"`        // e.g. "MacBook Touch ID", "iPhone Face ID"
	CreatedAt       time.Time  `json:"createdAt"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
}

func (c PasskeyCredential) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return ErrInvalidCredentialID
	}
	if strings.TrimSpace(c.UserID) == "" {
		return ErrInvalidUserID
	}
	if len(c.PublicKey) == 0 {
		return ErrInvalidPublicKey
	}
	// WebAuthn Invariant: BS=1 is forbidden if BE=0
	if c.BackupState && !c.BackupEligible {
		return ErrInvalidBackupState
	}
	return nil
}

// RecoveryCode represents a single-use account emergency recovery secret.
type RecoveryCode struct {
	CodeHash   string     `json:"codeHash"` // SHA-256 hash of normalized code
	UserID     string     `json:"userId"`
	CreatedAt  time.Time  `json:"createdAt"`
	ConsumedAt *time.Time `json:"consumedAt,omitempty"`
}

// WebSession represents a persistent, server-side authenticated web session.
type WebSession struct {
	SessionID    string     `json:"sessionId"` // 32-byte CSPRNG Base64URL token
	UserID       string     `json:"userId"`
	UserAgent    string     `json:"userAgent,omitempty"`
	LastIP       string     `json:"lastIp,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	LastActiveAt time.Time  `json:"lastActiveAt"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
}

func (s WebSession) IsActive(now time.Time) bool {
	if s.RevokedAt != nil && !s.RevokedAt.IsZero() {
		return false
	}
	if now.After(s.ExpiresAt) {
		return false
	}
	return true
}

// Device represents a registered native app/device (Android App, Android TV).
type Device struct {
	ID            string    `json:"id"`            // Device ID (e.g. dev_xxxx)
	UserID        string    `json:"userId"`        // Owner user ID
	DeviceName    string    `json:"deviceName"`    // e.g. "Pixel 8 Pro", "NVIDIA Shield"
	Platform      string    `json:"platform"`      // "android", "android_tv"
	PublicKeyJWK  string    `json:"publicKeyJwk"`  // Full P-256 public key in JWK format
	JWKThumbprint string    `json:"jwkThumbprint"` // RFC 7638 SHA-256 JWK thumbprint (jkt)
	CreatedAt     time.Time `json:"createdAt"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
}

// GrantType records which enrollment front-end authorised this device.
//
// Both front-ends produce the same kind of grant on the same substrate — that
// is the point of the convergence — but a grant must not claim to have come
// from a mechanism it did not. Storing every grant as "passkey" made the
// enrollment path unauditable.
type GrantType string

const (
	// GrantTypePasskeyEnrollment: the user proved identity with a passkey and
	// enrolled the device directly.
	//
	// The stored value keeps its historical spelling so existing grants stay
	// valid; the value is data, the type is the contract.
	GrantTypePasskeyEnrollment GrantType = "passkey_device_grant"

	// GrantTypePairingEnrollment: the device was authorised through the pairing
	// flow — a code or QR payload approved by an already-authenticated user.
	GrantTypePairingEnrollment GrantType = "pairing_device_grant"
)

// DeviceGrant represents a persistent grant session issued to a specific device.
type DeviceGrant struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"deviceId"`
	UserID    string     `json:"userId"`
	FamilyID  string     `json:"familyId"`
	GrantType GrantType  `json:"grantType"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

// RefreshTokenFamily tracks rotating refresh tokens for a specific device.
type RefreshTokenFamily struct {
	TokenHash        string     `json:"tokenHash"`
	FamilyID         string     `json:"familyId"`
	DeviceID         string     `json:"deviceId"`
	Generation       int        `json:"generation"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	RotatedAt        *time.Time `json:"rotatedAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	ReplayDetectedAt *time.Time `json:"replayDetectedAt,omitempty"`
}

// DPoPAccessToken represents an opaque access token bound to a specific RFC 7638 JWK thumbprint (jkt).
type DPoPAccessToken struct {
	TokenHash string     `json:"tokenHash"`
	DeviceID  string     `json:"deviceId"`
	UserID    string     `json:"userId"`
	BoundJKT  string     `json:"boundJkt"` // RFC 7638 JWK Thumbprint
	Scopes    string     `json:"scopes"`   // e.g. "api playback"
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}
