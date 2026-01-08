package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- LEGACY IMPLEMENTATIONS (Preserved for Equivalence Comparison) ---

func getSystemHealth_Legacy(s *Server, w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	hm := s.healthManager
	s.mu.RUnlock()
	if hm == nil {
		http.Error(w, "health manager not initialized", http.StatusServiceUnavailable)
		return
	}
	respH := hm.Health(r.Context(), true)
	status := SystemHealthStatusOk
	if respH.Status != health.StatusHealthy {
		status = SystemHealthStatusDegraded
	}
	receiverStatus := ComponentStatusStatusOk
	if res, ok := respH.Checks["receiver_connection"]; ok {
		if res.Status != health.StatusHealthy {
			receiverStatus = ComponentStatusStatusError
		}
	} else {
		receiverStatus = ComponentStatusStatusError
	}
	epgStatus := EPGStatusStatusOk
	if res, ok := respH.Checks["epg_status"]; ok {
		if res.Status != health.StatusHealthy {
			epgStatus = EPGStatusStatusMissing
		}
	} else {
		epgStatus = EPGStatusStatusMissing
	}
	int64Ptr := func(i int64) *int64 { return &i }
	missing := 0
	resp := SystemHealth{
		Status: &status,
		Receiver: &ComponentStatus{
			Status:    &receiverStatus,
			LastCheck: &respH.Timestamp,
		},
		Epg: &EPGStatus{
			Status:          &epgStatus,
			MissingChannels: &missing,
		},
		Version:       &respH.Version,
		UptimeSeconds: int64Ptr(respH.Uptime),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func getSystemConfig_Legacy(s *Server, w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	epgSource := EPGConfigSource(cfg.EPGSource)
	bouquets := make([]string, 0)
	for _, name := range strings.Split(cfg.Bouquet, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			bouquets = append(bouquets, trimmed)
		}
	}
	openWebIF := &OpenWebIFConfig{
		BaseUrl:    &cfg.Enigma2.BaseURL,
		StreamPort: &cfg.Enigma2.StreamPort,
	}
	if cfg.Enigma2.Username != "" {
		openWebIF.Username = &cfg.Enigma2.Username
	}
	picons := &PiconsConfig{
		BaseUrl: &cfg.PiconBase,
	}
	deliveryPolicy := StreamingConfigDeliveryPolicy(cfg.Streaming.DeliveryPolicy)
	streaming := &StreamingConfig{
		DeliveryPolicy: &deliveryPolicy,
	}
	resp := AppConfig{
		Version:   &cfg.Version,
		DataDir:   &cfg.DataDir,
		LogLevel:  &cfg.LogLevel,
		OpenWebIF: openWebIF,
		Bouquets:  &bouquets,
		Epg: &EPGConfig{
			Days:    &cfg.EPGDays,
			Enabled: &cfg.EPGEnabled,
			Source:  &epgSource,
		},
		Picons:    picons,
		Streaming: streaming,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func getServicesBouquets_Legacy(s *Server, w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	playlistName := snap.Runtime.PlaylistFilename
	path := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	var bouquetNames []string

	if data, err := os.ReadFile(path); err == nil {
		channels := m3u.Parse(string(data))
		seen := make(map[string]bool)
		for _, ch := range channels {
			if ch.Group != "" && !seen[ch.Group] {
				bouquetNames = append(bouquetNames, ch.Group)
				seen[ch.Group] = true
			}
		}
	}

	if len(bouquetNames) == 0 {
		configured := strings.Split(cfg.Bouquet, ",")
		for _, b := range configured {
			if trimmed := strings.TrimSpace(b); trimmed != "" {
				bouquetNames = append(bouquetNames, trimmed)
			}
		}
	}

	sort.Strings(bouquetNames)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bouquetNames)
}

func getDvrStatus_Legacy(s *Server, w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	if ds == nil {
		http.Error(w, "DVR source not initialized", http.StatusServiceUnavailable)
		return
	}

	statusInfo, err := ds.GetStatusInfo(r.Context())

	resp := RecordingStatus{
		IsRecording: false,
	}

	if err != nil {
		log.L().Warn().Err(err).Msg("failed to get dvr status")
		http.Error(w, "Failed to get status", http.StatusBadGateway)
		return
	}

	if statusInfo.IsRecording == "true" {
		resp.IsRecording = true
	}
	resp.ServiceName = &statusInfo.ServiceName

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func getDvrCapabilities_Legacy(s *Server, w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	if ds == nil {
		http.Error(w, "DVR source not initialized", http.StatusServiceUnavailable)
		return
	}

	editSupported := ds.HasTimerChange(r.Context())

	ptr := func(b bool) *bool { return &b }
	str := func(s string) *string { return &s }

	mode := None // Series mode

	resp := DvrCapabilities{
		Timers: struct {
			Delete         *bool `json:"delete,omitempty"`
			Edit           *bool `json:"edit,omitempty"`
			ReadBackVerify *bool `json:"readBackVerify,omitempty"`
		}{
			Delete:         ptr(true),
			Edit:           ptr(true),
			ReadBackVerify: ptr(true),
		},
		Conflicts: struct {
			Preview       *bool `json:"preview,omitempty"`
			ReceiverAware *bool `json:"receiverAware,omitempty"`
		}{
			Preview:       ptr(true),
			ReceiverAware: ptr(editSupported),
		},
		Series: struct {
			DelegatedProvider *string                    `json:"delegatedProvider,omitempty"`
			Mode              *DvrCapabilitiesSeriesMode `json:"mode,omitempty"`
			Supported         *bool                      `json:"supported,omitempty"`
		}{
			Supported: ptr(false),
			Mode:      (*DvrCapabilitiesSeriesMode)(str(string(mode))),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func getSystemScanStatus_Legacy(s *Server, w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ss := s.scanSource
	s.mu.RUnlock()

	if ss == nil {
		http.Error(w, "Scanner not initialized", http.StatusServiceUnavailable)
		return
	}

	st := ss.GetStatus()
	state := ScanStatusState(st.State)
	start := st.StartedAt
	total := st.TotalChannels
	scanned := st.ScannedChannels
	updated := st.UpdatedCount
	lastErr := st.LastError

	resp := ScanStatus{
		State:           &state,
		StartedAt:       &start,
		TotalChannels:   &total,
		ScannedChannels: &scanned,
		UpdatedCount:    &updated,
	}
	if st.FinishedAt > 0 {
		finish := st.FinishedAt
		resp.FinishedAt = &finish
	}
	if st.LastError != "" {
		resp.LastError = &lastErr
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// --- EQUIVALENCE HELPERS ---

// normalizeJSON recursively cleans a JSON-decoded tree for comparison.
// It ensures that {} vs null vs omitted are treated distinctly as decoded.
func normalizeJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			return map[string]any{}
		}
		newMap := make(map[string]any)
		for k, v := range x {
			// Normalize unstable timestamps
			if k == "last_check" {
				newMap[k] = "STABLE_TIMESTAMP"
				continue
			}
			newMap[k] = normalizeJSON(v)
		}
		return newMap
	case []any:
		if len(x) == 0 {
			return []any{}
		}
		newSlice := make([]any, len(x))
		for i, v := range x {
			newSlice[i] = normalizeJSON(v)
		}
		return newSlice
	default:
		return x
	}
}

func assertEquivalence(t *testing.T, wLegacy, wNew *httptest.ResponseRecorder) {
	t.Helper()

	// 1. Assert Status Equality
	require.Equal(t, wLegacy.Code, wNew.Code, "HTTP Status Code must be identical")

	// 2. Assert Header Subset (Contract headers)
	headers := []string{"Content-Type", "Cache-Control", "Retry-After"}
	for _, h := range headers {
		assert.Equal(t, wLegacy.Header().Get(h), wNew.Header().Get(h), "Header %s must match", h)
	}

	// 3. Semantic JSON Equality (only if Content-Type is application/json)
	if strings.Contains(wLegacy.Header().Get("Content-Type"), "application/json") {
		var legacyJSON, newJSON any
		require.NoError(t, json.Unmarshal(wLegacy.Body.Bytes(), &legacyJSON), "Legacy body must be valid JSON")
		require.NoError(t, json.Unmarshal(wNew.Body.Bytes(), &newJSON), "New body must be valid JSON")

		normLegacy := normalizeJSON(legacyJSON)
		normNew := normalizeJSON(newJSON)

		if !reflect.DeepEqual(normLegacy, normNew) {
			// Pretty print diff for easier debugging
			legacyPretty, _ := json.MarshalIndent(normLegacy, "", "  ")
			newPretty, _ := json.MarshalIndent(normNew, "", "  ")
			t.Errorf("JSON mismatch!\nLegacy:\n%s\nNew:\n%s", legacyPretty, newPretty)
		}
	} else {
		// Fallback for non-JSON content
		assert.Equal(t, wLegacy.Body.Bytes(), wNew.Body.Bytes(), "Raw body must be identical for non-JSON content")
	}
}

// --- HARDENED EQUIVALENCE TESTS ---

func TestHealthEquivalence_Hardened(t *testing.T) {
	hm := health.NewManager("2.0.0")
	server := &Server{healthManager: hm}
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)

	wLegacy := httptest.NewRecorder()
	getSystemHealth_Legacy(server, wLegacy, req)

	wNew := httptest.NewRecorder()
	server.GetSystemHealth(wNew, req)

	assertEquivalence(t, wLegacy, wNew)
}

func TestConfigEquivalence_Hardened(t *testing.T) {
	cfg := config.AppConfig{
		Version: "1.0.0",
		Bouquet: "A, B",
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	server := &Server{cfg: cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)

	wLegacy := httptest.NewRecorder()
	getSystemConfig_Legacy(server, wLegacy, req)

	wNew := httptest.NewRecorder()
	server.GetSystemConfig(wNew, req)

	assertEquivalence(t, wLegacy, wNew)
}

func TestConfigRedaction_Hardened(t *testing.T) {
	marker := "XG2G__SECRET_MARKER__9f3a8b7c6d5e4f3a2b1__DO_NOT_LEAK"
	cfg := config.AppConfig{
		Enigma2: config.Enigma2Settings{
			Password: marker,
		},
	}
	server := &Server{cfg: cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	w := httptest.NewRecorder()

	server.GetSystemConfig(w, req)

	// 1. Assert absolute absence in raw response bytes
	raw := w.Body.String()
	assert.NotContains(t, raw, marker, "Secret marker MUST NOT appear in the raw HTTP response body")

	// 2. Assert field absence in decoded structure (proves key removal, not just masking)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	owi, ok := body["openWebIF"].(map[string]any)
	require.True(t, ok, "Config should have openWebIF object")
	_, hasPassword := owi["password"]
	assert.False(t, hasPassword, "Field 'password' MUST be absent from the DTO, not merely masked")
}

func TestHealthVsHealthzDivergence_Hardened(t *testing.T) {
	// GIVEN a health manager that would report degraded status
	// (In this test, we don't even need to mock it deeply, we just prove healthz is static/independent)
	server := &Server{healthManager: nil} // healthManager nil would cause /health to error but /healthz to pass

	reqZ := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz", nil)
	wZ := httptest.NewRecorder()
	server.GetSystemHealthz(wZ, reqZ)

	assert.Equal(t, http.StatusOK, wZ.Code, "Healthz must be OK even without health manager")
	var bodyZ map[string]string
	require.NoError(t, json.Unmarshal(wZ.Body.Bytes(), &bodyZ))
	assert.Equal(t, "ok", bodyZ["status"])

	// AND health (rich) should fail/reflect state
	reqH := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)
	wH := httptest.NewRecorder()
	server.GetSystemHealth(wH, reqH)
	assert.Equal(t, http.StatusServiceUnavailable, wH.Code, "Rich health must reflect the actual service state")
}

func TestSlice5_1_Equivalence(t *testing.T) {
	cfg := config.AppConfig{
		Version: "2.5.0",
		DataDir: t.TempDir(),
		Bouquet: "Favorites,Movies",
		Enigma2: config.Enigma2Settings{BaseURL: "http://enigma2"},
	}
	snap := config.Snapshot{
		Runtime: config.RuntimeSnapshot{
			PlaylistFilename: "test.m3u",
		},
	}

	// Setup mock data
	mockScan := &mockScanSource{
		status: scan.ScanStatus{
			State:           "running",
			StartedAt:       123456789,
			TotalChannels:   100,
			ScannedChannels: 42,
			UpdatedCount:    5,
		},
	}
	mockDvr := &mockDvrSource{
		statusInfo: &openwebif.StatusInfo{
			IsRecording: "true",
			ServiceName: "ZDF HD",
		},
		canEdit: true,
	}

	s := NewServer(cfg, nil, nil)
	s.snap = snap
	s.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, mockScan, mockDvr, nil, nil)

	testCases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		legacy  func(http.ResponseWriter, *http.Request)
		path    string
	}{
		{
			name:    "Scan Status",
			handler: s.GetSystemScanStatus,
			legacy:  func(w http.ResponseWriter, r *http.Request) { getSystemScanStatus_Legacy(s, w, r) },
			path:    "/api/v3/system/scan",
		},
		{
			name:    "DVR Capabilities",
			handler: s.GetDvrCapabilities,
			legacy:  func(w http.ResponseWriter, r *http.Request) { getDvrCapabilities_Legacy(s, w, r) },
			path:    "/api/v3/dvr/capabilities",
		},
		{
			name:    "DVR Status",
			handler: s.GetDvrStatus,
			legacy:  func(w http.ResponseWriter, r *http.Request) { getDvrStatus_Legacy(s, w, r) },
			path:    "/api/v3/dvr/status",
		},
		{
			name:    "Services Bouquets",
			handler: s.GetServicesBouquets,
			legacy:  func(w http.ResponseWriter, r *http.Request) { getServicesBouquets_Legacy(s, w, r) },
			path:    "/api/v3/services/bouquets",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req = withAdminScope(req)

			wLegacy := httptest.NewRecorder()
			wNew := httptest.NewRecorder()

			tc.legacy(wLegacy, req)
			tc.handler(wNew, req)

			assertEquivalence(t, wLegacy, wNew)
		})
	}
}

func withAdminScope(r *http.Request) *http.Request {
	p := auth.NewPrincipal("test-token", "admin", []string{string(ScopeV3Admin)})
	ctx := auth.WithPrincipal(r.Context(), p)
	return r.WithContext(ctx)
}

type mockScanSource struct {
	status scan.ScanStatus
}

func (m *mockScanSource) GetStatus() scan.ScanStatus { return m.status }

type mockDvrSource struct {
	statusInfo *openwebif.StatusInfo
	canEdit    bool
}

func (m *mockDvrSource) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	if m.statusInfo == nil {
		return nil, errors.New("receiver unreachable")
	}
	return m.statusInfo, nil
}
func (m *mockDvrSource) HasTimerChange(ctx context.Context) bool { return m.canEdit }
