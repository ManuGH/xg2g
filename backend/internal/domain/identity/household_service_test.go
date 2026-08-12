// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHouseholdService_InvitesPasswordsAndProfiles(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "household_svc.sqlite")

	st, err := store.OpenStateStore("sqlite", dbPath)
	require.NoError(t, err)

	svc := identity.NewService(identity.Config{
		RPID:           "localhost",
		RPName:         "xg2g",
		ExpectedOrigin: "https://localhost",
		SessionTTL:     24 * time.Hour,
	}, st)

	ctx := context.Background()

	// 1. Initial Admin User Setup
	adminUser := &identity.User{
		ID:          "usr_papa",
		Username:    "admin",
		DisplayName: "Admin Papa",
		Role:        identity.RoleAdmin,
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, st.PutUser(ctx, adminUser))
	require.NoError(t, st.PutHouseholdMembership(ctx, &identity.HouseholdMembership{
		HouseholdID: "default_household",
		UserID:      adminUser.ID,
		Role:        identity.RoleAdmin,
		CreatedAt:   time.Now().UTC(),
	}))
	adminID := adminUser.ID

	// 2. Admin creates invitation for Guest (Cousine)
	inviteCode, inv, err := svc.CreateInvitation(ctx, adminID, identity.RoleGuest, "Cousine Tina")
	require.NoError(t, err)
	assert.NotEmpty(t, inviteCode)
	assert.Equal(t, identity.RoleGuest, inv.Role)

	// 3. Guest redeems invitation with username & password
	redeemRes, err := svc.RedeemInvitationWithPassword(ctx, inviteCode, "tina", "Cousine Tina", "guestPassword123!")
	require.NoError(t, err)
	assert.Equal(t, "tina", redeemRes.User.Username)
	assert.Equal(t, identity.RoleGuest, redeemRes.User.Role)
	assert.NotEmpty(t, redeemRes.SessionID)

	// 4. Repeat redeem fails atomically with ErrInvitationAlreadyUsed
	_, err = svc.RedeemInvitationWithPassword(ctx, inviteCode, "tina2", "Tina 2", "guestPassword123!")
	assert.ErrorIs(t, err, identity.ErrInvitationAlreadyUsed)

	// 5. Password Authentication
	authRes, err := svc.AuthenticateWithPassword(ctx, "tina", "guestPassword123!")
	require.NoError(t, err)
	assert.Equal(t, "tina", authRes.User.Username)
	assert.Equal(t, redeemRes.User.Username, authRes.User.Username)

	_, err = svc.AuthenticateWithPassword(ctx, "tina", "wrongPassword!")
	assert.ErrorIs(t, err, identity.ErrInvalidPassword)

	// 6. Profile Creation & EffectivePermissions Calculation
	prof, pol, err := svc.CreateProfile(ctx, adminID, "Max 👦", "", true, []string{"Kinder-TV"}, nil, 6, "1234")
	require.NoError(t, err)
	assert.Equal(t, "Max 👦", prof.Name)
	assert.False(t, pol.DVRAllowed)

	// Admin + Child Profile -> Restricted Admin (DVR restricted by profile)
	effAdminChild, err := svc.GetEffectivePermissions(ctx, adminID, prof.ID)
	require.NoError(t, err)
	assert.True(t, effAdminChild.IsAdmin)
	assert.False(t, effAdminChild.CanDVR, "Profile restriction must apply to Admin")
	assert.Equal(t, []string{"Kinder-TV"}, effAdminChild.AllowedBouquets)

	// Guest + Child Profile -> Guest permissions
	effGuestChild, err := svc.GetEffectivePermissions(ctx, redeemRes.User.ID, prof.ID)
	require.NoError(t, err)
	assert.True(t, effGuestChild.IsGuest)
	assert.False(t, effGuestChild.IsAdmin)
	assert.False(t, effGuestChild.CanDVR)
}
