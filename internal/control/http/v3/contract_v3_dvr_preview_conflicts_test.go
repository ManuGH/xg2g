package v3

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
)

func TestContract_PreviewConflicts(t *testing.T) {
	cfg := config.AppConfig{}
	snap := config.Snapshot{}

	t.Run("51_InvalidJSON_400", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers/conflicts/preview", bytes.NewBufferString(`{invalid`))
		w := httptest.NewRecorder()

		s.PreviewConflicts(w, req)

		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_input")
	})

	t.Run("52_InvalidTimeRange_422", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		body := `{"proposed": {"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 2000, "end": 1000, "name": "test"}}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers/conflicts/preview", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.PreviewConflicts(w, req)

		assertProblemDetails(t, w.Result(), 422, "dvr/validation")
	})

	t.Run("53_ReceiverUnreachable_FailClosed_502", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, errors.New("timeout")
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		body := `{"proposed": {"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers/conflicts/preview", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.PreviewConflicts(w, req)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_unreachable")
	})

	t.Run("54_NoConflicts_200_CanScheduleTrue", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, nil // No existing timers
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		body := `{"proposed": {"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers/conflicts/preview", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.PreviewConflicts(w, req)

		assert.Equal(t, 200, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), `"canSchedule":true`)
	})

	t.Run("55_ConflictDetected_200_CanScheduleFalse", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return []openwebif.Timer{
					{ServiceRef: "1:0:1:C3:21:85:C00000:0:0:0:", Begin: 1500, End: 2500, Name: "existing"},
				}, nil
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) openWebIFClient { return mock }}

		body := `{"proposed": {"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers/conflicts/preview", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.PreviewConflicts(w, req)

		assert.Equal(t, 200, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), `"canSchedule":false`)
		assert.Contains(t, w.Body.String(), `"conflicts"`)
	})
}
