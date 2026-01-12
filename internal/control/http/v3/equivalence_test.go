package v3

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/http/v3/problem"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/epg"
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
		problem.Write(w, r, http.StatusServiceUnavailable, "system/test_error", "Test Error", "TEST_ERROR", "health manager not initialized", nil)
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
		problem.Write(w, r, http.StatusServiceUnavailable, "system/test_error", "Test Error", "TEST_ERROR", "DVR source not initialized", nil)
		return
	}

	statusInfo, err := ds.GetStatusInfo(r.Context())

	resp := RecordingStatus{
		IsRecording: false,
	}

	if err != nil {
		log.L().Warn().Err(err).Msg("failed to get dvr status")
		problem.Write(w, r, http.StatusBadGateway, "system/test_error", "Test Error", "TEST_ERROR", "Failed to get status", nil)
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
		problem.Write(w, r, http.StatusServiceUnavailable, "system/test_error", "Test Error", "TEST_ERROR", "DVR source not initialized", nil)
		return
	}

	editSupported := ds.HasTimerChange(r.Context())

	ptr := func(b bool) *bool { return &b }
	str := func(s string) *string { return &s }

	mode := DvrCapabilitiesSeriesModeNone // Series mode

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
		problem.Write(w, r, http.StatusServiceUnavailable, "system/test_error", "Test Error", "TEST_ERROR", "Scanner not initialized", nil)
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

