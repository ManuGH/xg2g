package v3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
)

type mockOWI struct {
	getTimersFunc         func(ctx context.Context) ([]openwebif.Timer, error)
	addTimerFunc          func(ctx context.Context, sRef string, begin, end int64, name, desc string) error
	deleteTimerFunc       func(ctx context.Context, sRef string, begin, end int64) error
	updateTimerFunc       func(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error
	detectTimerChangeFunc func(ctx context.Context) (openwebif.TimerChangeCap, error)
	getTimersCount        int
}

func (m *mockOWI) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	m.getTimersCount++
	if m.getTimersFunc == nil {
		return nil, nil
	}
	return m.getTimersFunc(ctx)
}
func (m *mockOWI) AddTimer(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
	if m.addTimerFunc == nil {
		return nil
	}
	return m.addTimerFunc(ctx, sRef, begin, end, name, desc)
}
func (m *mockOWI) DeleteTimer(ctx context.Context, sRef string, begin, end int64) error {
	if m.deleteTimerFunc == nil {
		return nil
	}
	return m.deleteTimerFunc(ctx, sRef, begin, end)
}
func (m *mockOWI) UpdateTimer(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error {
	if m.updateTimerFunc == nil {
		return nil
	}
	return m.updateTimerFunc(ctx, oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, name, description, enabled)
}
func (m *mockOWI) DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error) {
	if m.detectTimerChangeFunc == nil {
		return openwebif.TimerChangeCap{}, nil
	}
	return m.detectTimerChangeFunc(ctx)
}

func TestContract_AddTimer(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.AppConfig{DataDir: tempDir}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "missing.m3u"}}

	t.Run("23_InvalidJSON_400", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(`{invalid json`))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_input")
	})

	t.Run("24_InvalidTime_422", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap}
		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 2000, "end": 1000, "name": "test"}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 422, "dvr/invalid_time")
	})

	t.Run("25_ReceiverUnreachable_DuplicateCheck_502", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, openwebif.ErrUpstreamUnavailable
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_unreachable")
	})

	t.Run("26_Duplicate_409", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return []openwebif.Timer{
					{ServiceRef: "1:0:1:C3:21:85:C00000:0:0:0:", Begin: 1000, End: 2000},
				}, nil
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 409, "dvr/duplicate")
	})

	t.Run("27_AddConflict_409", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, nil // No duplicates
			},
			addTimerFunc: func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
				return openwebif.ErrConflict
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 409, "dvr/add_failed")
	})

	t.Run("28_AddFails_Generic_500", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) { return nil, nil },
			addTimerFunc: func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
				return errors.New("internal server error")
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 500, "dvr/add_failed")
	})

	t.Run("29_VerificationFails_502", func(t *testing.T) {
		mock := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				return nil, nil // Never finds the created timer during read-back
			},
			addTimerFunc: func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
				return nil
			},
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test"}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assertProblemDetails(t, w.Result(), 502, "dvr/receiver_inconsistent")
	})

	t.Run("30_Success_201_ReturnsTimer", func(t *testing.T) {
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			if mock.getTimersCount == 1 {
				// First call (duplicate check): return empty
				return nil, nil
			}
			// Second call (verification): return the timer
			return []openwebif.Timer{
				{ServiceRef: sRef, Begin: 1000, End: 2000, Name: "test", ServiceName: "Test CH"},
			}, nil
		}
		mock.addTimerFunc = func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := fmt.Sprintf(`{"serviceRef": "%s", "begin": 1000, "end": 2000, "name": "test"}`, sRef)
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		resp := w.Result()
		assert.Equal(t, 201, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	})

	t.Run("31_PaddingApplied_BeginEndShifted", func(t *testing.T) {
		targetBegin := int64(1000 - 60)
		targetEnd := int64(2000 + 120)
		sRef := "1:0:1:C3:21:85:C00000:0:0:0:"

		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			if mock.getTimersCount == 1 {
				return nil, nil
			}
			// Success check returns the SHIFTED times
			return []openwebif.Timer{
				{ServiceRef: sRef, Begin: targetBegin, End: targetEnd, Name: "test"},
			}, nil
		}
		mock.addTimerFunc = func(ctx context.Context, ref string, b, e int64, name, desc string) error {
			if b != targetBegin || e != targetEnd {
				return fmt.Errorf("wrong times: got %d-%d, want %d-%d", b, e, targetBegin, targetEnd)
			}
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := `{"serviceRef": "1:0:1:C3:21:85:C00000:0:0:0:", "begin": 1000, "end": 2000, "name": "test", "paddingBeforeSec": 60, "paddingAfterSec": 120}`
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assert.Equal(t, 201, w.Result().StatusCode)
	})

	t.Run("32_ServiceRefResolution_NoColon_StillCreates", func(t *testing.T) {
		// Mock logic: ServiceRef without ":" should trigger resolution.
		// If playlist fails (default), it should proceed with original ref.
		sRefOriginal := "M3U_CHANNEL_ID"
		mock := &mockOWI{}
		mock.getTimersFunc = func(ctx context.Context) ([]openwebif.Timer, error) {
			if mock.getTimersCount == 1 {
				return nil, nil
			}
			return []openwebif.Timer{
				{ServiceRef: sRefOriginal, Begin: 1000, End: 2000, Name: "test"},
			}, nil
		}
		mock.addTimerFunc = func(ctx context.Context, ref string, b, e int64, name, desc string) error {
			if ref != sRefOriginal {
				return fmt.Errorf("ref was modified: %s", ref)
			}
			return nil
		}
		s := &Server{cfg: cfg, snap: snap, owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl { return mock }}

		body := fmt.Sprintf(`{"serviceRef": "%s", "begin": 1000, "end": 2000, "name": "test"}`, sRefOriginal)
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.AddTimer(w, req)

		assert.Equal(t, 201, w.Result().StatusCode)
	})
}
