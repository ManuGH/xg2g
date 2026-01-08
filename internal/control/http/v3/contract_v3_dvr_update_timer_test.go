package v3

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
)

func TestContract_UpdateTimer(t *testing.T) {
	cfg := config.AppConfig{}
	snap := config.Snapshot{}

	t.Run("41_InvalidID_400", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/invalid-id", bytes.NewBufferString(`{}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, "invalid-id")

		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_id")
	})

	t.Run("42_InvalidInput_400", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{invalid`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_input")
	})

	t.Run("43_TimerNotFound_404", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, nil // Empty list
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 404, "dvr/not_found")
	})

	t.Run("44_ReceiverUnreachable_GetTimers_502", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, errors.New("timeout")
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_unreachable")
	})

	t.Run("45_NativeUpdateSuccess_200", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			// Both first and second calls return the "updated" timer for simplicity in this path
			return []openwebif.Timer{
				{ServiceRef: sRef, Begin: 1100, End: 2100, Name: "new name", ServiceName: "Test CH"},
			}, nil
		}
		mock.hasTimerChangeFunc = func(ctx context.Context) bool { return true }
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID(sRef, 1100, 2100) // Initial match
		body := `{"name": "new name"}`
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assert.Equal(t, 200, w.Result().StatusCode)
		assert.Contains(t, w.Result().Header.Get("Content-Type"), "application/json")
	})

	t.Run("46_NativeUpdateConflict_409", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			return []openwebif.Timer{
				{ServiceRef: sRef, Begin: 1000, End: 2000, Name: "test"},
			}, nil
		}
		mock.hasTimerChangeFunc = func(ctx context.Context) bool { return true }
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return errors.New("Conflict with event X")
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{"begin": 1100}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 409, "dvr/update_failed")
	})

	t.Run("47_FallbackSuccess_200", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			if mock.getTimersCount == 1 {
				// Pre-check
				return []openwebif.Timer{{ServiceRef: sRef, Begin: 1000, End: 2000}}, nil
			}
			// Verification (Post-add)
			return []openwebif.Timer{{ServiceRef: sRef, Begin: 1100, End: 2100}}, nil
		}
		mock.hasTimerChangeFunc = func(ctx context.Context) bool { return false } // Triggers fallback
		mock.deleteTimerFunc = func(ctx context.Context, s string, b, e int64) error { return nil }
		mock.addTimerFunc = func(ctx context.Context, s string, b, e int64, n, d string) error { return nil }

		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		body := `{"begin": 1100, "end": 2100}`
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assert.Equal(t, 200, w.Result().StatusCode)
	})

	t.Run("48_FallbackAddConflict_Rollback_409", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		var rolledBack bool
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			return []openwebif.Timer{{ServiceRef: sRef, Begin: 1000, End: 2000}}, nil
		}
		mock.hasTimerChangeFunc = func(ctx context.Context) bool { return false }
		mock.deleteTimerFunc = func(ctx context.Context, s string, b, e int64) error { return nil }
		mock.addTimerFunc = func(ctx context.Context, s string, b, e int64, n, d string) error {
			if b == 1100 {
				return errors.New("Conflict")
			}
			if b == 1000 {
				rolledBack = true
			}
			return nil
		}

		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{"begin": 1100}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assert.Equal(t, 409, w.Result().StatusCode)
		assert.True(t, rolledBack, "Should have attempted rollback")
	})

	t.Run("50_VerificationFails_502", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			if mock.getTimersCount == 1 {
				return []openwebif.Timer{{ServiceRef: sRef, Begin: 1000, End: 2000}}, nil
			}
			return nil, nil // verification fails
		}
		mock.hasTimerChangeFunc = func(ctx context.Context) bool { return true }
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{"name": "test"}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_inconsistent")
	})
}
