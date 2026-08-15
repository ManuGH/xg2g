package v3

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	identitystore "github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

// withIdentityDeviceEnrollment gives a test server the identity substrate the
// pairing exchange now depends on.
//
// Backed by real SQLite rather than a fake: the exchange's guarantees — one
// device identity per thumbprint, a rotating refresh family, a DPoP-bound
// access token — are properties of the store and its constraints, and a stub
// would assert them away instead of proving them.
//
// `username` must match the principal the approving request authenticates as,
// because the pairing records that username as its owner and the exchange
// resolves it against identity.users. That mapping is exactly what the fixture
// needs to make real.
func withIdentityDeviceEnrollment(t *testing.T, srv *Server, username string) *identity.Service {
	t.Helper()

	store, err := identitystore.OpenSQLite(filepath.Join(t.TempDir(), "identity.sqlite"), sqlite.DefaultConfig())
	require.NoError(t, err, "open identity store")
	t.Cleanup(func() { _ = store.Close() })

	svc := identity.NewService(identity.Config{}, store)
	require.NoError(t, store.PutUser(context.Background(), &identity.User{
		ID:       "usr_" + username,
		Username: username,
		Role:     identity.RoleAdmin,
	}), "seed identity user")

	srv.SetIdentityService(svc)
	return svc
}

// identityDeviceCount reports how many device identities exist, so a test can
// assert that a retry re-used one rather than minting a second.
func identityDeviceCount(t *testing.T, svc *identity.Service, thumbprint string) int {
	t.Helper()

	device, err := svc.Store().GetDeviceByThumbprint(context.Background(), thumbprint)
	if err != nil || device == nil {
		return 0
	}
	return 1
}

// assertDeviceAuthHoldsNoDurableState is the ADR-032 ownership boundary as an
// assertion: after a successful exchange the pairing store must hold only the
// consumed bootstrap, never a device, grant or session.
func assertDeviceAuthHoldsNoDurableState(t *testing.T, srv *Server, deviceID string) {
	t.Helper()

	store := srv.deviceAuthStore()
	if store == nil {
		return
	}

	ctx := context.Background()
	device, err := store.GetDevice(ctx, deviceID)
	require.True(t, err != nil || device == nil,
		"deviceauth must not hold a device after the exchange; identity owns it")

	grant, err := store.GetActiveDeviceGrantByDevice(ctx, deviceID)
	require.True(t, err != nil || grant == nil,
		"deviceauth must not hold a device grant after the exchange")

	sessions, err := store.ListAccessSessionsByDevice(ctx, deviceID)
	require.True(t, err != nil || len(sessions) == 0,
		"deviceauth must not hold an access session after the exchange")
}
