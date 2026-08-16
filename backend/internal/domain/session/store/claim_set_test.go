package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestStores(t *testing.T) []store.LeaseStore {
	t.Helper()
	memStore := store.NewMemoryStore()

	dbPath := filepath.Join(t.TempDir(), "claim_test.db")
	sqlStore, err := store.NewSqliteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlStore.Close() })

	return []store.LeaseStore{memStore, sqlStore}
}

func TestClaimSet_ExclusiveDemodConcurrency(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const workers = 10
			var wg sync.WaitGroup
			var successCount int32
			var conflictCount int32

			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					req := model.ClaimSetRequest{
						SessionID:     fmt.Sprintf("session-%d", idx),
						ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
						MultiplexID:   fmt.Sprintf("mux-%d", idx),
						InputID:       "input_a",
						RequiredPlane: "192:HIGH:H",
						DemodID:       "demod_shared_target", // Contested single demod
						TTL:           5 * time.Second,
					}
					res, err := st.TryAcquireClaimSet(ctx, req)
					if err == nil && res.Success {
						atomic.AddInt32(&successCount, 1)
					} else if err == nil && res.ConflictType == model.ConflictDemodOccupied {
						atomic.AddInt32(&conflictCount, 1)
					}
				}(i)
			}
			wg.Wait()

			assert.Equal(t, int32(1), successCount, "Exactly 1 worker must acquire exclusive demod")
			assert.Equal(t, int32(workers-1), conflictCount, "Remaining workers must receive ConflictDemodOccupied")
		})
	}
}

func TestClaimSet_CompatibleSharedInputPlane(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const workers = 5
			var wg sync.WaitGroup
			var successCount int32

			// 5 workers request different demods, but the SAME input and SAME RFPlane (192:HIGH:H)
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					req := model.ClaimSetRequest{
						SessionID:     fmt.Sprintf("sess-plane-%d", idx),
						ServiceRef:    fmt.Sprintf("1:0:1:%d:1:1:1:0:0:0:", idx),
						MultiplexID:   fmt.Sprintf("mux-plane-%d", idx),
						InputID:       "input_fbc_a",
						RequiredPlane: "192:HIGH:H",
						DemodID:       fmt.Sprintf("demod_%d", idx), // Independent demods
						TTL:           5 * time.Second,
					}
					res, err := st.TryAcquireClaimSet(ctx, req)
					if err == nil && res.Success {
						atomic.AddInt32(&successCount, 1)
					}
				}(i)
			}
			wg.Wait()

			assert.Equal(t, int32(workers), successCount, "All 5 workers must succeed sharing the same compatible RFPlane on input_fbc_a")
		})
	}
}

func TestClaimSet_PlaneConflictOnSameInput(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// 1. First session acquires input_a on 192:HIGH:H
			req1 := model.ClaimSetRequest{
				SessionID:     "session-high-h",
				ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
				MultiplexID:   "mux-1",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_1",
				TTL:           5 * time.Second,
			}
			res1, err := st.TryAcquireClaimSet(ctx, req1)
			require.NoError(t, err)
			require.True(t, res1.Success)

			// 2. Second session attempts to use input_a on conflicting plane 192:LOW:V
			req2 := model.ClaimSetRequest{
				SessionID:     "session-low-v",
				ServiceRef:    "1:0:1:2:2:2:2:0:0:0:",
				MultiplexID:   "mux-2",
				InputID:       "input_a",
				RequiredPlane: "192:LOW:V",
				DemodID:       "demod_2",
				TTL:           5 * time.Second,
			}
			res2, err := st.TryAcquireClaimSet(ctx, req2)
			require.NoError(t, err)
			assert.False(t, res2.Success, "Must reject conflicting RFPlane on same legacy input")
			assert.Equal(t, model.ConflictPlaneConflict, res2.ConflictType)
		})
	}
}

func TestClaimSet_MultiplexReuseAndRefCountLifecycle(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// 1. Session A starts on Multiplex M1 -> allocates hardware
			reqA := model.ClaimSetRequest{
				SessionID:     "session-A",
				ServiceRef:    "1:0:1:100:1:1:1:0:0:0:",
				MultiplexID:   "mux-M1",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_a",
				TTL:           10 * time.Second,
			}
			resA, err := st.TryAcquireClaimSet(ctx, reqA)
			require.NoError(t, err)
			require.True(t, resA.Success)
			assert.False(t, resA.ReusedMux)

			// 2. Session B starts on same Multiplex M1 -> reuses transport stream
			reqB := model.ClaimSetRequest{
				SessionID:     "session-B",
				ServiceRef:    "1:0:1:101:1:1:1:0:0:0:", // Different service on same mux
				MultiplexID:   "mux-M1",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_b", // Request proposes demod_b, but will reuse demod_a from mux
				TTL:           10 * time.Second,
			}
			resB, err := st.TryAcquireClaimSet(ctx, reqB)
			require.NoError(t, err)
			require.True(t, resB.Success)
			assert.True(t, resB.ReusedMux, "Session B must reuse active multiplex")
			assert.Equal(t, "demod_a", resB.DemodID, "Session B must bind to M1's parent demod_a")

			// 3. Session A leaves -> Mux M1 remains active because Session B is still a member
			err = st.ReleaseClaimSet(ctx, "session-A")
			require.NoError(t, err)

			// Verify parent demod is STILL occupied by mux
			demodLease, hasLease, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_a"))
			assert.True(t, hasLease, "Parent demod lease must remain active while Session B is present")
			assert.NotNil(t, demodLease)

			// 4. Session B leaves -> Now 0 members remain, parent demod and mux are freed
			err = st.ReleaseClaimSet(ctx, "session-B")
			require.NoError(t, err)

			_, hasLeaseAfter, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_a"))
			assert.False(t, hasLeaseAfter, "Parent demod lease must be freed after last session leaves")
		})
	}
}

func TestClaimSet_ReaperCleansOrphanedMuxAndHardware(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Session starts with short TTL (50ms)
			req := model.ClaimSetRequest{
				SessionID:     "session-crashed",
				ServiceRef:    "1:0:1:500:1:1:1:0:0:0:",
				MultiplexID:   "mux-crashed",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_crashed",
				TTL:           50 * time.Millisecond,
			}
			res, err := st.TryAcquireClaimSet(ctx, req)
			require.NoError(t, err)
			require.True(t, res.Success)

			// Wait for TTL expiry
			time.Sleep(80 * time.Millisecond)

			// Run Reaper
			reapedMembers, reapedMuxes, err := st.ReapExpiredClaimMembers(ctx)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, reapedMembers, 1)
			assert.GreaterOrEqual(t, reapedMuxes, 1)

			// Verify parent demod lease is now freed
			_, hasLease, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_crashed"))
			assert.False(t, hasLease, "Demod lease must be reclaimed by reaper")
		})
	}
}
