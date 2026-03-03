package v3

import (
	"bytes"
	"context"
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
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 404, "dvr/not_found")
	})

	t.Run("44_ReceiverUnreachable_GetTimers_502", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, openwebif.ErrTimeout
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

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
		mock.detectTimerChangeFunc = func(ctx context.Context) (openwebif.TimerChangeCap, error) {
			return openwebif.TimerChangeCap{Supported: true}, nil
		}
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

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
		mock.detectTimerChangeFunc = func(ctx context.Context) (openwebif.TimerChangeCap, error) {
			return openwebif.TimerChangeCap{Supported: true}, nil
		}
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return openwebif.ErrConflict
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

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
		mock.detectTimerChangeFunc = func(ctx context.Context) (openwebif.TimerChangeCap, error) {
			return openwebif.TimerChangeCap{Supported: false}, nil
		} // Triggers fallback
		mock.deleteTimerFunc = func(ctx context.Context, s string, b, e int64) error { return nil }
		mock.addTimerFunc = func(ctx context.Context, s string, b, e int64, n, d string) error { return nil }

		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		body := `{"begin": 1100, "end": 2100}`
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assert.Equal(t, 200, w.Result().StatusCode)
	})

	t.Run("48_UpdateConflict_409", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			return []openwebif.Timer{{ServiceRef: sRef, Begin: 1000, End: 2000}}, nil
		}
		// Handler calls Detect for logging only
		mock.detectTimerChangeFunc = func(ctx context.Context) (openwebif.TimerChangeCap, error) {
			return openwebif.TimerChangeCap{Supported: false}, nil
		}

		// Handler calls UpdateTimer unconditionally. logic is inside client.
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return openwebif.ErrConflict
		}

		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{"begin": 1100}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assert.Equal(t, 409, w.Result().StatusCode)
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
		mock.detectTimerChangeFunc = func(ctx context.Context) (openwebif.TimerChangeCap, error) {
			return openwebif.TimerChangeCap{Supported: true}, nil
		}
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{"name": "test"}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_inconsistent")
	})

	t.Run("51_PartialFailure_502", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			return []openwebif.Timer{{ServiceRef: sRef, Begin: 1000, End: 2000}}, nil
		}
		mock.updateTimerFunc = func(ctx context.Context, oSRef string, oB, oE int64, nSRef string, nB, nE int64, name, desc string, en bool) error {
			// Simulate partial failure sentinel
			return openwebif.ErrTimerUpdatePartial
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID(sRef, 1000, 2000)
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/"+timerId, bytes.NewBufferString(`{"name": "test"}`))
		w := httptest.NewRecorder()

		s.UpdateTimer(w, req, timerId)

		// Assertions: 502 + machine-readable code RECEIVER_INCONSISTENT
		resp := w.Result()
		assert.Equal(t, 502, resp.StatusCode)
		assertProblemDetails(t, resp, 502, "dvr/receiver_inconsistent")
	})
}
