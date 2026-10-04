// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/epg"
	householddomain "github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	v3bus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestResolverMulti(t *testing.T) (*edge.Resolver, map[string]string) {
	t.Helper()
	secret := "secret-test-key-of-at-least-32-bytes!!"
	parser, err := sourceref.NewParser([]byte(secret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	rawCanary1 := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	rawCanary2 := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-2/live/token-xyz-987/channel_second.ts:Canary Channel 2"

	src1, err := parser.Parse(rawCanary1)
	require.NoError(t, err)
	src2, err := parser.Parse(rawCanary2)
	require.NoError(t, err)

	require.NoError(t, reg.Replace([]sourceref.Source{src1, src2}))

	res := edge.NewResolver(reg, parser, metrics.IncIPTVLegacyIngress)
	return res, map[string]string{
		string(src1.ID()): rawCanary1,
		string(src2.ID()): rawCanary2,
	}
}

func TestIPTVInbound_PostLivePlaybackInfo(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaqueID string
	for k := range canaryMap {
		opaqueID = k
		break
	}

	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)
	s.SetJWTSecret(jwtTestSecret)
	s.SetDependencies(Dependencies{Scan: verifiedLivePlaybackScanner(), RecordingsService: svc})
	s.SetIPTVResolver(res)

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`

	// 1. Opaque known: resolves and returns 200 with playback decision
	{
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var payload map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		assert.NotEmpty(t, payload["playbackDecisionToken"])
	}

	// 2. Opaque unknown: returns 404 ProblemDetails, zero input echo
	{
		unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, unknownID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusNotFound, w.Code)
		assert.NotContains(t, w.Body.String(), unknownID)
		assert.Contains(t, w.Body.String(), problemcode.CodeNotFound)
	}

	// 3. Malformed iptv_ ID: returns 400 ProblemDetails, zero input echo
	{
		malformedID := "iptv_not_valid_base32!!!"
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, malformedID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.NotContains(t, w.Body.String(), malformedID)
		assert.Contains(t, w.Body.String(), problemcode.CodeInvalidInput)
	}

	// 4. DVB ref: passes through
	{
		dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, dvbRef, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	// 5. Nil resolver: opaque fails with 404, DVB passes
	{
		sNoRes := createTestServerDTO(svc)
		sNoRes.SetJWTSecret(jwtTestSecret)
		sNoRes.SetDependencies(Dependencies{Scan: verifiedLivePlaybackScanner(), RecordingsService: svc})
		sNoRes.SetIPTVResolver(nil)

		// Opaque returns 404
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		sNoRes.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusNotFound, w.Code)

		// DVB succeeds
		dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
		bodyDVB := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, dvbRef, caps)
		wDVB := httptest.NewRecorder()
		rDVB := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(bodyDVB))
		rDVB.Header.Set("Content-Type", "application/json")
		sNoRes.PostLivePlaybackInfo(wDVB, rDVB, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusOK, wDVB.Code)
	}
}

func TestIPTVInbound_TokenRoundTrip_PlaybackInfoToIntents(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaqueID, rawCanary string
	for k, v := range canaryMap {
		opaqueID = k
		rawCanary = v
		break
	}

	s, st := newV3TestServer(t, t.TempDir())
	svc := new(MockRecordingsService)
	b := v3bus.NewMemoryBus()
	s.SetDependencies(Dependencies{
		Bus:               b,
		Store:             st,
		Scan:              verifiedLivePlaybackScanner(),
		RecordingsService: svc,
	})
	s.SetIPTVResolver(res)

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`

	// Step 1: Request stream-info with opaque ID to obtain decision token
	body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var streamInfoResp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &streamInfoResp))
	token, ok := streamInfoResp["playbackDecisionToken"].(string)
	require.True(t, ok)
	require.NotEmpty(t, token)

	// Step 2: Client passes the SAME opaque ID and the obtained token to /api/v3/intents
	intentJSON := fmt.Sprintf(`{
		"type":"stream.start",
		"serviceRef":%q,
		"playbackDecisionToken":%q,
		"client":%s
	}`, opaqueID, token, caps)

	wIntent := httptest.NewRecorder()
	rIntent := httptest.NewRequest(http.MethodPost, "/api/v3/intents", strings.NewReader(intentJSON))
	rIntent.Header.Set("Content-Type", "application/json")
	rIntent.Header.Set("Authorization", "Bearer test-token")

	s.handleV3Intents(wIntent, rIntent)
	require.Equal(t, http.StatusAccepted, wIntent.Code, wIntent.Body.String())

	// Step 3: Verify negative control: intents with unknown opaque ID returns 404
	{
		unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
		badIntentJSON := fmt.Sprintf(`{"type":"stream.start","serviceRef":%q}`, unknownID)
		wBad := httptest.NewRecorder()
		rBad := httptest.NewRequest(http.MethodPost, "/api/v3/intents", strings.NewReader(badIntentJSON))
		rBad.Header.Set("Content-Type", "application/json")
		rBad.Header.Set("Authorization", "Bearer test-token")

		s.handleV3Intents(wBad, rBad)
		require.Equal(t, http.StatusNotFound, wBad.Code)
		assert.NotContains(t, wBad.Body.String(), unknownID)
		assert.Contains(t, wBad.Body.String(), problemcode.CodeNotFound)
	}

	// Step 4: Verify negative control: intents with malformed ID returns 400
	{
		malformedID := "iptv_invalid_base32!!!"
		badIntentJSON := fmt.Sprintf(`{"type":"stream.start","serviceRef":%q}`, malformedID)
		wBad := httptest.NewRecorder()
		rBad := httptest.NewRequest(http.MethodPost, "/api/v3/intents", strings.NewReader(badIntentJSON))
		rBad.Header.Set("Content-Type", "application/json")
		rBad.Header.Set("Authorization", "Bearer test-token")

		s.handleV3Intents(wBad, rBad)
		require.Equal(t, http.StatusBadRequest, wBad.Code)
		assert.NotContains(t, wBad.Body.String(), malformedID)
		assert.Contains(t, wBad.Body.String(), problemcode.CodeInvalidInput)
	}

	_ = rawCanary
}

