// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"errors"
	"time"
)

var (
	ErrHouseholdNotFound      = errors.New("household not found")
	ErrInvitationNotFound     = errors.New("invitation not found or expired")
	ErrInvitationAlreadyUsed  = errors.New("invitation already used")
	ErrProfileNotFound        = errors.New("profile not found")
	ErrInvalidPassword        = errors.New("invalid password")
	ErrPasswordTooShort       = errors.New("password must be at least 8 characters long")
	ErrProfilePINMismatch     = errors.New("invalid profile exit pin")
	ErrAccessPolicyRevoked    = errors.New("access policy has been revoked")
	ErrAccessPolicyExpired    = errors.New("access policy has expired")
	ErrOutsideTimeWindow      = errors.New("access is restricted outside allowed time window")
	ErrApprovalNotFound       = errors.New("approval request not found")
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
	ID                string     `json:"id"`
	AccountID         string     `json:"accountId"`
	ValidFrom         *time.Time `json:"validFrom,omitempty"`
	ValidUntil        *time.Time `json:"validUntil,omitempty"`
	DailyStart        string     `json:"dailyStart,omitempty"` // e.g. "07:00"
	DailyEnd          string     `json:"dailyEnd,omitempty"`   // e.g. "19:00"
	Timezone          string     `json:"timezone"`             // e.g. "Europe/Vienna"
	AllowedDaysMask   int        `json:"allowedDaysMask"`      // Bitmask for Mon=1, Tue=2, Wed=4, Thu=8, Fri=16, Sat=32, Sun=64 (127 = all days)
	LiveTVAllowed     bool       `json:"liveTvAllowed"`
	EPGAllowed        bool       `json:"epgAllowed"`
	DVRAllowed        bool       `json:"dvrAllowed"`
	RecordingsAllowed bool       `json:"recordingsAllowed"`
	MaxDevices        int        `json:"maxDevices"`
	RevokedAt         *time.Time `json:"revokedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

// ApprovalRequest represents a child request for locked content or recordings awaiting admin approval.
type ApprovalRequest struct {
	ID               string     `json:"id"`
	HouseholdID      string     `json:"householdId"`
	ProfileID        string     `json:"profileId"`
	RequestType      string     `json:"requestType"` // "view_content", "record_content", "time_extension"
	ResourceID       string     `json:"resourceId"`
	ResourceName     string     `json:"resourceName"`
	ParentalRating   int        `json:"parentalRating"`
	Scope            string     `json:"scope"`  // "single", "today", "series", "permanent"
	Status           string     `json:"status"` // "pending", "approved", "denied", "expired"
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	ApprovedByUserID string     `json:"approvedByUserId,omitempty"`
	ApprovedAt       *time.Time `json:"approvedAt,omitempty"`
}

// HouseholdResourcePolicy defines system-wide concurrency limits and arbitration thresholds.
type HouseholdResourcePolicy struct {
	HouseholdID               string    `json:"householdId"`
	MaxConcurrentLiveServices int       `json:"maxConcurrentLiveServices"`
	MaxConcurrentViewers      int       `json:"maxConcurrentViewers"`
	MaxParallelRecordings     int       `json:"maxParallelRecordings"`
	MaxParallelTranscodes     int       `json:"maxParallelTranscodes"`
	PreemptionEnabled         bool      `json:"preemptionEnabled"`
	PreemptionPriorityRanks   []string  `json:"preemptionPriorityRanks,omitempty"`
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
	UserID        string `json:"userId"`
	Role          Role   `json:"role"`
	IsAdmin       bool   `json:"isAdmin"`
	IsMember      bool   `json:"isMember"`
	IsGuest       bool   `json:"isGuest"`
	CanManageUser bool   `json:"canManageUser"`
	CanConfig     bool   `json:"canConfig"`

	// Four First-Class Product Capabilities
	LiveTV          bool `json:"liveTv"`
	Record          bool `json:"record"`
	WatchRecordings bool `json:"watchRecordings"`
	EPG             bool `json:"epg"`
	CanDVR          bool `json:"canDvr"` // Legacy alias for Record

	AllowedBouquets []string `json:"allowedBouquets,omitempty"`
	BlockedChannels []string `json:"blockedChannels,omitempty"`
	ProfileID       string   `json:"profileId,omitempty"`
	ProfileName     string   `json:"profileName,omitempty"`
	IsChildProfile  bool     `json:"isChildProfile"`
}

// CalculateEffectivePermissions computes EffectivePermissions for current time.
func CalculateEffectivePermissions(userID string, role Role, policy *ProfilePolicy, access *AccessPolicy) EffectivePermissions {
	return CalculateEffectivePermissionsAt(userID, role, policy, access, time.Now().UTC())
}

// CalculateEffectivePermissionsAt computes EffectivePermissions at a given timestamp.
// ProfilePolicy and AccessPolicy can ONLY restrict capabilities, NEVER expand account role rights!
func CalculateEffectivePermissionsAt(userID string, role Role, policy *ProfilePolicy, access *AccessPolicy, now time.Time) EffectivePermissions {
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
		if access.RevokedAt != nil || isAccessPolicyTimeRestricted(access, now) {
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
		if policy.ViewingTimeWindow != nil && isProfileTimeRestricted(policy.ViewingTimeWindow, now) {
			perms.LiveTV = false
			perms.Record = false
			perms.WatchRecordings = false
			perms.EPG = false
		}

		perms.AllowedBouquets = policy.AllowedBouquets
		perms.BlockedChannels = policy.BlockedChannels
	}

	// Keep CanDVR synchronized with Record for backward compatibility
	perms.CanDVR = perms.Record

	return perms
}

func isAccessPolicyTimeRestricted(access *AccessPolicy, now time.Time) bool {
	if access.ValidFrom != nil && now.Before(access.ValidFrom.UTC()) {
		return true
	}
	if access.ValidUntil != nil && now.After(access.ValidUntil.UTC()) {
		return true
	}

	loc := time.UTC
	if access.Timezone != "" {
		if l, err := time.LoadLocation(access.Timezone); err == nil {
			loc = l
		}
	}
	nowInTZ := now.In(loc)

	// Check Weekday Mask if configured
	if access.AllowedDaysMask > 0 {
		var dayBit int
		switch nowInTZ.Weekday() {
		case time.Monday:
			dayBit = 1
		case time.Tuesday:
			dayBit = 2
		case time.Wednesday:
			dayBit = 4
		case time.Thursday:
			dayBit = 8
		case time.Friday:
			dayBit = 16
		case time.Saturday:
			dayBit = 32
		case time.Sunday:
			dayBit = 64
		}
		if (access.AllowedDaysMask & dayBit) == 0 {
			return true
		}
	}

	// Check Daily Window if configured
	if access.DailyStart != "" && access.DailyEnd != "" {
		currentHM := nowInTZ.Format("15:04")
		if access.DailyStart <= access.DailyEnd {
			if currentHM < access.DailyStart || currentHM >= access.DailyEnd {
				return true
			}
		} else { // Overnight window (e.g. 22:00 to 06:00)
			if currentHM < access.DailyStart && currentHM >= access.DailyEnd {
				return true
			}
		}
	}

	return false
}

func isProfileTimeRestricted(tw *ViewingTimeWindow, now time.Time) bool {
	if tw == nil || tw.Start == "" || tw.End == "" {
		return false
	}
	currentHM := now.UTC().Format("15:04")
	if tw.Start <= tw.End {
		return currentHM < tw.Start || currentHM >= tw.End
	}
	return currentHM < tw.Start && currentHM >= tw.End
}

// AuditLogEntry is an append-only tamper-evident audit record.
type AuditLogEntry struct {
	ID             int64     `json:"id"`
	ActorUserID    string    `json:"actorUserId"`
	Action         string    `json:"action"`
	TargetResource string    `json:"targetResource"`
	DetailsJSON    string    `json:"detailsJson,omitempty"`
	PrevHash       string    `json:"prevHash"`
	Hash           string    `json:"hash"`
	CreatedAt      time.Time `json:"createdAt"`
}

const (
	ReasonCodeAllowed           = "allowed"
	ReasonCodeOutsideTimeWindow = "outside_time_window"
	ReasonCodeRatingBlocked     = "rating_blocked"
	ReasonCodeApprovalRequired  = "approval_required"
	ReasonCodeDeviceRevoked     = "device_revoked"
	ReasonCodeResourceLimit     = "resource_limit"
	ReasonCodeInternalError     = "internal_error"
)

type PolicyDecision struct {
	Allowed           bool                 `json:"allowed"`
	Capabilities      EffectivePermissions `json:"capabilities"`
	ReasonCode        string               `json:"reasonCode"`
	RequiresApproval  bool                 `json:"requiresApproval"`
	ApprovalRequestID string               `json:"approvalRequestId,omitempty"`
	EffectiveUntil    *time.Time           `json:"effectiveUntil,omitempty"`
	NextRecheckAt     *time.Time           `json:"nextRecheckAt,omitempty"`
	PolicyVersion     int                  `json:"policyVersion"`
}

// EvaluatePolicyDecision performs a centralized, fail-closed policy evaluation.
func EvaluatePolicyDecision(userID string, role Role, policy *ProfilePolicy, access *AccessPolicy, resourcePol *HouseholdResourcePolicy, now time.Time) PolicyDecision {
	if userID == "" {
		return PolicyDecision{
			Allowed:       false,
			ReasonCode:    ReasonCodeInternalError,
			PolicyVersion: 1,
		}
	}

	// 1. Evaluate Effective Capabilities
	caps := CalculateEffectivePermissionsAt(userID, role, policy, access, now)

	// Fail-closed check: If all product capabilities are false due to access restrictions
	if !caps.LiveTV && !caps.Record && !caps.WatchRecordings && !caps.EPG {
		var nextRecheck *time.Time
		if access != nil && access.DailyEnd != "" {
			loc := time.UTC
			if access.Timezone != "" {
				if l, err := time.LoadLocation(access.Timezone); err == nil {
					loc = l
				}
			}
			nowInTZ := now.In(loc)
			if access.DailyStart != "" {
				if nextStart, err := time.ParseInLocation("15:04", access.DailyStart, loc); err == nil {
					t := time.Date(nowInTZ.Year(), nowInTZ.Month(), nowInTZ.Day(), nextStart.Hour(), nextStart.Minute(), 0, 0, loc)
					if t.Before(nowInTZ) {
						t = t.Add(24 * time.Hour)
					}
					utcT := t.UTC()
					nextRecheck = &utcT
				}
			}
		}

		return PolicyDecision{
			Allowed:       false,
			Capabilities:  caps,
			ReasonCode:    ReasonCodeOutsideTimeWindow,
			NextRecheckAt: nextRecheck,
			PolicyVersion: 1,
		}
	}

	// 2. Compute Effective Until boundary
	var effUntil *time.Time
	if access != nil {
		if access.ValidUntil != nil {
			t := access.ValidUntil.UTC()
			effUntil = &t
		}
		if access.DailyEnd != "" {
			loc := time.UTC
			if access.Timezone != "" {
				if l, err := time.LoadLocation(access.Timezone); err == nil {
					loc = l
				}
			}
			nowInTZ := now.In(loc)
			if endToday, err := time.ParseInLocation("15:04", access.DailyEnd, loc); err == nil {
				t := time.Date(nowInTZ.Year(), nowInTZ.Month(), nowInTZ.Day(), endToday.Hour(), endToday.Minute(), 0, 0, loc)
				if t.Before(nowInTZ) {
					t = t.Add(24 * time.Hour)
				}
				utcT := t.UTC()
				if effUntil == nil || utcT.Before(*effUntil) {
					effUntil = &utcT
				}
			}
		}
	}

	// Set NextRecheckAt 30 seconds before EffectiveUntil
	var nextRecheck *time.Time
	if effUntil != nil {
		t := effUntil.Add(-30 * time.Second)
		nextRecheck = &t
	}

	return PolicyDecision{
		Allowed:        true,
		Capabilities:   caps,
		ReasonCode:     ReasonCodeAllowed,
		EffectiveUntil: effUntil,
		NextRecheckAt:  nextRecheck,
		PolicyVersion:  1,
	}
}
