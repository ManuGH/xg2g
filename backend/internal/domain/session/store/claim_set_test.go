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
			err = st.ReleaseClaimSet(ctx, "session-A", resA.GenerationToken)
			require.NoError(t, err)

			// Verify parent demod is STILL occupied by mux
			demodLease, hasLease, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_a"))
			assert.True(t, hasLease, "Parent demod lease must remain active while Session B is present")
			assert.NotNil(t, demodLease)

			// 4. Session B leaves -> Now 0 members remain, parent demod and mux are freed
			err = st.ReleaseClaimSet(ctx, "session-B", resB.GenerationToken)
			require.NoError(t, err)

			_, hasLeaseAfter, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_a"))
			assert.False(t, hasLeaseAfter, "Parent demod lease must be freed after last session leaves")
		})
	}
}

func TestClaimSet_StrictGenerationFencing(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// 1. Session acquired under Generation 1
			req1 := model.ClaimSetRequest{
				SessionID:     "session-fenced",
				ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
				MultiplexID:   "mux-fenced",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_fenced",
				TTL:           10 * time.Second,
			}
			res1, err := st.TryAcquireClaimSet(ctx, req1)
			require.NoError(t, err)
			require.True(t, res1.Success)
			require.NotEmpty(t, res1.GenerationToken)

			// 2. Attempt release with empty token -> must error
			errEmpty := st.ReleaseClaimSet(ctx, "session-fenced", "")
			assert.Error(t, errEmpty, "Must reject tokenless release")

			// 3. Attempt release with mismatched token -> must NOT delete claims
			errWrong := st.ReleaseClaimSet(ctx, "session-fenced", "wrong-generation-token")
			require.NoError(t, errWrong) // Safe no-op or mismatch

			// Verify claim is still active
			demodLease, hasLease, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_fenced"))
			assert.True(t, hasLease, "Claim must NOT be deleted by mismatched generation token")
			assert.NotNil(t, demodLease)

			// 4. Release with correct token -> succeeds and removes claim
			errCorrect := st.ReleaseClaimSet(ctx, "session-fenced", res1.GenerationToken)
			require.NoError(t, errCorrect)

			_, hasLeaseAfter, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_fenced"))
			assert.False(t, hasLeaseAfter, "Claim must be deleted with valid generation token")
		})
	}
}

func TestClaimSet_ForceAdminRelease(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			req := model.ClaimSetRequest{
				SessionID:     "session-admin-force",
				ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
				MultiplexID:   "mux-admin",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_admin",
				TTL:           10 * time.Second,
			}
			res, err := st.TryAcquireClaimSet(ctx, req)
			require.NoError(t, err)
			require.True(t, res.Success)

			// Admin force release without token
			err = st.ForceAdminReleaseClaimSet(ctx, "session-admin-force")
			require.NoError(t, err)

			_, hasLease, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_admin"))
			assert.False(t, hasLease, "Demod lease must be cleared by admin force release")
		})
	}
}

func TestClaimSet_ApplyReconciliationPlan(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Start two sessions
			req1 := model.ClaimSetRequest{
				SessionID:     "sess-reconcile-1",
				ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
				MultiplexID:   "mux-rec-1",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_rec_1",
				TTL:           10 * time.Second,
			}
			res1, err := st.TryAcquireClaimSet(ctx, req1)
			require.NoError(t, err)
			require.True(t, res1.Success)

			req2 := model.ClaimSetRequest{
				SessionID:     "sess-reconcile-2",
				ServiceRef:    "1:0:1:2:2:2:2:0:0:0:",
				MultiplexID:   "mux-rec-2",
				InputID:       "input_b",
				RequiredPlane: "192:LOW:V",
				DemodID:       "demod_rec_2",
				TTL:           10 * time.Second,
			}
			res2, err := st.TryAcquireClaimSet(ctx, req2)
			require.NoError(t, err)
			require.True(t, res2.Success)

			// Apply reconciliation plan that reaps session 1 and mux 1
			plan := model.ReconciliationPlan{
				SessionsToReap: []string{"sess-reconcile-1"},
				ExpiredMuxes:   []string{"mux-rec-1"},
			}
			err = st.ApplyReconciliationPlan(ctx, plan)
			require.NoError(t, err)

			// Verify session 1 is reaped, but session 2 remains untouched
			_, hasLease1, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_rec_1"))
			assert.False(t, hasLease1, "Demod 1 must be reaped by plan")

			_, hasLease2, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_rec_2"))
			assert.True(t, hasLease2, "Demod 2 must remain active")
		})
	}
}

