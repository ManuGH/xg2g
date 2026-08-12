// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"context"
	"errors"
	"time"
)

var (
	ErrStoreNotFound = errors.New("identity store: record not found")
	ErrStoreConflict = errors.New("identity store: record conflict")
)

// UserStore owns user account persistence.
type UserStore interface {
	PutUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUser(ctx context.Context, id string, fn func(*User) error) (*User, error)
	DeleteUser(ctx context.Context, id string) error
}

// PasskeyStore owns WebAuthn credential persistence.
type PasskeyStore interface {
	PutPasskey(ctx context.Context, passkey *PasskeyCredential) error
	GetPasskey(ctx context.Context, id string) (*PasskeyCredential, error)
	ListPasskeysByUser(ctx context.Context, userID string) ([]PasskeyCredential, error)
	UpdatePasskey(ctx context.Context, id string, signCount uint32, backupState bool, lastUsedAt time.Time) error
	DeletePasskey(ctx context.Context, id string) error
}

// RecoveryStore owns single-use recovery code hashes.
type RecoveryStore interface {
	PutRecoveryCodes(ctx context.Context, codes []RecoveryCode) error
	ListRecoveryCodesByUser(ctx context.Context, userID string) ([]RecoveryCode, error)
	ConsumeRecoveryCode(ctx context.Context, userID, codeHash string, now time.Time) error
}

// WebSessionStore owns persistent web browser sessions.
type WebSessionStore interface {
	PutSession(ctx context.Context, session *WebSession) error
	GetSession(ctx context.Context, sessionID string) (*WebSession, error)
	TouchSession(ctx context.Context, sessionID string, now time.Time, userAgent, ip string) error
	RevokeSession(ctx context.Context, sessionID string, now time.Time) error
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID string, now time.Time) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}

// BootstrapStore owns setup tokens and initial admin bootstrap state.
type BootstrapStore interface {
	PutBootstrapToken(ctx context.Context, tokenHash string, createdAt, expiresAt time.Time) error
	ConsumeBootstrapToken(ctx context.Context, tokenHash string, now time.Time) (bool, error)
	CommitInitialAdminBootstrap(ctx context.Context, user *User, cred *PasskeyCredential, recoveryCodes []RecoveryCode, session *WebSession) error
	AcknowledgeRecoveryCodes(ctx context.Context, userID string, now time.Time) error
	GetBootstrapMeta(ctx context.Context) (*BootstrapMeta, error)
}

// Store is the combined identity persistence interface.
type Store interface {
	UserStore
	PasskeyStore
	RecoveryStore
	WebSessionStore
	BootstrapStore
	Close() error
}
