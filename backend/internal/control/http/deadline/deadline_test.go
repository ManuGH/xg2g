// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package deadline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRegistrationKey(t *testing.T) {
	key, err := NormalizeRegistrationKey("v3", "get", "/sessions/{sessionID}/events", "/api/v3", 0)
	require.NoError(t, err)
	assert.Equal(t, RegistrationKey{
		RouterID: "v3",
		Method:   "GET",
		Pattern:  "/api/v3/sessions/{sessionID}/events",
		Ordinal:  0,
	}, key)

	for _, tc := range []struct {
		name      string
		routerID  string
		method    string
		pattern   string
		ordinal   int
		errorText string
	}{
		{name: "router", routerID: "other", method: "GET", pattern: "/", errorText: "invalid router ID"},
		{name: "method", routerID: "outer", method: "INVALID", pattern: "/", errorText: "invalid HTTP method"},
		{name: "pattern", routerID: "outer", method: "GET", pattern: "", errorText: "invalid route pattern"},
		{name: "ordinal", routerID: "outer", method: "GET", pattern: "/", ordinal: -1, errorText: "non-negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeRegistrationKey(tc.routerID, tc.method, tc.pattern, "", tc.ordinal)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errorText)
		})
	}
}

func TestPolicyBindingRegistryReservationOrdinalsAndSnapshotIsolation(t *testing.T) {
	registry := NewPolicyBindingRegistry()
	apiPolicy := RoutePolicy{Class: RouteDeadlineAPIBounded}

	reservation, err := registry.ReserveBinding("outer", "GET", "/healthz", "", apiPolicy)
	require.NoError(t, err)
	assert.Equal(t, 0, reservation.Key().Ordinal)
	assert.Equal(t, 0, registry.Snapshot().Len(), "reservation must not be visible before commit")
	reservation.Cancel()

	first, err := registry.RecordBinding("outer", "GET", "/healthz", "", apiPolicy)
	require.NoError(t, err)
	second, err := registry.RecordBinding("outer", "GET", "/healthz", "", apiPolicy)
	require.NoError(t, err)
	assert.Equal(t, 0, first.Ordinal)
	assert.Equal(t, 1, second.Ordinal)

	snapshot := registry.Snapshot()
	_, err = registry.RecordBinding("v3", "POST", "/auth/session", "/api/v3", apiPolicy)
	require.NoError(t, err)
	assert.Equal(t, 2, snapshot.Len())
	assert.Equal(t, 3, registry.Snapshot().Len())

	_, err = registry.RecordBinding("outer", "GET", "/healthz", "", RoutePolicy{Class: RouteDeadlineMediaBounded})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting policy")
}

func TestDeadlineTimeoutsValidation(t *testing.T) {
	require.NoError(t, DefaultTimeouts().Validate())
	require.Error(t, (DeadlineTimeouts{}).Validate())
	require.Error(t, (DeadlineTimeouts{
		APIWriteTimeout:      10 * time.Second,
		MediaWriteTimeout:    5 * time.Second,
		StreamingIdleTimeout: time.Second,
	}).Validate())
}
