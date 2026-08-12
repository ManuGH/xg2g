// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity_test

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/stretchr/testify/assert"
)

func TestCalculateEffectivePermissions_IntersectionRule(t *testing.T) {
	// Scenario 1: Member selects "Papa" (Adult) profile with DVRAllowed=true
	// EffectivePermissions MUST NOT grant Admin rights!
	policyAdult := &identity.ProfilePolicy{
		ProfileID:       "prof_adult",
		DVRAllowed:      true,
		AllowedBouquets: []string{"All"},
	}

	effMemberAdult := identity.CalculateEffectivePermissions("usr_frau", identity.RoleMember, policyAdult, nil)
	assert.False(t, effMemberAdult.IsAdmin, "Member role selecting adult profile must NOT gain Admin rights")
	assert.True(t, effMemberAdult.IsMember)
	assert.True(t, effMemberAdult.LiveTV)
	assert.True(t, effMemberAdult.Record)
	assert.True(t, effMemberAdult.WatchRecordings)
	assert.True(t, effMemberAdult.EPG)
	assert.True(t, effMemberAdult.CanDVR)
	assert.False(t, effMemberAdult.CanManageUser)
	assert.False(t, effMemberAdult.CanConfig)

	// Scenario 2: Guest selects "Adult" profile with DVRAllowed=true
	// EffectivePermissions MUST NOT grant DVR/Record/WatchRecordings rights to Guest!
	effGuestAdult := identity.CalculateEffectivePermissions("usr_cousine", identity.RoleGuest, policyAdult, nil)
	assert.False(t, effGuestAdult.IsAdmin)
	assert.False(t, effGuestAdult.IsMember)
	assert.True(t, effGuestAdult.IsGuest)
	assert.True(t, effGuestAdult.LiveTV)
	assert.True(t, effGuestAdult.EPG)
	assert.False(t, effGuestAdult.Record, "Guest role must NOT have Record capability")
	assert.False(t, effGuestAdult.WatchRecordings, "Guest role must NOT have WatchRecordings capability")
	assert.False(t, effGuestAdult.CanDVR, "Guest role selecting adult profile must NOT gain DVR rights")

	// Scenario 3: Admin selects Child profile with DVRAllowed=false and bouquet filters
	// EffectivePermissions MUST restrict DVR and filter bouquets
	policyChild := &identity.ProfilePolicy{
		ProfileID:       "prof_child",
		AllowedBouquets: []string{"Kinder-TV"},
		MaturityLevel:   6,
		DVRAllowed:      false,
	}

	effAdminChild := identity.CalculateEffectivePermissions("usr_papa", identity.RoleAdmin, policyChild, nil)
	assert.True(t, effAdminChild.IsAdmin)
	assert.False(t, effAdminChild.Record, "Profile DVR restriction must apply even to Admin")
	assert.False(t, effAdminChild.CanDVR)
	assert.Equal(t, []string{"Kinder-TV"}, effAdminChild.AllowedBouquets)
	assert.True(t, effAdminChild.IsChildProfile)

	// Scenario 4: AccessPolicy Restrictions apply (e.g. Member with revoked/restricted AccessPolicy)
	revokedAt := time.Now().UTC()
	accessRestricted := &identity.AccessPolicy{
		AccountID:         "usr_frau",
		LiveTVAllowed:     true,
		EPGAllowed:        true,
		DVRAllowed:        false,
		RecordingsAllowed: false,
		RevokedAt:         &revokedAt,
	}
	effRestricted := identity.CalculateEffectivePermissions("usr_frau", identity.RoleMember, policyAdult, accessRestricted)
	assert.False(t, effRestricted.LiveTV, "Revoked access policy must revoke all capabilities")
	assert.False(t, effRestricted.Record)
	assert.False(t, effRestricted.WatchRecordings)
	assert.False(t, effRestricted.EPG)
}
