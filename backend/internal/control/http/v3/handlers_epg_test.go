package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEpgSource is a mock implementation of the EpgSource interface
type MockEpgSource struct {
	mock.Mock
}

func (m *MockEpgSource) GetPrograms(ctx context.Context) ([]epg.Programme, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]epg.Programme), args.Error(1)
}

func (m *MockEpgSource) GetBouquetServiceRefs(ctx context.Context, bouquet string) (map[string]struct{}, error) {
	args := m.Called(ctx, bouquet)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]struct{}), args.Error(1)
}

func TestGetEpg_ResponseShape(t *testing.T) {
	// Setup
	mockSource := new(MockEpgSource)
	// We need to bypass the read package logic or mock it, but handlers_epg.go calls read.GetEpg directly.
	// Since read.GetEpg is a function and not on an interface, we can't easily mock it without refactoring
	// or relying on the behavior of read.GetEpg using the source we provide.

	// However, looking at handlers_epg.go:
	// entries, err := read.GetEpg(r.Context(), src, q, read.RealClock{})

	// read.GetEpg uses src.GetPrograms. So if we mock src, we can control the output associated with read.GetEpg logic.

	// Let's create a server instance with the mock source
	// Note: We need to see how Server is constructed and if we can inject epgSource.
	// In handlers_epg.go: src := s.epgSource

	server := &Server{
		epgSource: mockSource,
	}

	// Mock data
	now := time.Now()
	progs := []epg.Programme{
		{
			Channel: "1:0:1:1:1:1:1:0:0:0:",
			Title:   epg.Title{Text: "Test Show"},
			Start:   now.Format("20060102150405 -0700"), // XMLTV format
			Stop:    now.Add(1 * time.Hour).Format("20060102150405 -0700"),
		},
	}

	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil)
	// For default query, it might not call GetBouquetServiceRefs unless bouquet filter is used,
	// but read.GetEpg might verify services. Let's assume simple path first.

	req := httptest.NewRequest("GET", "/api/v3/epg", nil)
	w := httptest.NewRecorder()

	// Execute
	server.GetEpg(w, req, GetEpgParams{})

	// Verify
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Response should be a bare array (not wrapped in {"items": ...})
	var items []EpgItem
	err := json.NewDecoder(resp.Body).Decode(&items)
	assert.NoError(t, err, "Response should be a bare JSON array")
	assert.Len(t, items, 1)
	assert.Equal(t, "Test Show", items[0].Title)
}
