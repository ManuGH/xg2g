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
	ErrInvalidUserID         = errors.New("user id must not be empty")
	ErrInvalidUsername       = errors.New("username must not be empty")
	ErrInvalidDisplayName    = errors.New("display name must not be empty")
	ErrInvalidRole           = errors.New("invalid user role")
	ErrInvalidCredentialID   = errors.New("credential id must not be empty")
	ErrInvalidPublicKey      = errors.New("public key must not be empty")
	ErrInvalidBackupState    = errors.New("invalid backup state: backup state cannot be true when backup eligible is false")
	ErrInvalidRecoveryCode   = errors.New("recovery code must not be empty")
	ErrInvalidSessionID      = errors.New("session id must not be empty")
	ErrSessionExpired        = errors.New("session is expired")
	ErrSessionRevoked        = errors.New("session is revoked")
	ErrUserNotFound          = errors.New("user not found")
	ErrCredentialNotFound    = errors.New("passkey credential not found")
	ErrRecoveryCodeNotFound  = errors.New("recovery code not found or already consumed")
	ErrAdminAlreadyExists    = errors.New("admin user already initialized; unauthenticated bootstrap is forbidden")
	ErrBootstrapUnauthorized = errors.New("operator authorization required for initial admin bootstrap")
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleMember, RoleViewer:
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

func (u User) Normalize() (User, error) {
	u.ID = strings.TrimSpace(u.ID)
	u.Username = strings.ToLower(strings.TrimSpace(u.Username))
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
