// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity_test

import (
	"testing"

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

	effMemberAdult := identity.CalculateEffectivePermissions("usr_frau", identity.RoleMember, policyAdult)
	assert.False(t, effMemberAdult.IsAdmin, "Member role selecting adult profile must NOT gain Admin rights")
	assert.True(t, effMemberAdult.IsMember)
	assert.True(t, effMemberAdult.CanDVR)
	assert.False(t, effMemberAdult.CanManageUser)
	assert.False(t, effMemberAdult.CanConfig)

	// Scenario 2: Guest selects "Adult" profile with DVRAllowed=true
	// EffectivePermissions MUST NOT grant DVR rights to Guest!
	effGuestAdult := identity.CalculateEffectivePermissions("usr_cousine", identity.RoleGuest, policyAdult)
	assert.False(t, effGuestAdult.IsAdmin)
	assert.False(t, effGuestAdult.IsMember)
	assert.True(t, effGuestAdult.IsGuest)
	assert.False(t, effGuestAdult.CanDVR, "Guest role selecting adult profile must NOT gain DVR rights")

	// Scenario 3: Admin selects Child profile with DVRAllowed=false and bouquet filters
	// EffectivePermissions MUST restrict DVR and filter bouquets
	policyChild := &identity.ProfilePolicy{
		ProfileID:       "prof_child",
		AllowedBouquets: []string{"Kinder-TV"},
		MaturityLevel:   6,
		DVRAllowed:      false,
	}

	effAdminChild := identity.CalculateEffectivePermissions("usr_papa", identity.RoleAdmin, policyChild)
	assert.True(t, effAdminChild.IsAdmin)
	assert.False(t, effAdminChild.CanDVR, "Profile DVR restriction must apply even to Admin")
	assert.Equal(t, []string{"Kinder-TV"}, effAdminChild.AllowedBouquets)
	assert.True(t, effAdminChild.IsChildProfile)
}