func TestIPTVInbound_NowNextEPG(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaque1, raw1 string
	for k, v := range canaryMap {
		opaque1 = k
		raw1 = v
		break
	}

	mockSource := new(MockEpgSource)
	s := &Server{
		epgSource: mockSource,
	}
	s.SetIPTVResolver(res)

	now := time.Now()
	progs := []epg.Programme{
		{
			Channel: raw1,
			Title:   epg.Title{Text: "Canary Program Live"},
			Start:   now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
			Stop:    now.Add(35 * time.Minute).Format(xmltvTimeFormat),
		},
	}
	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil)

	// Batch with: known opaque, unknown opaque, and DVB ref
	unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
	dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	reqBody := fmt.Sprintf(`{"services":[%q, %q, %q]}`, opaque1, unknownID, dvbRef)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", strings.NewReader(reqBody))
	r.Header.Set("Content-Type", "application/json")

	s.handleNowNextEPG(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp NowNextResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 3)

	// Item 0: Opaque known -> resolved to raw1 internally to find EPG, but returns original opaque1!
	assert.Equal(t, opaque1, resp.Items[0].ServiceRef)
	require.NotNil(t, resp.Items[0].Now)
	assert.Equal(t, "Canary Program Live", resp.Items[0].Now.Title)
	// Must not leak the raw canary URL into the response!
	assert.NotContains(t, w.Body.String(), "SECRET-CANARY-1")

	// Item 1: Opaque unknown -> included with original unknownID and nil Now/Next
	assert.Equal(t, unknownID, resp.Items[1].ServiceRef)
	assert.Nil(t, resp.Items[1].Now)

	// Item 2: DVB ref
	assert.Equal(t, dvbRef, resp.Items[2].ServiceRef)
}

func TestIPTVInbound_PostLivePlaybackSummary(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaque1, raw1 string
	for k, v := range canaryMap {
		opaque1 = k
		raw1 = v
		break
	}

	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)
	s.SetDependencies(Dependencies{Scan: verifiedLivePlaybackScanner(), RecordingsService: svc})
	s.SetIPTVResolver(res)

	unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
	dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"

	body := fmt.Sprintf(`{
		"serviceRefs":[%q, %q, %q],
		"capabilities":%s
	}`, opaque1, unknownID, dvbRef, playbackSummaryTestCaps)

	w := postPlaybackSummary(t, s, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var rawResp struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rawResp))

	// Invariant: Response map key must be the client-supplied opaque1, never raw1!
	assert.Contains(t, rawResp.Items, opaque1)
	assert.NotContains(t, rawResp.Items, raw1)
	// Invariant: Unknown opaque element is omitted from the batch
	assert.NotContains(t, rawResp.Items, unknownID)
	// DVB ref is present
	assert.Contains(t, rawResp.Items, dvbRef)
}

