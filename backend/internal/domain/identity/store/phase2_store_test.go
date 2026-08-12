// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhase2Store_AccessPoliciesAndApprovals(t *testing.T) {
	st, err := store.NewSQLiteStore(":memory:")
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// Create user
	user := &identity.User{
		ID:          "usr_cousine",
		Username:    "cousine",
		DisplayName: "Cousine",
		Role:        identity.RoleGuest,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, st.PutUser(ctx, user))

	// 1. Test AccessPolicy
	validUntil := now.Add(48 * time.Hour)
	policy := &identity.AccessPolicy{
		ID:                "pol_cousine",
		AccountID:         user.ID,
		ValidUntil:        &validUntil,
		DailyStart:        "07:00",
		DailyEnd:          "19:00",
		Timezone:          "Europe/Vienna",
		AllowedDaysMask:   127,
		LiveTVAllowed:     true,
		EPGAllowed:        true,
		DVRAllowed:        false,
		RecordingsAllowed: false,
		MaxDevices:        1,
		CreatedAt:         now,
	}
	require.NoError(t, st.PutAccessPolicy(ctx, policy))

	fetchedPol, err := st.GetAccessPolicy(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, fetchedPol)
	assert.Equal(t, "pol_cousine", fetchedPol.ID)
	assert.False(t, fetchedPol.DVRAllowed)
	assert.Equal(t, 1, fetchedPol.MaxDevices)

	// 2. Test Profile with DateOfBirth
	adminUser := &identity.User{
		ID:          "usr_admin",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, st.PutUser(ctx, adminUser))

	dob := time.Date(2018, 5, 14, 0, 0, 0, 0, time.UTC)
	profile := &identity.Profile{
		ID:                  "prof_max",
		HouseholdID:         "default_household",
		Name:                "Max",
		IsChild:             true,
		DateOfBirth:         &dob,
		MaxParentalRating:   12,
		UnknownRatingPolicy: epg.UnknownPolicyRequestApproval,
		StorageQuotaBytes:   10 * 1024 * 1024 * 1024,
		CreatedByUserID:     "usr_admin",
		CreatedAt:           now,
	}
	pol := &identity.ProfilePolicy{
		ProfileID:       profile.ID,
		AllowedBouquets: []string{"Kinder"},
		MaturityLevel:   12,
		DVRAllowed:      true,
	}
	require.NoError(t, st.PutProfile(ctx, profile, pol))

	fetchedProf, fetchedProfilePol, err := st.GetProfile(ctx, profile.ID)
	require.NoError(t, err)
	require.NotNil(t, fetchedProf)
	require.NotNil(t, fetchedProfilePol)
	assert.True(t, fetchedProf.IsChild)
	assert.Equal(t, 12, fetchedProf.MaxParentalRating)
	assert.Equal(t, epg.UnknownPolicyRequestApproval, fetchedProf.UnknownRatingPolicy)
	assert.True(t, fetchedProf.Age() > 0)

	// 3. Test Approval Requests
	apprReq := &identity.ApprovalRequest{
		ID:             "appr_1001",
		HouseholdID:    "default_household",
		ProfileID:      profile.ID,
		RequestType:    "view_content",
		ResourceID:     "movie_spiderman",
		ResourceName:   "Spider-Man",
		ParentalRating: 12,
		Scope:          "single",
		Status:         "pending",
		CreatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	}
	require.NoError(t, st.CreateApprovalRequest(ctx, apprReq))

	listPending, err := st.ListApprovalRequests(ctx, "default_household", "pending")
	require.NoError(t, err)
	require.Len(t, listPending, 1)
	assert.Equal(t, "appr_1001", listPending[0].ID)

	require.NoError(t, st.SettleApprovalRequest(ctx, "appr_1001", "approved", "usr_admin", now))

	fetchedAppr, err := st.GetApprovalRequest(ctx, "appr_1001")
	require.NoError(t, err)
	assert.Equal(t, "approved", fetchedAppr.Status)
	assert.Equal(t, "usr_admin", fetchedAppr.ApprovedByUserID)

	// Settling already settled request should fail
	err = st.SettleApprovalRequest(ctx, "appr_1001", "approved", "usr_admin", now)
	assert.ErrorIs(t, err, identity.ErrApprovalAlreadySettled)

	// 4. Test HouseholdResourcePolicy
	resPol := &identity.HouseholdResourcePolicy{
		HouseholdID:               "default_household",
		MaxConcurrentLiveServices: 4,
		MaxConcurrentViewers:      6,
		MaxParallelRecordings:     5,
		MaxParallelTranscodes:     2,
		PreemptionEnabled:         true,
		UpdatedAt:                 now,
	}
	require.NoError(t, st.PutHouseholdResourcePolicy(ctx, resPol))

	fetchedRes, err := st.GetHouseholdResourcePolicy(ctx, "default_household")
	require.NoError(t, err)
	assert.Equal(t, 4, fetchedRes.MaxConcurrentLiveServices)
	assert.True(t, fetchedRes.PreemptionEnabled)

	// 5. Test Recording Profile Access Join Table
	require.NoError(t, st.PutRecordingProfileAccess(ctx, "rec_999", []string{"prof_max"}))

	accessProfiles, err := st.GetRecordingProfileAccess(ctx, "rec_999")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"prof_max"}, accessProfiles)
}
