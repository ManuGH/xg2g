package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestDashboard() *Dashboard {
	config := config.AppConfig{
		Version:    "test-1.0.0",
		OWIBase:    "http://test.example.com",
		Bouquet:    "Test Bouquet",
		StreamPort: 8001,
		EPGEnabled: true,
		EPGDays:    7,
		DataDir:    "/tmp/test",
	}

	logger := zerolog.New(nil).With().Logger()
	return New(config, logger)
}

func TestNew(t *testing.T) {
	config := config.AppConfig{Version: "1.0.0"}
	logger := zerolog.New(nil).With().Logger()

	dashboard := New(config, logger)

	assert.NotNil(t, dashboard)
	assert.Equal(t, config, dashboard.config)
	assert.NotNil(t, dashboard.stats)
	assert.False(t, dashboard.stats.StartTime.IsZero())
}

func TestUpdateStats(t *testing.T) {
	dashboard := createTestDashboard()

	time.Sleep(100 * time.Millisecond)
	dashboard.UpdateStats()

	assert.Greater(t, dashboard.stats.MemoryUsageMB, 0.0)
	assert.Greater(t, dashboard.stats.GoroutineCount, 0)
	assert.GreaterOrEqual(t, dashboard.stats.UptimeSeconds, int64(0))
}

func TestRecordRefresh(t *testing.T) {
	dashboard := createTestDashboard()

	dashboard.RecordRefresh(true, 25, 3, 500)

	assert.Equal(t, int64(1), dashboard.stats.RefreshCount)
	assert.Equal(t, int64(0), dashboard.stats.ErrorCount)
	assert.Equal(t, 25, dashboard.stats.ChannelsActive)
	assert.Equal(t, 3, dashboard.stats.BouquetsActive)
	assert.Equal(t, 500, dashboard.stats.EPGProgrammes)
	assert.False(t, dashboard.stats.LastRefresh.IsZero())
}

func TestHandleAPIStats(t *testing.T) {
	dashboard := createTestDashboard()

	dashboard.RecordRefresh(true, 10, 2, 200)
	dashboard.RecordRequest(25 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()

	dashboard.HandleAPIStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var stats ServiceStats
	err := json.Unmarshal(w.Body.Bytes(), &stats)
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.RefreshCount)
	assert.Equal(t, 10, stats.ChannelsActive)
}
