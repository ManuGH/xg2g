package v3

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/stretchr/testify/assert"
)

func TestContract_DeleteTimer(t *testing.T) {
	cfg := config.AppConfig{}
	snap := config.Snapshot{}

	t.Run("37_InvalidID_400", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		req := httptest.NewRequest("DELETE", "/api/v3/dvr/timers/invalid-id", nil)
		w := httptest.NewRecorder()

		s.DeleteTimer(w, req, "invalid-id")

		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_id")
	})

	t.Run("38_ReceiverUnreachable_502", func(t *testing.T) {
		mock := &mockOWI{
			deleteTimerFunc: func(ctx context.Context, sRef string, begin, end int64) error {
				return errors.New("connection refused")
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("DELETE", "/api/v3/dvr/timers/"+timerId, nil)
		w := httptest.NewRecorder()

		s.DeleteTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_unreachable")
	})

	t.Run("39_TimerNotFound_404", func(t *testing.T) {
		mock := &mockOWI{
			deleteTimerFunc: func(ctx context.Context, sRef string, begin, end int64) error {
				return errors.New("timer not found")
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("DELETE", "/api/v3/dvr/timers/"+timerId, nil)
		w := httptest.NewRecorder()

		s.DeleteTimer(w, req, timerId)

		assertProblemDetails(t, w.Result(), 404, "dvr/not_found")
	})

	t.Run("40_Success_204", func(t *testing.T) {
		mock := &mockOWI{
			deleteTimerFunc: func(ctx context.Context, sRef string, begin, end int64) error {
				return nil
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		timerId := read.MakeTimerID("1:0:1:C3:21:85:C00000:0:0:0:", 1000, 2000)
		req := httptest.NewRequest("DELETE", "/api/v3/dvr/timers/"+timerId, nil)
		w := httptest.NewRecorder()

		s.DeleteTimer(w, req, timerId)

		assert.Equal(t, 204, w.Result().StatusCode)
	})
}
