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
	ErrAccessPolicyRevoked   = errors.New("access policy has been revoked")
	ErrAccessPolicyExpired   = errors.New("access policy has expired")
	ErrOutsideTimeWindow     = errors.New("access is restricted outside allowed time window")
	ErrApprovalNotFound      = errors.New("approval request not found")
	ErrApprovalAlreadySettled = errors.New("approval request is already approved or denied")
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
	ID                  string     `json:"id"`
	HouseholdID         string     `json:"householdId"`
	Name                string     `json:"name"`
	AvatarURL           string     `json:"avatarUrl,omitempty"`
	IsChild             bool       `json:"isChild"`
	DateOfBirth         *time.Time `json:"dateOfBirth,omitempty"`
	MaxParentalRating   int        `json:"maxParentalRating"`
	UnknownRatingPolicy string     `json:"unknownRatingPolicy"` // "block", "request_approval", "allow"
	StorageQuotaBytes   int64      `json:"storageQuotaBytes"`
	CreatedByUserID     string     `json:"createdByUserId"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// Age returns the profile's dynamic age in years based on DateOfBirth, or -1 if unconfigured.
func (p Profile) Age() int {
	if p.DateOfBirth == nil {
		return -1
	}
	now := time.Now()
	years := now.Year() - p.DateOfBirth.Year()
	if now.YearDay() < p.DateOfBirth.YearDay() {
		years--
	}
	if years < 0 {
		return 0
	}
	return years
}

// AccessPolicy defines account-level time window, expiration, and capability restrictions.
type AccessPolicy struct {
	ID                  string     `json:"id"`
	AccountID           string     `json:"accountId"`
	ValidFrom           *time.Time `json:"validFrom,omitempty"`
	ValidUntil          *time.Time `json:"validUntil,omitempty"`
	DailyStart          string     `json:"dailyStart,omitempty"` // e.g. "07:00"
	DailyEnd            string     `json:"dailyEnd,omitempty"`   // e.g. "19:00"
	Timezone            string     `json:"timezone"`             // e.g. "Europe/Vienna"
	AllowedDaysMask     int        `json:"allowedDaysMask"`      // Bitmask for Mon=1, Tue=2, Wed=4, Thu=8, Fri=16, Sat=32, Sun=64 (127 = all days)
	LiveTVAllowed       bool       `json:"liveTvAllowed"`
	EPGAllowed          bool       `json:"epgAllowed"`
	DVRAllowed          bool       `json:"dvrAllowed"`
	RecordingsAllowed   bool       `json:"recordingsAllowed"`
	MaxDevices          int        `json:"maxDevices"`
	RevokedAt           *time.Time `json:"revokedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// ApprovalRequest represents a child request for locked content or recordings awaiting admin approval.
type ApprovalRequest struct {
	ID              string     `json:"id"`
	HouseholdID     string     `json:"householdId"`
	ProfileID       string     `json:"profileId"`
	RequestType     string     `json:"requestType"` // "view_content", "record_content", "time_extension"
	ResourceID      string     `json:"resourceId"`
	ResourceName    string     `json:"resourceName"`
	ParentalRating  int        `json:"parentalRating"`
	Scope           string     `json:"scope"` // "single", "today", "series", "permanent"
	Status          string     `json:"status"` // "pending", "approved", "denied", "expired"
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	ApprovedByUserID string    `json:"approvedByUserId,omitempty"`
	ApprovedAt      *time.Time `json:"approvedAt,omitempty"`
}

// HouseholdResourcePolicy defines system-wide concurrency limits and arbitration thresholds.
type HouseholdResourcePolicy struct {
	HouseholdID               string    `json:"householdId"`
	MaxConcurrentLiveServices int       `json:"maxConcurrentLiveServices"`
	MaxConcurrentViewers      int       `json:"maxConcurrentViewers"`
	MaxParallelRecordings     int       `json:"maxParallelRecordings"`
	MaxParallelTranscodes     int       `json:"maxParallelTranscodes"`
	PreemptionEnabled         bool      `json:"preemptionEnabled"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

// RecordingProfileAccess maps selected profile visibility for virtual recording libraries.
type RecordingProfileAccess struct {
	RecordingID string `json:"recordingId"`
	ProfileID   string `json:"profileId"`
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

// EffectivePermissions is the evaluated intersection:
// AccountRole(admin/member/guest) ∩ ProfilePolicy ∩ AccessPolicy ∩ DeviceState
type EffectivePermissions struct {
	UserID          string   `json:"userId"`
	Role            Role     `json:"role"`
	IsAdmin         bool     `json:"isAdmin"`
	IsMember        bool     `json:"isMember"`
	IsGuest         bool     `json:"isGuest"`
	CanManageUser   bool     `json:"canManageUser"`
	CanConfig       bool     `json:"canConfig"`

	// Four First-Class Product Capabilities
	LiveTV          bool     `json:"liveTv"`
	Record          bool     `json:"record"`
	WatchRecordings bool     `json:"watchRecordings"`
	EPG             bool     `json:"epg"`
	CanDVR          bool     `json:"canDvr"` // Legacy alias for Record

	AllowedBouquets []string `json:"allowedBouquets,omitempty"`
	BlockedChannels []string `json:"blockedChannels,omitempty"`
	ProfileID       string   `json:"profileId,omitempty"`
	ProfileName     string   `json:"profileName,omitempty"`
	IsChildProfile  bool     `json:"isChildProfile"`
}

// CalculateEffectivePermissions computes EffectivePermissions.
// ProfilePolicy and AccessPolicy can ONLY restrict capabilities, NEVER expand account role rights!
func CalculateEffectivePermissions(userID string, role Role, policy *ProfilePolicy, access *AccessPolicy) EffectivePermissions {
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
		perms.LiveTV = true
		perms.Record = true
		perms.WatchRecordings = true
		perms.EPG = true
	case RoleMember:
		perms.IsMember = true
		perms.LiveTV = true
		perms.Record = true
		perms.WatchRecordings = true
		perms.EPG = true
	case RoleGuest, RoleViewer:
		perms.IsGuest = true
		perms.LiveTV = true
		perms.Record = false
		perms.WatchRecordings = false
		perms.EPG = true
	}

	// Apply AccessPolicy restrictions if present
	if access != nil {
		if access.RevokedAt != nil {
			perms.LiveTV = false
			perms.Record = false
			perms.WatchRecordings = false
			perms.EPG = false
		} else {
			if !access.LiveTVAllowed {
				perms.LiveTV = false
			}
			if !access.DVRAllowed {
				perms.Record = false
			}
			if !access.RecordingsAllowed {
				perms.WatchRecordings = false
			}
			if !access.EPGAllowed {
				perms.EPG = false
			}
		}
	}

	// Apply ProfilePolicy restrictions if present
	if policy != nil {
		perms.ProfileID = policy.ProfileID
		perms.IsChildProfile = policy.MaturityLevel > 0

		if !policy.DVRAllowed {
			perms.Record = false
		}
		perms.AllowedBouquets = policy.AllowedBouquets
		perms.BlockedChannels = policy.BlockedChannels
	}

	// Keep CanDVR synchronized with Record for backward compatibility
	perms.CanDVR = perms.Record

	return perms
}