func TestIPTVInbound_Timers(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaque1, raw1 string
	for k, v := range canaryMap {
		opaque1 = k
		raw1 = v
		break
	}

	var capturedAddSRef string
	var capturedDeleteSRef string
	var addedTimer *openwebif.Timer
	mockClient := &mockOWI{
		getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
			timers := []openwebif.Timer{
				{ServiceRef: raw1, Begin: 1000, End: 2000, Name: "Existing Timer"},
			}
			if addedTimer != nil {
				timers = append(timers, *addedTimer)
			}
			return timers, nil
		},
		addTimerFunc: func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
			capturedAddSRef = sRef
			addedTimer = &openwebif.Timer{
				ServiceRef: sRef,
				Begin:      begin,
				End:        end,
				Name:       name,
			}
			return nil
		},
		deleteTimerFunc: func(ctx context.Context, sRef string, begin, end int64) error {
			capturedDeleteSRef = sRef
			return nil
		},
	}

	s := &Server{
		cfg: config.AppConfig{
			DataDir: t.TempDir(),
		},
		snap: config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "missing.m3u"}},
		owiFactory: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
			return mockClient
		},
	}
	s.SetIPTVResolver(res)

	begin := int64(3000)
	end := int64(4000)

	// 1. AddTimer with opaque known ref -> calls owi.AddTimer with raw ref
	{
		body := fmt.Sprintf(`{"serviceRef":%q,"begin":%d,"end":%d,"name":"Test Timer"}`, opaque1, begin, end)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.AddTimer(w, r)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		assert.Equal(t, raw1, capturedAddSRef)
	}

	// 2. AddTimer with unknown opaque ref -> 404 zero echo
	{
		unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
		body := fmt.Sprintf(`{"serviceRef":%q,"begin":%d,"end":%d,"name":"Bad Timer"}`, unknownID, begin, end)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.AddTimer(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
		assert.NotContains(t, w.Body.String(), unknownID)
	}

	// 3. PreviewConflicts with opaque known ref -> resolves before conflict check
	{
		body := fmt.Sprintf(`{"proposed":{"serviceRef":%q,"begin":%d,"end":%d}}`, opaque1, begin, end)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/timers/conflicts/preview", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.PreviewConflicts(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	// 4. GetTimer with timer ID based on opaque ID -> resolves to raw ref to find timer
	{
		timerID := read.MakeTimerID(opaque1, 1000, 2000)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/timers/"+timerID, nil)

		s.GetTimer(w, r, timerID)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	// 5. DeleteTimer with timer ID based on opaque ID -> resolves to raw ref for owi.DeleteTimer
	{
		timerID := read.MakeTimerID(opaque1, 1000, 2000)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodDelete, "/api/v3/timers/"+timerID, nil)

		s.DeleteTimer(w, r, timerID)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		assert.Equal(t, raw1, capturedDeleteSRef)
	}
}

func TestIPTVInbound_HouseholdProfileWrites(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaque1, raw1 string
	for k, v := range canaryMap {
		opaque1 = k
		raw1 = v
		break
	}

	unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
	dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"

	s := &Server{}
	s.SetIPTVResolver(res)

	// 1. resolveHouseholdRefs directly: element-wise resolution omitting unknown opaque IDs
	inRefs := []string{opaque1, unknownID, dvbRef}
	outRefs := s.resolveHouseholdRefs(inRefs)
	assert.Equal(t, []string{raw1, dvbRef}, outRefs)

	// 2. PostHouseholdProfiles & PutHouseholdProfile with household service
	hhStore := householddomain.NewMemoryStore()
	hhSvc := householddomain.NewService(hhStore)
	sLegacy := &Server{
		householdService: hhSvc,
	}
	sLegacy.SetIPTVResolver(res)

	profBody := householddomain.Profile{
		ID:                  "test-profile",
		Name:                "Test Profile",
		Kind:                householddomain.ProfileKindAdult,
		AllowedServiceRefs:  []string{opaque1, unknownID},
		FavoriteServiceRefs: []string{dvbRef, unknownID},
	}
	bodyBytes, err := json.Marshal(profBody)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/household/profiles", bytes.NewReader(bodyBytes))
	r.Header.Set("Content-Type", "application/json")

	sLegacy.PostHouseholdProfiles(w, r, PostHouseholdProfilesParams{})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	stored, err := hhSvc.Resolve(context.Background(), "test-profile")
	require.NoError(t, err)
	// Assert: opaque1 resolved to raw1, unknownID omitted
	assert.Equal(t, []string{raw1}, stored.AllowedServiceRefs)
	assert.Equal(t, []string{strings.TrimSuffix(dvbRef, ":")}, stored.FavoriteServiceRefs)
}

func TestIPTVInbound_PostPlaybackTelemetry(t *testing.T) {
	res, canaryMap := newTestResolverMulti(t)
	var opaque1 string
	for k := range canaryMap {
		opaque1 = k
		break
	}

	s := &Server{}
	s.SetIPTVResolver(res)

	// Send telemetry batch with opaque ID and legacy raw ref
	legacyRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//legacy.invalid/stream.ts:Legacy"
	batch := PlaybackTelemetryBatch{
		Client: PlaybackTelemetryClient{
			Platform: PlaybackTelemetryClientPlatformIos,
		},
		Events: []PlaybackTelemetryEvent{
			{
				Kind:       PlaybackTelemetryEventKindSessionStart,
				OccurredAt: time.Now(),
				ServiceRef: &opaque1,
			},
			{
				Kind:       PlaybackTelemetryEventKindHeartbeat,
				OccurredAt: time.Now(),
				ServiceRef: &legacyRef,
			},
		},
	}
	bodyBytes, err := json.Marshal(batch)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/telemetry/playback", bytes.NewReader(bodyBytes))
	r.Header.Set("Content-Type", "application/json")

	s.PostPlaybackTelemetry(w, r)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
}
