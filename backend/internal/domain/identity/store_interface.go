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
	ReplaceRecoveryCodesForUser(ctx context.Context, userID string, codes []RecoveryCode) error
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

// DeviceStore owns native Android device registrations, DPoP grants, and token family rotation.
type DeviceStore interface {
	PutDevice(ctx context.Context, dev *Device) error
	GetDevice(ctx context.Context, id string) (*Device, error)
	GetDeviceByThumbprint(ctx context.Context, thumbprint string) (*Device, error)
	ListDevicesByUser(ctx context.Context, userID string) ([]Device, error)
	PutDeviceGrant(ctx context.Context, grant *DeviceGrant, initialToken *RefreshTokenFamily) error
	GetDeviceGrant(ctx context.Context, grantID string) (*DeviceGrant, error)
	GetRefreshTokenFamily(ctx context.Context, tokenHash string) (*RefreshTokenFamily, error)
	RotateRefreshToken(ctx context.Context, oldTokenHash, newTokenHash string, newExpiresAt, now time.Time) (*RefreshTokenFamily, error)
	RevokeDeviceGrantFamily(ctx context.Context, familyID string, now time.Time) error
	PutDPoPAccessToken(ctx context.Context, token *DPoPAccessToken) error
	GetDPoPAccessToken(ctx context.Context, tokenHash string) (*DPoPAccessToken, error)
	RevokeDPoPAccessToken(ctx context.Context, tokenHash string, now time.Time) error
}

// HouseholdStore owns household membership, password auth, invitation, and profile persistence.
type HouseholdStore interface {
	GetHousehold(ctx context.Context, id string) (*Household, error)
	GetHouseholdMembership(ctx context.Context, householdID, userID string) (*HouseholdMembership, error)
	ListHouseholdMemberships(ctx context.Context, householdID string) ([]HouseholdMembership, error)
	PutHouseholdMembership(ctx context.Context, membership *HouseholdMembership) error

	PutAccountPassword(ctx context.Context, userID, passwordHash string, now time.Time) error
	GetAccountPasswordHash(ctx context.Context, userID string) (string, error)

	CreateInvitation(ctx context.Context, invite *AccountInvitation) error
	GetInvitationByCodeHash(ctx context.Context, codeHash string) (*AccountInvitation, error)
	RedeemInvitationAtomic(ctx context.Context, inviteID, codeHash string, user *User, passwordHash string, passkey *PasskeyCredential, now time.Time) (*HouseholdMembership, error)

	PutProfile(ctx context.Context, profile *Profile, policy *ProfilePolicy) error
	GetProfile(ctx context.Context, profileID string) (*Profile, *ProfilePolicy, error)
	ListProfilesByHousehold(ctx context.Context, householdID string) ([]Profile, error)
	DeleteProfile(ctx context.Context, profileID string) error

	PutAccessPolicy(ctx context.Context, policy *AccessPolicy) error
	GetAccessPolicy(ctx context.Context, accountID string) (*AccessPolicy, error)
	RevokeAccessPolicy(ctx context.Context, id string, now time.Time) error

	CreateApprovalRequest(ctx context.Context, req *ApprovalRequest) error
	GetApprovalRequest(ctx context.Context, id string) (*ApprovalRequest, error)
	ListApprovalRequests(ctx context.Context, householdID, status string) ([]ApprovalRequest, error)
	SettleApprovalRequest(ctx context.Context, id, status, approvedByUserID string, approvedAt time.Time) error

	CreateNotification(ctx context.Context, n *Notification) error
	ListNotifications(ctx context.Context, householdID, userID string, unreadOnly bool, limit int) ([]Notification, error)
	MarkNotificationRead(ctx context.Context, id, userID string, readAt time.Time) error
	MarkAllNotificationsRead(ctx context.Context, householdID, userID string, readAt time.Time) error
	DeleteNotification(ctx context.Context, id, userID string) error

	SavePushSubscription(ctx context.Context, sub *PushSubscription) error
	ListPushSubscriptions(ctx context.Context, householdID, userID string) ([]PushSubscription, error)
	DeletePushSubscription(ctx context.Context, id, userID string) error
	DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) error
	RecordNotificationDelivery(ctx context.Context, delivery *NotificationDelivery) error
	GetPendingNotificationDeliveries(ctx context.Context, limit int) ([]NotificationDelivery, error)
	UpdateNotificationDelivery(ctx context.Context, delivery *NotificationDelivery) error

	PutHouseholdResourcePolicy(ctx context.Context, policy *HouseholdResourcePolicy) error
	GetHouseholdResourcePolicy(ctx context.Context, householdID string) (*HouseholdResourcePolicy, error)

	PutRecordingProfileAccess(ctx context.Context, recordingID string, profileIDs []string) error
	GetRecordingProfileAccess(ctx context.Context, recordingID string) ([]string, error)

	RevokeAllUserSessions(ctx context.Context, userID string, now time.Time) error
}

// Store is the combined identity persistence interface.
type Store interface {
	UserStore
	PasskeyStore
	RecoveryStore
	WebSessionStore
	BootstrapStore
	DeviceStore
	HouseholdStore
	Close() error
}