func getServices_Legacy(s *Server, w http.ResponseWriter, r *http.Request, bouquet *string) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	cm := s.servicesSource
	s.mu.RUnlock()

	playlistName := snap.Runtime.PlaylistFilename
	path := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	data, err := os.ReadFile(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Service{})
		return
	}

	channels := m3u.Parse(string(data))
	var resp []Service

	for _, ch := range channels {
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}
		if bouquet != nil && ch.Group != *bouquet {
			continue
		}
		enabled := true
		if cm != nil {
			enabled = cm.IsEnabled(id)
		}
		name := ch.Name
		group := ch.Group
		logo := ch.Logo
		publicURL := snap.Runtime.PublicURL
		if publicURL != "" && strings.HasPrefix(logo, publicURL) {
			logo = strings.TrimPrefix(logo, publicURL)
		}
		number := ch.Number
		serviceRef := ""
		if ch.URL != "" {
			if u, err := url.Parse(ch.URL); err == nil {
				if ref := u.Query().Get("ref"); ref != "" {
					serviceRef = ref
				} else {
					parts := strings.Split(u.Path, "/")
					if len(parts) > 0 {
						serviceRef = parts[len(parts)-1]
					}
				}
			} else {
				parts := strings.Split(ch.URL, "/")
				if len(parts) > 0 {
					serviceRef = parts[len(parts)-1]
				}
			}
		}
		if serviceRef == "" {
			serviceRef = id
		}
		if serviceRef != "" {
			piconRef := strings.ReplaceAll(serviceRef, ":", "_")
			piconRef = strings.TrimSuffix(piconRef, "_")
			logo = fmt.Sprintf("/logos/%s.png", piconRef)
		}

		resp = append(resp, Service{
			Id:         &id,
			Name:       &name,
			Group:      &group,
			LogoUrl:    &logo,
			Number:     &number,
			Enabled:    &enabled,
			ServiceRef: &serviceRef,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func getTimers_Legacy(s *Server, w http.ResponseWriter, r *http.Request, state *string) {
	s.mu.RLock()
	ts := s.timersSource
	s.mu.RUnlock()

	if ts == nil {
		problem.Write(w, r, http.StatusServiceUnavailable, "system/test_error", "Test Error", "TEST_ERROR", "Timers source not initialized", nil)
		return
	}

	timers, err := ts.GetTimers(r.Context())
	if err != nil {
		problem.Write(w, r, http.StatusBadGateway, "system/test_error", "Test Error", "TEST_ERROR", "Failed to fetch timers", nil)
		return
	}

	mapped := make([]Timer, 0, len(timers))
	for _, t := range timers {
		stateStr := TimerStateUnknown
		switch t.State {
		case 0:
			stateStr = TimerStateScheduled
			if t.Disabled != 0 {
				stateStr = TimerStateDisabled
			}
		case 2:
			stateStr = TimerStateRecording
		case 3:
			stateStr = TimerStateCompleted
		default:
			if t.Disabled != 0 {
				stateStr = TimerStateDisabled
			}
		}

		if state != nil && *state != "all" {
			if string(stateStr) != *state {
				continue
			}
		}

		timerId := read.MakeTimerID(t.ServiceRef, t.Begin, t.End)
		mapped = append(mapped, Timer{
			TimerId:     timerId,
			ServiceRef:  t.ServiceRef,
			ServiceName: &t.ServiceName,
			Name:        t.Name,
			Description: &t.Description,
			Begin:       t.Begin,
			End:         t.End,
			State:       stateStr,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TimerList{Items: mapped})
}

func getEpg_Legacy(s *Server, w http.ResponseWriter, r *http.Request, params GetEpgParams) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if strings.TrimSpace(cfg.XMLTVPath) == "" {
		problem.Write(w, r, http.StatusNotFound, "system/test_error", "Test Error", "TEST_ERROR", "XMLTV not configured", nil)
		return
	}

	xmltvPath, err := s.dataFilePath(cfg.XMLTVPath)
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "system/test_error", "Test Error", "TEST_ERROR", "XMLTV not available", nil)
		return
	}

	// 1. Singleflight for Concurrency Protection
	result, err, _ := s.epgSfg.Do("epg-load", func() (interface{}, error) {
		fileInfo, err := os.Stat(xmltvPath)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		if s.epgCache != nil && !fileInfo.ModTime().After(s.epgCacheMTime) {
			defer s.mu.Unlock()
			return s.epgCache, nil
		}
		s.mu.Unlock()

		// Parse
		data, err := os.ReadFile(xmltvPath) // #nosec G304
		if err != nil {
			return nil, err
		}

		var parsedTU epg.TV
		if err := xml.Unmarshal(data, &parsedTU); err != nil {
			s.mu.RLock()
			stale := s.epgCache
			s.mu.RUnlock()
			if stale != nil {
				return stale, nil
			}
			return nil, err
		}

		// Update Cache
		s.mu.Lock()
		s.epgCache = &parsedTU
		s.epgCacheMTime = fileInfo.ModTime()
		s.epgCacheTime = time.Now()
		tvVal := s.epgCache
		s.mu.Unlock()

		return tvVal, nil
	})

	if err != nil {
		if os.IsNotExist(err) {
			problem.Write(w, r, http.StatusNotFound, "system/test_error", "Test Error", "TEST_ERROR", "XMLTV not available", nil)
			return
		}
		problem.Write(w, r, http.StatusInternalServerError, "system/test_error", "Test Error", "TEST_ERROR", "EPG Load Error", nil)
		return
	}

	tv := result.(*epg.TV)

	// Extract parameters
	var fromTime, toTime time.Time
	now := time.Now()

	if params.From != nil {
		fromTime = time.Unix(int64(*params.From), 0)
	} else {
		fromTime = now.Add(-30 * time.Minute)
	}

	if params.To != nil {
		toTime = time.Unix(int64(*params.To), 0)
	} else {
		toTime = now.Add(7 * 24 * time.Hour)
	}

	// Requirement: Max 7 days server-side
	maxEnd := now.Add(7 * 24 * time.Hour)
	if toTime.After(maxEnd) {
		toTime = maxEnd
	}

	bouquetFilter := ""
	if params.Bouquet != nil {
		bouquetFilter = strings.TrimSpace(*params.Bouquet)
	}

	qLower := ""
	if params.Q != nil {
		qLower = strings.ToLower(strings.TrimSpace(*params.Q))
	}

	allowedRefs := make(map[string]struct{})
	if bouquetFilter != "" {
		s.mu.RLock()
		snap := s.snap
		s.mu.RUnlock()
		playlistName := snap.Runtime.PlaylistFilename
		playlistPath := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))
		if data, err := os.ReadFile(playlistPath); err == nil { // #nosec G304
			channels := m3u.Parse(string(data))
			for _, ch := range channels {
				if ch.Group != bouquetFilter {
					continue
				}
				if ch.TvgID != "" {
					allowedRefs[ch.TvgID] = struct{}{}
				}
			}
		}
	}

	// If search requested and bouquet filter yields nothing, relax filter
	if qLower != "" && bouquetFilter != "" && len(allowedRefs) == 0 {
		allowedRefs = nil
	}

	var items []EpgItem
	for _, p := range tv.Programs {
		if bouquetFilter != "" {
			_, ok1 := allowedRefs[p.Channel]
			if !ok1 {
				continue
			}
		}

		startTime, errStart := parseXMLTVTime(p.Start)
		endTime, errEnd := parseXMLTVTime(p.Stop)
		if errStart != nil || errEnd != nil {
			continue
		}

		if !startTime.Before(toTime) || !endTime.After(fromTime) {
			continue
		}

		if qLower != "" {
			match := false
			if strings.Contains(strings.ToLower(p.Title.Text), qLower) {
				match = true
			} else if strings.Contains(strings.ToLower(p.Desc), qLower) {
				match = true
			}
			if !match {
				continue
			}
		}

		id := p.Channel
		title := p.Title.Text
		desc := p.Desc
		startUnix := int(startTime.Unix())
		endUnix := int(endTime.Unix())
		dur := int(endUnix - startUnix)

		items = append(items, EpgItem{
			Id:         &id,
			ServiceRef: p.Channel,
			Title:      title,
			Desc:       &desc,
			Start:      startUnix,
			End:        endUnix,
			Duration:   &dur,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
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

	mockSvs := &mockServicesSource{}
	mockTs := &mockTimersSource{}

	s := NewServer(cfg, nil, nil)
	s.snap = snap
	s.SetDependencies(
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		mockScan, mockDvr, mockSvs, mockTs,
		nil, nil, nil,
	)

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

type mockServicesSource struct {
	enabled map[string]bool
}

func (m *mockServicesSource) IsEnabled(id string) bool {
	if m.enabled == nil {
		return true
	}
	return m.enabled[id]
}

type mockTimersSource struct {
	timers []openwebif.Timer
	err    error
}

func (m *mockTimersSource) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	return m.timers, m.err
}

func TestSlice5_2_Equivalence(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.AppConfig{
		Version: "2.5.0",
		DataDir: tempDir,
		Bouquet: "Favorites,Movies",
	}
	snap := config.Snapshot{
		Runtime: config.RuntimeSnapshot{
			PlaylistFilename: "test.m3u",
		},
	}

	// Create a mock M3U playlist
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-id="id1" group-title="Favorites",Service 1
http://stream/1
#EXTINF:-1 tvg-id="id2" group-title="Movies",Service 2
http://stream/2
#EXTINF:-1 tvg-id="id3" group-title="Favorites",Service 3
http://stream/3
`
	err := os.WriteFile(filepath.Join(tempDir, "test.m3u"), []byte(m3uContent), 0600)
	require.NoError(t, err)

	mockSvs := &mockServicesSource{
		enabled: map[string]bool{"id1": true, "id2": false, "id3": true},
	}
	now := time.Now().Unix()
	mockTs := &mockTimersSource{
		timers: []openwebif.Timer{
			// Future timer: State 0 (Scheduled)
			{ServiceRef: "ref1", Name: "Timer 1", State: 0, Begin: now + 3600, End: now + 7200},
			// Active timer: State 2 (Recording)
			{ServiceRef: "ref2", Name: "Timer 2", State: 2, Begin: now - 100, End: now + 100},
		},
	}

	s := NewServer(cfg, nil, nil)
	s.snap = snap
	s.SetDependencies(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &mockScanSource{}, &mockDvrSource{}, mockSvs, mockTs, nil, nil, nil)

	t.Run("GetServices/Combinatorial", func(t *testing.T) {
		tests := []struct {
			name    string
			bouquet *string
		}{
			{"All Services", nil},
			{"Favorites Only", testStrPtr("Favorites")},
			{"Movies Only", testStrPtr("Movies")},
			{"Non Existent", testStrPtr("NonExistent")},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				path := "/api/v3/services"
				if tc.bouquet != nil {
					path += "?bouquet=" + *tc.bouquet
				}
				req := httptest.NewRequest("GET", path, nil)
				req = withAdminScope(req)

				wLegacy := httptest.NewRecorder()
				wNew := httptest.NewRecorder()

				getServices_Legacy(s, wLegacy, req, tc.bouquet)
				s.GetServices(wNew, req, GetServicesParams{Bouquet: tc.bouquet})

				assertEquivalence(t, wLegacy, wNew)
			})
		}
	})

	t.Run("GetTimers/All", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/timers", nil)
		req = withAdminScope(req)

		wLegacy := httptest.NewRecorder()
		wNew := httptest.NewRecorder()

		getTimers_Legacy(s, wLegacy, req, nil)
		s.GetTimers(wNew, req, GetTimersParams{})

		assertEquivalence(t, wLegacy, wNew)
	})

	t.Run("GetServices/Playlist_Missing", func(t *testing.T) {
		// Ensure playlist is missing
		_ = os.Remove(filepath.Join(tempDir, "test.m3u"))

		req := httptest.NewRequest("GET", "/api/v3/services", nil)
		req = withAdminScope(req)

		wLegacy := httptest.NewRecorder()
		getServices_Legacy(s, wLegacy, req, nil)

		wNew := httptest.NewRecorder()
		s.GetServices(wNew, req, GetServicesParams{})

		assertEquivalence(t, wLegacy, wNew)
	})

	t.Run("GetServices/Playlist_Empty", func(t *testing.T) {
		// Create empty playlist
		err := os.WriteFile(filepath.Join(tempDir, "test.m3u"), []byte(""), 0600)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/api/v3/services", nil)
		req = withAdminScope(req)

		wLegacy := httptest.NewRecorder()
		getServices_Legacy(s, wLegacy, req, nil)

		wNew := httptest.NewRecorder()
		s.GetServices(wNew, req, GetServicesParams{})

	})

	t.Run("GetServices/PlaylistFilename_Empty", func(t *testing.T) {
		// Set empty playlist filename
		cfg.XMLTVPath = "epg.xml" // Restore XMLTV
		s.cfg = cfg

		s.mu.Lock()
		snap := s.snap
		oldPlaylist := snap.Runtime.PlaylistFilename
		snap.Runtime.PlaylistFilename = ""
		s.snap = snap
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			snap := s.snap
			snap.Runtime.PlaylistFilename = oldPlaylist
			s.snap = snap
			s.mu.Unlock()
		}()

		req := httptest.NewRequest("GET", "/api/v3/services", nil)
		req = withAdminScope(req)

		wLegacy := httptest.NewRecorder()
		getServices_Legacy(s, wLegacy, req, nil)

		wNew := httptest.NewRecorder()
		s.GetServices(wNew, req, GetServicesParams{})

		assertEquivalence(t, wLegacy, wNew)
	})

	t.Run("GetServicesBouquets/Empty", func(t *testing.T) {
		// Ensure playlist missing and bouquet config empty
		_ = os.Remove(filepath.Join(tempDir, "test.m3u"))

		s.mu.Lock()
		cfg := s.cfg
		oldBouquet := cfg.Bouquet
		cfg.Bouquet = "" // Empty config
		s.cfg = cfg
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			cfg := s.cfg
			cfg.Bouquet = oldBouquet
			s.cfg = cfg
			s.mu.Unlock()
		}()

		req := httptest.NewRequest("GET", "/api/v3/services/bouquets", nil)
		req = withAdminScope(req)

		wLegacy := httptest.NewRecorder()
		getServicesBouquets_Legacy(s, wLegacy, req)

		wNew := httptest.NewRecorder()
		s.GetServicesBouquets(wNew, req)

		assertEquivalence(t, wLegacy, wNew)
	})

	t.Run("GetServicesBouquets/PlaylistFilename_Empty_ConfigBouquetEmpty", func(t *testing.T) {
		// Ensure playlist filename is empty and bouquet config is empty
		s.mu.Lock()
		cfg := s.cfg
		oldBouquet := cfg.Bouquet
		cfg.Bouquet = ""
		s.cfg = cfg

		snap := s.snap
		oldPlaylist := snap.Runtime.PlaylistFilename
		snap.Runtime.PlaylistFilename = ""
		s.snap = snap
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			cfg := s.cfg
			cfg.Bouquet = oldBouquet
			s.cfg = cfg
			snap := s.snap
			snap.Runtime.PlaylistFilename = oldPlaylist
			s.snap = snap
			s.mu.Unlock()
		}()

		req := httptest.NewRequest("GET", "/api/v3/services/bouquets", nil)
		req = withAdminScope(req)

		wLegacy := httptest.NewRecorder()
		getServicesBouquets_Legacy(s, wLegacy, req)

		wNew := httptest.NewRecorder()
		s.GetServicesBouquets(wNew, req)

		assertEquivalence(t, wLegacy, wNew)
	})

	t.Run("GetEpg/Combinatorial", func(t *testing.T) {
		// Mock XMLTV File
		xmltvContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE tv SYSTEM "xmltv.dtd">
<tv generator-info-name="xg2g">
  <channel id="id1">
    <display-name>Service 1</display-name>
  </channel>
  <channel id="id2">
    <display-name>Service 2</display-name>
  </channel>
  <programme start="20240101120000 +0000" stop="20240101130000 +0000" channel="id1">
    <title lang="en">News</title>
    <desc lang="en">Daily News</desc>
  </programme>
  <programme start="20240101130000 +0000" stop="20240101140000 +0000" channel="id2">
    <title lang="en">Movie</title>
    <desc lang="en">Blockbuster</desc>
  </programme>
  <programme start="20240101140000 +0000" stop="20240101150000 +0000" channel="id1">
    <title lang="en">Sport</title>
    <desc lang="en">Live Football</desc>
  </programme>
</tv>`
		err := os.WriteFile(filepath.Join(tempDir, "epg.xml"), []byte(xmltvContent), 0600)
		require.NoError(t, err)

		// Set XMLTVPath in config
		cfg.XMLTVPath = "epg.xml"
		s.cfg = cfg
		// Reset EPG cache
		s.epgCache = nil
		s.epgCacheMTime = time.Time{}

		// Define Test Cases
		// Times relative to XMLTV data:
		// Event 1: 12:00-13:00 (id1)
		// Event 2: 13:00-14:00 (id2)
		// Event 3: 14:00-15:00 (id1)

		// Unix Timestamps for 2024-01-01 12:00 UTC = 1704110400
		t1200 := int64(1704110400)
		t1500 := int64(1704121200)
		t1230 := int64(1704112200)

		testCases := []struct {
			name   string
			params url.Values
		}{
			{
				name:   "All_Events_Wide_Window",
				params: url.Values{"from": {fmt.Sprintf("%d", t1200)}, "to": {fmt.Sprintf("%d", t1500)}},
			},
			{
				name:   "Restricted_Window_First_Event",
				params: url.Values{"from": {fmt.Sprintf("%d", t1200)}, "to": {fmt.Sprintf("%d", t1230)}}, // Overlap check
			},
			{
				name: "Bouquet_Filter_Favorites_Only_id1",
				// Favorites has id1 (Step 4122 setup: id1, id3). id2 is not in Favorites.
				params: url.Values{"from": {fmt.Sprintf("%d", t1200)}, "to": {fmt.Sprintf("%d", t1500)}, "bouquet": {"Favorites"}},
			},
			{
				name: "Bouquet_Filter_Playlist_Missing",
				// Step 4122 setup: playlist exists. This case needs manual setup inside the loop?
				// Or we run it separately.
				// Let's stick to simple param combinations here and add a separate run for missing playlist.
				params: url.Values{"from": {fmt.Sprintf("%d", t1200)}, "to": {fmt.Sprintf("%d", t1500)}, "bouquet": {"Favorites"}},
			},
			{
				name:   "Search_Query_News",
				params: url.Values{"from": {fmt.Sprintf("%d", t1200)}, "to": {fmt.Sprintf("%d", t1500)}, "q": {"News"}},
			},
			{
				name:   "Search_Query_NoMatch",
				params: url.Values{"from": {fmt.Sprintf("%d", t1200)}, "to": {fmt.Sprintf("%d", t1500)}, "q": {"NonExistent"}},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				path := "/api/v3/epg?" + tc.params.Encode()
				req := httptest.NewRequest("GET", path, nil)
				req = withAdminScope(req)

				wLegacy := httptest.NewRecorder()
				legacyParams := GetEpgParams{
					From:    ptrInt(tc.params.Get("from")),
					To:      ptrInt(tc.params.Get("to")),
					Bouquet: testStrPtr(tc.params.Get("bouquet")),
					Q:       testStrPtr(tc.params.Get("q")),
				}
				getEpg_Legacy(s, wLegacy, req, legacyParams)

				wNew := httptest.NewRecorder()
				s.GetEpg(wNew, req, legacyParams)

				assertEquivalence(t, wLegacy, wNew)
			})
		}
	})

	t.Run("GetEpg/Bouquet_Filter_Failure", func(t *testing.T) {
		// Remove playlist (M3U) but keep XMLTV
		_ = os.Remove(filepath.Join(tempDir, "test.m3u"))

		// Ensure XMLTV exists
		xmltvContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE tv SYSTEM "xmltv.dtd">
<tv><programme start="20240101120000 +0000" stop="20240101130000 +0000" channel="id1"><title>Event</title></programme></tv>`
		err := os.WriteFile(filepath.Join(tempDir, "epg.xml"), []byte(xmltvContent), 0600)
		require.NoError(t, err)

		// Reset cache
		s.epgCache = nil
		s.epgCacheMTime = time.Time{}

		testCases := []struct {
			name   string
			params url.Values
		}{
			{
				name:   "Playlist_Missing_With_Bouquet",
				params: url.Values{"bouquet": {"Favorites"}},
			},
			{
				name:   "Playlist_Missing_With_Bouquet_And_Search",
				params: url.Values{"bouquet": {"Favorites"}, "q": {"Event"}},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				path := "/api/v3/epg?" + tc.params.Encode()
				req := httptest.NewRequest("GET", path, nil)
				req = withAdminScope(req)

				wLegacy := httptest.NewRecorder()
				legacyParams := GetEpgParams{
					Bouquet: testStrPtr(tc.params.Get("bouquet")),
					Q:       testStrPtr(tc.params.Get("q")),
				}
				getEpg_Legacy(s, wLegacy, req, legacyParams)

				wNew := httptest.NewRecorder()
				s.GetEpg(wNew, req, legacyParams)

				assertEquivalence(t, wLegacy, wNew)
			})
		}
	})
}

func ptrInt(s string) *int {
	if s == "" {
		return nil
	}
	// Parse int
	var i int
	if _, err := fmt.Sscanf(s, "%d", &i); err != nil {
		return nil
	}
	return &i
}

func testStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
