package manager

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/rs/zerolog"
	sqlite3 "modernc.org/sqlite/lib"
)

type codedStoreError struct {
	code int
}

func (e codedStoreError) Error() string { return "sqlite contention" }
func (e codedStoreError) Code() int     { return e.code }

type flakyHeartbeatStore struct {
	sessionstore.StateStore
	failuresLeft int32
	err          error
	attempts     int32
}

func (s *flakyHeartbeatStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	atomic.AddInt32(&s.attempts, 1)
	if atomic.AddInt32(&s.failuresLeft, -1) >= 0 {
		return nil, s.err
	}
	return s.StateStore.UpdateSession(ctx, id, fn)
}

func TestUpdateLatestSegmentHeartbeat_RetriesBusySnapshot(t *testing.T) {
	ctx := context.Background()
	mem := sessionstore.NewMemoryStore()
	if err := mem.PutSession(ctx, &model.SessionRecord{
		SessionID: "sess-retry",
		State:     model.SessionReady,
	}); err != nil {
		t.Fatal(err)
	}

	flaky := &flakyHeartbeatStore{
		StateStore:   mem,
		failuresLeft: 1,
		err:          codedStoreError{code: sqlite3.SQLITE_BUSY_SNAPSHOT},
	}
	orch := &Orchestrator{Store: flaky}
	latest := time.Now().UTC().Round(time.Second)

	if err := orch.updateLatestSegmentHeartbeat(ctx, "sess-retry", latest, zerolog.Nop()); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}

	if got := atomic.LoadInt32(&flaky.attempts); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}

	rec, err := mem.GetSession(ctx, "sess-retry")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || !rec.LatestSegmentAt.Equal(latest) {
		t.Fatalf("expected latest segment time %v, got %#v", latest, rec)
	}
}

func TestUpdateLatestSegmentHeartbeat_DoesNotRetryNonSQLiteErrors(t *testing.T) {
	ctx := context.Background()
	mem := sessionstore.NewMemoryStore()
	if err := mem.PutSession(ctx, &model.SessionRecord{
		SessionID: "sess-no-retry",
		State:     model.SessionReady,
	}); err != nil {
		t.Fatal(err)
	}

	flaky := &flakyHeartbeatStore{
		StateStore:   mem,
		failuresLeft: 1,
		err:          errors.New("boom"),
	}
	orch := &Orchestrator{Store: flaky}

	err := orch.updateLatestSegmentHeartbeat(ctx, "sess-no-retry", time.Now().UTC(), zerolog.Nop())
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected original error, got %v", err)
	}
	if got := atomic.LoadInt32(&flaky.attempts); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
}