func TestClaimSet_DemuxSaturationLimit(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Max members configured to 2
			reqA := model.ClaimSetRequest{
				SessionID:     "sess-demux-1",
				ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
				MultiplexID:   "mux-demux-test",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_demux_1",
				MaxMuxMembers: 2,
				TTL:           10 * time.Second,
			}
			resA, err := st.TryAcquireClaimSet(ctx, reqA)
			require.NoError(t, err)
			require.True(t, resA.Success)
			assert.False(t, resA.ReusedMux)

			reqB := model.ClaimSetRequest{
				SessionID:     "sess-demux-2",
				ServiceRef:    "1:0:1:2:1:1:1:0:0:0:",
				MultiplexID:   "mux-demux-test",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_demux_2",
				MaxMuxMembers: 2,
				TTL:           10 * time.Second,
			}
			resB, err := st.TryAcquireClaimSet(ctx, reqB)
			require.NoError(t, err)
			require.True(t, resB.Success)
			assert.True(t, resB.ReusedMux, "Session 2 must reuse multiplex")

			// Third session on same multiplex hits MaxMuxMembers (2) -> cannot reuse demod_demux_1
			// It should allocate a second independent demod (demod_demux_3) on compatible plane
			reqC := model.ClaimSetRequest{
				SessionID:     "sess-demux-3",
				ServiceRef:    "1:0:1:3:1:1:1:0:0:0:",
				MultiplexID:   "mux-demux-test",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_demux_3",
				MaxMuxMembers: 2,
				TTL:           10 * time.Second,
			}
			resC, err := st.TryAcquireClaimSet(ctx, reqC)
			require.NoError(t, err)
			require.True(t, resC.Success)
			assert.False(t, resC.ReusedMux, "Session 3 must NOT reuse saturated mux; must allocate demod_demux_3")
			assert.Equal(t, "demod_demux_3", resC.DemodID)
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

func TestClaimSet_ReAcquireSameSessionGenerationFencing(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// 1. Initial acquire -> Generation 1
			req1 := model.ClaimSetRequest{
				SessionID:     "reacquire-sess",
				ServiceRef:    "1:0:1:1:1:1:1:0:0:0:",
				MultiplexID:   "mux-reacquire",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_reacq",
				TTL:           10 * time.Second,
			}
			res1, err := st.TryAcquireClaimSet(ctx, req1)
			require.NoError(t, err)
			require.True(t, res1.Success)
			gen1 := res1.GenerationToken
			require.NotEmpty(t, gen1)

			// 2. Re-acquire same session (e.g. channel switch / retune) -> Generation 2
			req2 := model.ClaimSetRequest{
				SessionID:     "reacquire-sess",
				ServiceRef:    "1:0:1:2:1:1:1:0:0:0:",
				MultiplexID:   "mux-reacquire",
				InputID:       "input_a",
				RequiredPlane: "192:HIGH:H",
				DemodID:       "demod_reacq",
				TTL:           10 * time.Second,
			}
			res2, err := st.TryAcquireClaimSet(ctx, req2)
			require.NoError(t, err)
			require.True(t, res2.Success)
			gen2 := res2.GenerationToken
			require.NotEmpty(t, gen2)
			assert.NotEqual(t, gen1, gen2, "New acquisition must generate fresh fencing token")

			// 3. Stale teardown handler from Gen 1 fires -> must NOT delete Gen 2 claim
			err = st.ReleaseClaimSet(ctx, "reacquire-sess", gen1)
			require.NoError(t, err)

			demodLease, hasLease, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_reacq"))
			assert.True(t, hasLease, "Claim for Generation 2 must NOT be deleted by Generation 1 release")
			assert.NotNil(t, demodLease)

			// 4. Current teardown handler from Gen 2 fires -> successfully deletes claim
			err = st.ReleaseClaimSet(ctx, "reacquire-sess", gen2)
			require.NoError(t, err)

			_, hasLeaseAfter, _ := st.GetLease(ctx, model.LeaseKeyDemod("demod_reacq"))
			assert.False(t, hasLeaseAfter, "Claim must be deleted when releasing with Generation 2 token")
		})
	}
}

func TestClaimSet_ConcurrentAcquireReleaseReaperRace(t *testing.T) {
	for _, st := range createTestStores(t) {
		name := fmt.Sprintf("%T", st)
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			const workers = 6
			var wg sync.WaitGroup

			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					sessID := fmt.Sprintf("race-sess-%d", workerID)
					for ctx.Err() == nil {
						req := model.ClaimSetRequest{
							SessionID:     sessID,
							ServiceRef:    "1:0:1:100:1:1:1:0:0:0:",
							MultiplexID:   "mux-race",
							InputID:       "input_a",
							RequiredPlane: "192:HIGH:H",
							DemodID:       fmt.Sprintf("demod_race_%d", workerID),
							TTL:           50 * time.Millisecond,
						}
						res, err := st.TryAcquireClaimSet(ctx, req)
						if err == nil && res.Success && res.GenerationToken != "" {
							time.Sleep(10 * time.Millisecond)
							_ = st.ReleaseClaimSet(ctx, sessID, res.GenerationToken)
						}
					}
				}(i)
			}

			// Run Reaper concurrently
			wg.Add(1)
			go func() {
				defer wg.Done()
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						_, _, _ = st.ReapExpiredClaimMembers(ctx)
					}
				}
			}()

			wg.Wait()
		})
	}
}
