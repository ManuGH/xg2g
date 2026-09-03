package identity_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthenticateWithPassword_TimingProtectionAndConcurrencyLimit(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "pwd_sec.sqlite")
	st, err := store.OpenStateStore("sqlite", dbPath)
	require.NoError(t, err)
	defer st.Close()

	svc := identity.NewService(identity.Config{}, st)
	defer svc.Close()

	ctx := context.Background()

	// Create user with known password
	adminUser := &identity.User{
		ID:          "usr_admin",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        identity.RoleAdmin,
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, st.PutUser(ctx, adminUser))
	hash, err := identity.HashPassword("CorrectPassword123!")
	require.NoError(t, err)
	err = st.PutAccountPassword(ctx, adminUser.ID, hash, time.Now().UTC())
	require.NoError(t, err)

	// 1. Timing Protection Test:
	// Verify that unknown user and wrong password both take comparable execution time (both run Argon2id)
	t.Run("TimingProtectionRunsArgon2ForUnknownUser", func(t *testing.T) {
		startUnknown := time.Now()
		_, errUnknown := svc.AuthenticateWithPassword(ctx, "non_existent_user_999", "WrongPassword123!")
		durationUnknown := time.Since(startUnknown)
		assert.ErrorIs(t, errUnknown, identity.ErrInvalidPassword)

		startWrongPass := time.Now()
		_, errWrongPass := svc.AuthenticateWithPassword(ctx, "admin", "WrongPassword123!")
		durationWrongPass := time.Since(startWrongPass)
		assert.ErrorIs(t, errWrongPass, identity.ErrInvalidPassword)

		// Both should take at least 5ms (Argon2id calculation), showing dummy hash was computed
		assert.True(t, durationUnknown > 5*time.Millisecond, "unknown user must run dummy Argon2id verification")
		assert.True(t, durationWrongPass > 5*time.Millisecond, "wrong password must run Argon2id verification")
	})

	// 2. Concurrency Limit Test:
	// Bombard with 20 parallel authentication requests.
	// At most maxConcurrentArgon2 can run concurrently; excess requests should receive ErrAuthBusy or succeed/fail cleanly.
	t.Run("Argon2ConcurrencyLimit", func(t *testing.T) {
		const concurrentRequests = 20
		var wg sync.WaitGroup
		var busyCount atomic.Int32
		var completedCount atomic.Int32

		for i := 0; i < concurrentRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Use a short context to observe load shed
				reqCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
				defer cancel()

				_, err := svc.AuthenticateWithPassword(reqCtx, "admin", "WrongPassword123!")
				if err != nil {
					if err == identity.ErrAuthBusy {
						busyCount.Add(1)
					}
					completedCount.Add(1)
				}
			}()
		}
		wg.Wait()

		assert.Equal(t, int32(concurrentRequests), completedCount.Load(), "all requests must be handled")
		t.Logf("Concurrency test finished: %d total, %d shed as ErrAuthBusy", completedCount.Load(), busyCount.Load())
	})
}
