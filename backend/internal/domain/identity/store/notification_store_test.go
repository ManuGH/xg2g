// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationStore_CRUDAndSettleGuard(t *testing.T) {
	st, err := store.NewSQLiteStore(":memory:")
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// Setup initial user & profile
	u1 := &identity.User{ID: "usr_admin1", Username: "admin1", Role: identity.RoleAdmin, CreatedAt: now}
	u2 := &identity.User{ID: "usr_admin2", Username: "admin2", Role: identity.RoleAdmin, CreatedAt: now}
	require.NoError(t, st.PutUser(ctx, u1))
	require.NoError(t, st.PutUser(ctx, u2))

	// 1. Create Notifications for Admin 1 and Admin 2
	n1 := &identity.Notification{
		ID:             "notif_001",
		HouseholdID:    "default_household",
		UserID:         "usr_admin1",
		Type:           "approval_request",
		Title:          "Freigabe erforderlich",
		Body:           "Max möchte 'Die Hard' (FSK 16) ansehen",
		ResourceID:     "appr_1001",
		ActionRequired: "approve_content",
		CreatedAt:      now,
	}
	n2 := &identity.Notification{
		ID:             "notif_002",
		HouseholdID:    "default_household",
		UserID:         "usr_admin2",
		Type:           "approval_request",
		Title:          "Freigabe erforderlich",
		Body:           "Max möchte 'Die Hard' (FSK 16) ansehen",
		ResourceID:     "appr_1001",
		ActionRequired: "approve_content",
		CreatedAt:      now,
	}

	require.NoError(t, st.CreateNotification(ctx, n1))
	require.NoError(t, st.CreateNotification(ctx, n2))

	// 2. Query unread for admin1 vs admin2
	listAdmin1, err := st.ListNotifications(ctx, "default_household", "usr_admin1", true, 50)
	require.NoError(t, err)
	require.Len(t, listAdmin1, 1)
	assert.Equal(t, "notif_001", listAdmin1[0].ID)

	listAdmin2, err := st.ListNotifications(ctx, "default_household", "usr_admin2", true, 50)
	require.NoError(t, err)
	require.Len(t, listAdmin2, 1)
	assert.Equal(t, "notif_002", listAdmin2[0].ID)

	// 3. Mark admin1 notification read -> admin2 notification remains unread
	require.NoError(t, st.MarkNotificationRead(ctx, "notif_001", "usr_admin1", now))

	listAdmin1After, _ := st.ListNotifications(ctx, "default_household", "usr_admin1", true, 50)
	assert.Len(t, listAdmin1After, 0)

	listAdmin2After, _ := st.ListNotifications(ctx, "default_household", "usr_admin2", true, 50)
	assert.Len(t, listAdmin2After, 1)

	// 4. Test PushSubscription save and list
	sub := &identity.PushSubscription{
		ID:          "sub_001",
		HouseholdID: "default_household",
		UserID:      "usr_admin1",
		Endpoint:    "https://push.example.com/sub/123",
		P256dh:      "test_p256dh",
		Auth:        "test_auth",
		CreatedAt:   now,
	}
	require.NoError(t, st.SavePushSubscription(ctx, sub))

	subs, err := st.ListPushSubscriptions(ctx, "default_household", "usr_admin1")
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "https://push.example.com/sub/123", subs[0].Endpoint)
}
