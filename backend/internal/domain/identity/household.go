// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"errors"
	"time"
)

var (
	ErrHouseholdNotFound     = errors.New("household not found")
	ErrInvitationNotFound    = errors.New("invitation not found or expired")
	ErrInvitationAlreadyUsed = errors.New("invitation already used")
	ErrProfileNotFound       = errors.New("profile not found")
	ErrInvalidPassword       = errors.New("invalid password")
	ErrPasswordTooShort      = errors.New("password must be at least 8 characters long")
	ErrProfilePINMismatch    = errors.New("invalid profile exit pin")
)

// Household represents a tenant container for accounts and profiles.
type Household struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// HouseholdMembership binds a user to a household with a role.
type HouseholdMembership struct {
	HouseholdID string    `json:"householdId"`
	UserID      string    `json:"userId"`
	Role        Role      `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

// AccountInvitation is a 1-time setup token for new household members or guests.
type AccountInvitation struct {
	ID              string     `json:"id"`
	HouseholdID     string     `json:"householdId"`
	CodeHash        string     `json:"-"`
	Role            Role       `json:"role"`
	DisplayName     string     `json:"displayName,omitempty"`
	CreatedByUserID string     `json:"createdByUserId"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	UsedAt          *time.Time `json:"usedAt,omitempty"`
}

// Profile is a screen-level viewing context on household devices.
type Profile struct {
	ID              string    `json:"id"`
	HouseholdID     string    `json:"householdId"`
	Name            string    `json:"name"`
	AvatarURL       string    `json:"avatarUrl,omitempty"`
	IsChild         bool      `json:"isChild"`
	CreatedByUserID string    `json:"createdByUserId"`
	CreatedAt       time.Time `json:"createdAt"`
}

// ViewingTimeWindow restricts viewing hours for child profiles.
type ViewingTimeWindow struct {
	Start string `json:"start"` // e.g. "08:00"
	End   string `json:"end"`   // e.g. "20:00"
}

// ProfilePolicy restricts content & capabilities for a profile.
type ProfilePolicy struct {
	ProfileID         string             `json:"profileId"`
	AllowedBouquets   []string           `json:"allowedBouquets,omitempty"`
	BlockedChannels   []string           `json:"blockedChannels,omitempty"`
	MaturityLevel     int                `json:"maturityLevel"`
	ExitPINHash       string             `json:"-"`
	DVRAllowed        bool               `json:"dvrAllowed"`
	ViewingTimeWindow *ViewingTimeWindow `json:"viewingTimeWindow,omitempty"`
}

// EffectivePermissions is the evaluated intersection: AccountPermissions ∩ ProfilePolicy.
type EffectivePermissions struct {
	UserID          string   `json:"userId"`
	Role            Role     `json:"role"`
	IsAdmin         bool     `json:"isAdmin"`
	IsMember        bool     `json:"isMember"`
	IsGuest         bool     `json:"isGuest"`
	CanManageUser   bool     `json:"canManageUser"`
	CanConfig       bool     `json:"canConfig"`
	CanDVR          bool     `json:"canDvr"`
	AllowedBouquets []string `json:"allowedBouquets,omitempty"`
	BlockedChannels []string `json:"blockedChannels,omitempty"`
	ProfileID       string   `json:"profileId,omitempty"`
	ProfileName     string   `json:"profileName,omitempty"`
	IsChildProfile  bool     `json:"isChildProfile"`
}

// CalculateEffectivePermissions computes EffectivePermissions = AccountPermissions ∩ ProfilePolicy.
// Note: Profile selection can ONLY restrict permissions, NEVER expand account-level rights!
func CalculateEffectivePermissions(userID string, role Role, policy *ProfilePolicy) EffectivePermissions {
	perms := EffectivePermissions{
		UserID: userID,
		Role:   role,
	}

	switch role {
	case RoleAdmin:
		perms.IsAdmin = true
		perms.IsMember = true
		perms.CanManageUser = true
		perms.CanConfig = true
		perms.CanDVR = true
	case RoleMember:
		perms.IsMember = true
		perms.CanDVR = true
	case RoleGuest, RoleViewer:
		perms.IsGuest = true
		perms.CanDVR = false
	}

	if policy != nil {
		perms.ProfileID = policy.ProfileID
		perms.IsChildProfile = policy.MaturityLevel > 0

		// Intersection: Profile can ONLY restrict capabilities
		if !policy.DVRAllowed {
			perms.CanDVR = false
		}
		perms.AllowedBouquets = policy.AllowedBouquets
		perms.BlockedChannels = policy.BlockedChannels
	}

	return perms
}
