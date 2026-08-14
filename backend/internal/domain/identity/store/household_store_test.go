// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store_test

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

func TestHouseholdStore_FullLifecycle(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "household_store.sqlite")

	s, err := store.OpenStateStore("sqlite", dbPath)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Verify default_household exists from migration
	h, err := s.GetHousehold(ctx, "default_household")
	require.NoError(t, err)
	assert.Equal(t, "Haupt-Haushalt", h.Name)

	// 2. Put User & Membership
	adminUser := identity.User{
		ID:          "usr_papa",
		Username:    "papa",
		DisplayName: "Papa Admin",
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
	}
	require.NoError(t, s.PutUser(ctx, &adminUser))

	mem := &identity.HouseholdMembership{
		HouseholdID: "default_household",
		UserID:      adminUser.ID,
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
	}
	require.NoError(t, s.PutHouseholdMembership(ctx, mem))

	mFetch, err := s.GetHouseholdMembership(ctx, "default_household", adminUser.ID)
	require.NoError(t, err)
	assert.Equal(t, identity.RoleAdmin, mFetch.Role)

	// 3. Password Hashing Storage
	require.NoError(t, s.PutAccountPassword(ctx, adminUser.ID, "$argon2id$v=19$m=19456,t=2,p=1$dummyhash", now))
	hashFetch, err := s.GetAccountPasswordHash(ctx, adminUser.ID)
	require.NoError(t, err)
	assert.Equal(t, "$argon2id$v=19$m=19456,t=2,p=1$dummyhash", hashFetch)

	// 4. Invitation Lifecycle & Atomic Redeem
	inv := &identity.AccountInvitation{
		ID:              "inv_cousine_123",
		HouseholdID:     "default_household",
		CodeHash:        "hash_secret_invite_code",
		Role:            identity.RoleGuest,
		DisplayName:     "Cousine Tina",
		CreatedByUserID: adminUser.ID,
		ExpiresAt:       now.Add(24 * time.Hour),
	}
	require.NoError(t, s.CreateInvitation(ctx, inv))

	invFetch, err := s.GetInvitationByCodeHash(ctx, "hash_secret_invite_code")
	require.NoError(t, err)
	assert.Equal(t, "inv_cousine_123", invFetch.ID)

	// Redeem atomically
	guestUser := &identity.User{
		ID:          "usr_cousine",
		Username:    "tina",
		DisplayName: "Cousine Tina",
		Role:        identity.RoleGuest,
		CreatedAt:   now,
	}
	guestMem, err := s.RedeemInvitationAtomic(ctx, "inv_cousine_123", "hash_secret_invite_code", guestUser, "$argon2id$v=19$m=19456,t=2,p=1$tinahash", nil, now)
	require.NoError(t, err)
	assert.Equal(t, identity.RoleGuest, guestMem.Role)

	// Verify repeat redeem fails atomically
	_, err = s.RedeemInvitationAtomic(ctx, "inv_cousine_123", "hash_secret_invite_code", guestUser, "$argon2id$v=19$m=19456,t=2,p=1$tinahash", nil, now)
	assert.ErrorIs(t, err, identity.ErrInvitationAlreadyUsed)

	// 5. Profiles & ProfilePolicies
	childProf := &identity.Profile{
		ID:              "prof_max_child",
		HouseholdID:     "default_household",
		Name:            "Max 👦",
		IsChild:         true,
		CreatedByUserID: adminUser.ID,
		CreatedAt:       now,
	}
	childPol := &identity.ProfilePolicy{
		ProfileID:       childProf.ID,
		AllowedBouquets: []string{"Kinder-TV"},
		BlockedChannels: []string{"channel_paytv_1"},
		MaturityLevel:   6,
		ExitPINHash:     "pin_hash_1234",
		DVRAllowed:      false,
		ViewingTimeWindow: &identity.ViewingTimeWindow{
			Start: "08:00",
			End:   "20:00",
		},
	}
	require.NoError(t, s.PutProfile(ctx, childProf, childPol))

	pFetch, polFetch, err := s.GetProfile(ctx, childProf.ID)
	require.NoError(t, err)
	assert.Equal(t, "Max 👦", pFetch.Name)
	assert.True(t, pFetch.IsChild)
	require.NotNil(t, polFetch)
	assert.Equal(t, []string{"Kinder-TV"}, polFetch.AllowedBouquets)
	assert.False(t, polFetch.DVRAllowed)
	assert.Equal(t, "08:00", polFetch.ViewingTimeWindow.Start)

	profs, err := s.ListProfilesByHousehold(ctx, "default_household")
	require.NoError(t, err)
	require.Len(t, profs, 1)
	assert.Equal(t, "prof_max_child", profs[0].ID)

	require.NoError(t, s.DeleteProfile(ctx, childProf.ID))
	_, _, err = s.GetProfile(ctx, childProf.ID)
	assert.ErrorIs(t, err, identity.ErrProfileNotFound)
}
