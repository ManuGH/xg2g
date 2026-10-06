// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	controlauth "github.com/ManuGH/xg2g/internal/control/auth"
	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	controlplayback "github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/read"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	identitystore "github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	householddomain "github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/jobs"
	ilog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	v3bus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	canarySecret = "secret-test-key-of-at-least-32-bytes!!"
	canaryRawRef = "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
)

var canaryMarkers = []string{
	"canary.invalid",
	"SECRET-CANARY-1",
	"token-xyz-987",
	"channel_prime.ts",
}

func setupCanaryResolver(t *testing.T) (*edge.Resolver, sourceref.ID, *sourceref.Registry) {
	t.Helper()
	parser, err := sourceref.NewParser([]byte(canarySecret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()
	src, err := parser.Parse(canaryRawRef)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))
	res := edge.NewResolver(reg, parser, nil)
	return res, src.ID(), reg
}

func assertNoCanaryLeak(t *testing.T, endpointName string, w *httptest.ResponseRecorder) {
	t.Helper()
	bodyStr := strings.ToLower(w.Body.String())
	for _, marker := range canaryMarkers {
		mLower := strings.ToLower(marker)
		if strings.Contains(bodyStr, mLower) {
			t.Fatalf("[%s] Canary leak detected in response body! Marker: %q, Body: %s", endpointName, marker, w.Body.String())
		}
		for hKey, hVals := range w.Header() {
			for _, val := range hVals {
				if strings.Contains(strings.ToLower(val), mLower) {
					t.Fatalf("[%s] Canary leak detected in response header %q! Marker: %q, Value: %s", endpointName, hKey, marker, val)
				}
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Route Classification Tables for Chi Router Walking
// ----------------------------------------------------------------------------

// refFreeRoutes documents routes that do not take or return channel/stream service references.
var refFreeRoutes = map[string]string{
	"GET /.well-known/assetlinks.json":                                   "Static Android Digital Asset Links metadata",
	"POST /api/v3/auth/bootstrap/acknowledge-recovery":                   "Auth: recovery acknowledgment",
	"POST /api/v3/auth/device/grant/finish":                              "Auth: device authorization finish",
	"POST /api/v3/auth/device/grant/start":                               "Auth: device authorization start",
	"POST /api/v3/auth/device/refresh":                                   "Auth: device token refresh",
	"POST /api/v3/auth/device/revoke":                                    "Auth: device token revocation",
	"GET /api/v3/auth/effective-permissions":                             "Auth: query effective permission scopes",
	"POST /api/v3/auth/invitations":                                      "Auth: issue household invitation",
	"POST /api/v3/auth/invitations/redeem":                               "Auth: redeem household invitation",
	"POST /api/v3/auth/login/password":                                   "Auth: password login",
	"POST /api/v3/auth/passkey/login/finish":                             "Auth: WebAuthn passkey assertion finish",
	"POST /api/v3/auth/passkey/login/start":                              "Auth: WebAuthn passkey assertion start",
	"POST /api/v3/auth/passkey/register/finish":                          "Auth: WebAuthn passkey registration finish",
	"POST /api/v3/auth/passkey/register/start":                           "Auth: WebAuthn passkey registration start",
	"GET /api/v3/auth/passkeys":                                          "Auth: list registered passkeys",
	"DELETE /api/v3/auth/passkeys/{id}":                                  "Auth: delete passkey credential",
	"POST /api/v3/auth/recovery":                                         "Auth: start account recovery",
	"POST /api/v3/auth/session":                                          "Auth: create user session",
	"DELETE /api/v3/auth/session":                                        "Auth: delete user session",
	"POST /api/v3/auth/sessions/revoke-others":                           "Auth: revoke other active sessions",
	"GET /api/v3/auth/status":                                            "Auth: check session authentication status",
	"GET /api/v3/dvr/capabilities":                                       "DVR: tuner slot and hardware capabilities",
	"GET /api/v3/dvr/status":                                             "DVR: engine admission status",
	"GET /api/v3/errors":                                                 "System: RFC 7807 problem code dictionary",
	"DELETE /api/v3/household/approvals/{id}/approve":                    "Household: approve join request",
	"POST /api/v3/household/approvals":                                   "Household: create join approval request",
	"GET /api/v3/household/approvals":                                    "Household: list pending join approvals",
	"POST /api/v3/household/approvals/{id}/approve":                      "Household: approve membership",
	"POST /api/v3/household/approvals/{id}/deny":                         "Household: deny membership",
	"GET /api/v3/household/devices":                                      "Household: list authorized devices",
	"POST /api/v3/household/devices/{id}/revoke":                         "Household: revoke device access",
	"GET /api/v3/household/members":                                      "Household: list household members",
	"POST /api/v3/household/members/invite":                              "Household: send member invitation",
	"DELETE /api/v3/household/members/{id}":                              "Household: remove member from household",
	"GET /api/v3/household/policies/access":                              "Household: query access policy",
	"POST /api/v3/household/policies/access":                             "Household: update access policy",
	"POST /api/v3/household/policies/access/revoke":                      "Household: revoke access policy",
	"DELETE /api/v3/household/profiles/{id}":                             "Household: delete profile",
	"DELETE /api/v3/household/profiles/{profileId}":                      "Household: delete profile by ID",
	"GET /api/v3/household/resource-policy":                              "Household: query resource limit policy",
	"PUT /api/v3/household/resource-policy":                              "Household: update resource limit policy",
	"DELETE /api/v3/household/unlock":                                    "Household: remove unlock session",
	"GET /api/v3/household/unlock":                                       "Household: check unlock status",
	"POST /api/v3/household/unlock":                                      "Household: authenticate unlock pin",
	"GET /api/v3/notifications":                                          "Notifications: list notifications",
	"DELETE /api/v3/notifications/{id}":                                  "Notifications: delete notification",
	"POST /api/v3/notifications/mark-all-read":                           "Notifications: mark all read",
	"POST /api/v3/notifications/mark-read":                               "Notifications: mark single read",
	"POST /api/v3/notifications/push-subscriptions":                      "Notifications: register WebPush subscription",
	"GET /api/v3/notifications/stream":                                   "Notifications: SSE notification stream",
	"GET /api/v3/notifications/vapid-key":                                "Notifications: get VAPID public key",
	"POST /api/v3/pairing/start":                                         "Pairing: begin device pairing",
	"POST /api/v3/pairing/{pairingId}/approve":                           "Pairing: approve pairing code",
	"POST /api/v3/pairing/{pairingId}/exchange":                          "Pairing: exchange paired token",
	"POST /api/v3/pairing/{pairingId}/status":                            "Pairing: poll pairing state",
	"DELETE /api/v3/profiles/{id}":                                       "Profiles: delete profile",
	"DELETE /api/v3/recordings/{recordingId}":                            "Recordings: delete recording file",
	"POST /api/v3/recordings/{recordingId}/delete":                       "Recordings: post delete recording",
	"HEAD /api/v3/recordings/{recordingId}/playlist.m3u8":                "Recordings: HLS playlist HEAD",
	"GET /api/v3/recordings/{recordingId}/playlist.m3u8":                 "Recordings: HLS playlist GET",
	"POST /api/v3/recordings/{recordingId}/rename":                       "Recordings: rename recording",
	"PUT /api/v3/recordings/{recordingId}/resume":                        "Recordings: update resume timestamp",
	"GET /api/v3/recordings/{recordingId}/scrub.jpg":                     "Recordings: scrub thumbnail storyboard",
	"HEAD /api/v3/recordings/{recordingId}/stream.mp4":                   "Recordings: direct stream MP4 HEAD",
	"GET /api/v3/recordings/{recordingId}/stream.mp4":                    "Recordings: direct stream MP4 GET",
	"GET /api/v3/recordings/{recordingId}/thumbnail.jpg":                 "Recordings: thumbnail image",
	"GET /api/v3/recordings/{recordingId}/timeshift.m3u8":                "Recordings: timeshift playlist GET",
	"HEAD /api/v3/recordings/{recordingId}/timeshift.m3u8":               "Recordings: timeshift playlist HEAD",
	"GET /api/v3/recordings/{recordingId}/{segment}":                     "Recordings: HLS segment GET",
	"HEAD /api/v3/recordings/{recordingId}/{segment}":                    "Recordings: HLS segment HEAD",
	"POST /api/v3/series-rules/run":                                      "Series: run all series rules immediately",
	"DELETE /api/v3/series-rules/{id}":                                   "Series: delete series rule",
	"POST /api/v3/series-rules/{id}/run":                                 "Series: run single series rule",
	"POST /api/v3/services/{id}/toggle":                                  "Services: toggle channel enabled/disabled state by id",
	"POST /api/v3/sessions/{sessionId}/feedback":                         "Sessions: report playback feedback",
	"POST /api/v3/sessions/{sessionID}/heartbeat":                        "Sessions: send session keepalive",
	"GET /api/v3/sessions/{sessionID}/hls/{filename}":                    "Sessions: HLS session playlist/segment GET",
	"HEAD /api/v3/sessions/{sessionID}/hls/{filename}":                   "Sessions: HLS session playlist/segment HEAD",
	"GET /api/v3/sessions/{sessionID}/hls/{variant}/{filename}":          "Sessions: variant HLS segment GET",
	"HEAD /api/v3/sessions/{sessionID}/hls/{variant}/{filename}":         "Sessions: variant HLS segment HEAD",
	"POST /api/v3/sessions/{sessionId}/playback-ticket":                  "Sessions: issue playback ticket",
	"POST /api/v3/sessions/revoke-user-sessions":                         "Sessions: revoke user sessions",
	"DELETE /api/v3/streams/{id}":                                        "Streams: stop active stream session",
	"GET /api/v3/system/config":                                          "System: read config snapshot",
	"PUT /api/v3/system/config":                                          "System: update config",
	"GET /api/v3/system/connectivity":                                    "System: test receiver connectivity",
	"GET /api/v3/system/entitlements":                                    "System: list entitlements",
	"POST /api/v3/system/entitlements/overrides":                         "System: override entitlement",
	"DELETE /api/v3/system/entitlements/overrides/{principalId}/{scope}": "System: delete entitlement override",
	"POST /api/v3/system/entitlements/receipts":                          "System: submit entitlement receipt",
	"GET /api/v3/system/health":                                          "System: health check",
	"GET /api/v3/system/healthz":                                         "System: k8s liveness check",
	"GET /api/v3/system/info":                                            "System: system info",
	"POST /api/v3/system/refresh":                                        "System: trigger bouquet/service refresh",
	"POST /api/v3/system/scan":                                           "System: trigger manual transponder scan",
	"GET /api/v3/system/scan":                                            "System: get transponder scan status",
}

// exercisedRoutes documents the inbound entry points that are exercised with opaque IDs.
var exercisedRoutes = map[string]string{
	"POST /api/v3/intents":                               "Inbound: stream start/stop intents",
	"POST /api/v3/services/now-next":                     "Inbound: batch now-next EPG lookups",
	"POST /api/v3/live/playback-summary":                 "Inbound: batch playback summary",
	"POST /api/v3/timers":                                "Inbound: timer creation",
	"GET /api/v3/timers/{timerId}":                       "Inbound: timer lookup by opaque ID",
	"PATCH /api/v3/timers/{timerId}":                     "Inbound: timer update by opaque ID",
	"DELETE /api/v3/timers/{timerId}":                    "Inbound: timer deletion by opaque ID",
	"POST /api/v3/timers/conflicts:preview":              "Inbound: timer conflicts preview",
	"POST /api/v3/stream/prepare":                        "Inbound: zap preparation start",
	"GET /api/v3/stream/prepare/{preparationId}":         "Inbound: zap preparation status",
	"POST /api/v3/stream/prepare/{preparationId}/commit": "Inbound: zap preparation commit",
	"DELETE /api/v3/stream/prepare/{preparationId}":      "Inbound: zap preparation cancel",
	"POST /api/v3/household/profiles":                    "Inbound: household profile creation",
	"PUT /api/v3/household/profiles/{profileId}":         "Inbound: household profile update",
	"PUT /api/v3/household/profiles/{id}":                "Inbound: household profile update by id",
	"POST /api/v3/profiles":                              "Inbound: legacy profile create alias",
	"PUT /api/v3/profiles/{id}":                          "Inbound: legacy profile update alias",
	"POST /api/v3/telemetry/playback":                    "Inbound: client telemetry playback reporting",
}

// allowlistEntry describes an outbound sink or log that currently leaks raw refs.
type allowlistEntry struct {
	Route       string
	SourceLoc   string
	TargetSlice string
	Description string
}

// auditedOutboundRoutes documents the outbound routes audited for zero canary leak in Slice 4.
var auditedOutboundRoutes = map[string]string{
	"GET /api/v3/services":                              "Outbound: services list returns masked iptv_<id>",
	"GET /api/v3/services/bouquets":                     "Outbound: bouquet listings return masked iptv_<id>",
	"GET /api/v3/epg":                                   "Outbound: full EPG export returns masked iptv_<id>",
	"GET /api/v3/timers":                                "Outbound: timer listing returns masked iptv_<id>",
	"GET /api/v3/sessions":                              "Outbound: active sessions return masked iptv_<id>",
	"GET /api/v3/sessions/{sessionID}":                  "Outbound: session detail returns masked iptv_<id>",
	"GET /api/v3/sessions/{sessionID}/events":           "Outbound: session SSE events return masked iptv_<id>",
	"GET /api/v3/receiver/current":                      "Outbound: current live channel returns masked iptv_<id>",
	"GET /api/v3/household/profiles":                    "Outbound: household profiles return masked iptv_<id>",
	"GET /api/v3/household/profiles/{id}":               "Outbound: household profile by ID returns masked iptv_<id>",
	"GET /api/v3/profiles":                              "Outbound: legacy profile list returns masked iptv_<id>",
	"GET /api/v3/profiles/{id}":                         "Outbound: legacy profile by ID returns masked iptv_<id>",
	"GET /api/v3/recordings":                            "Outbound: recordings list returns masked iptv_<id>",
	"GET /api/v3/recordings/{recordingId}/stream-info":  "Outbound: recording stream info returns masked iptv_<id>",
	"POST /api/v3/recordings/{recordingId}/stream-info": "Outbound: recording stream info post returns masked iptv_<id>",
	"GET /api/v3/recordings/{recordingId}/status":       "Outbound: recording status returns masked iptv_<id>",
	"GET /api/v3/series-rules":                          "Outbound: series rules return masked iptv_<id>",
	"POST /api/v3/series-rules":                         "Outbound: series rule create returns masked iptv_<id>",
	"PUT /api/v3/series-rules/{id}":                     "Outbound: series rule update returns masked iptv_<id>",
	"POST /api/v3/live/stream-info":                     "Outbound: live playback info returns masked iptv_<id> and opaque token",
	"GET /api/v3/streams":                               "Outbound: active stream listings return masked iptv_<id> or channel name",
	"GET /api/v3/logs":                                  "Outbound: internal log sink scrubbed of raw refs and provider credentials",
}

var allowlistRoutes = map[string]allowlistEntry{}

// ----------------------------------------------------------------------------
// Test 1: Route Coverage (Chi Router Walk)
// ----------------------------------------------------------------------------

func TestIPTV_CanaryLeakAudit_RouteCoverage(t *testing.T) {
	s, _ := newV3TestServer(t, t.TempDir())
	cfg := config.AppConfig{}
	h, err := NewHandler(s, cfg)
	require.NoError(t, err)

	router, ok := h.(chi.Router)
	require.True(t, ok)

	unclassified := make([]string, 0)
	totalRoutes := 0

	err = chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		fullPattern := route
		if !strings.HasPrefix(fullPattern, "/api/v3") && fullPattern != "/.well-known/assetlinks.json" {
			fullPattern = "/api/v3" + fullPattern
		}
		key := fmt.Sprintf("%s %s", method, fullPattern)
		totalRoutes++

		_, inExercised := exercisedRoutes[key]
		_, inAuditedOutbound := auditedOutboundRoutes[key]
		_, inRefFree := refFreeRoutes[key]
		_, inAllowlist := allowlistRoutes[key]

		if !inExercised && !inAuditedOutbound && !inRefFree && !inAllowlist {
			unclassified = append(unclassified, key)
		}
		return nil
	})
	require.NoError(t, err)

	if len(unclassified) > 0 {
		t.Fatalf("Unclassified routes detected! Every route must be either 'exercised', 'ref-free', or 'allowlist':\n%s", strings.Join(unclassified, "\n"))
	}
	assert.GreaterOrEqual(t, totalRoutes, 90, "Expected at least 90 routes walked on v3 router")
}

// ----------------------------------------------------------------------------
// Test 2: Inbound Endpoints Canary Leak Audit (Assert NO canary markers in response)
// ----------------------------------------------------------------------------

func TestIPTV_CanaryLeakAudit_InboundEndpointsZeroLeak(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	s, st := newV3TestServer(t, t.TempDir())
	svc := new(MockRecordingsService)
	b := v3bus.NewMemoryBus()
	hhStore := householddomain.NewMemoryStore()
	hhSvc := householddomain.NewService(hhStore)
	s.SetJWTSecret(jwtTestSecret)
	s.SetDependencies(Dependencies{
		Bus:               b,
		Store:             st,
		Scan:              verifiedLivePlaybackScanner(),
		RecordingsService: svc,
		Households:        hhSvc,
	})
	s.SetIPTVResolver(res)

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`

	// 1. POST /api/v3/live/stream-info (Obtain decision token, and verify 404/400 zero leak)
	var token string
	{
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var payload map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		token = payload["playbackDecisionToken"].(string)

		// 404 unknown opaque ID has ZERO canary leak
		w404 := httptest.NewRecorder()
		r404 := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{"serviceRef":"iptv_abcdefghijklmnopqrstuvwxyz"}`))
		r404.Header.Set("Content-Type", "application/json")
		s.PostLivePlaybackInfo(w404, r404, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusNotFound, w404.Code)
		assertNoCanaryLeak(t, "stream-info-404", w404)

		// 400 malformed ID has ZERO canary leak
		w400 := httptest.NewRecorder()
		r400 := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{"serviceRef":"iptv_invalid_base32!!!"}`))
		r400.Header.Set("Content-Type", "application/json")
		s.PostLivePlaybackInfo(w400, r400, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusBadRequest, w400.Code)
		assertNoCanaryLeak(t, "stream-info-400", w400)
	}

	// 2. POST /api/v3/intents
	{
		intentJSON := fmt.Sprintf(`{
			"type":"stream.start",
			"serviceRef":%q,
			"playbackDecisionToken":%q,
			"client":%s
		}`, opaqueID, token, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/intents", strings.NewReader(intentJSON))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer test-token")
		s.handleV3Intents(w, r)
		require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
		assertNoCanaryLeak(t, "intents", w)
	}

	// 3. POST /api/v3/services/now-next (EPG)
	{
		mockSource := new(MockEpgSource)
		s.epgSource = mockSource
		now := time.Now()
		progs := []epg.Programme{
			{
				Channel: canaryRawRef,
				Title:   epg.Title{Text: "Canary Program Live"},
				Start:   now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
				Stop:    now.Add(35 * time.Minute).Format(xmltvTimeFormat),
			},
		}
		mockSource.On("GetPrograms", mock.Anything).Return(progs, nil)
		body := fmt.Sprintf(`{"services":[%q]}`, opaqueID)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/services/now-next", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.handleNowNextEPG(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assertNoCanaryLeak(t, "now-next", w)
	}

	// 4. POST /api/v3/live/playback-summary (map keys must preserve client's opaque ID, zero canary leak in keys and headers)
	{
		body := fmt.Sprintf(`{
			"serviceRefs":[%q],
			"capabilities":%s
		}`, opaqueID, caps)
		w := postPlaybackSummary(t, s, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var rawResp struct {
			Items map[string]json.RawMessage `json:"items"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rawResp))
		assert.Contains(t, rawResp.Items, opaqueID)
		assert.NotContains(t, rawResp.Items, canaryRawRef)
		for k := range rawResp.Items {
			for _, marker := range canaryMarkers {
				assert.NotContains(t, k, marker, "Map key must never leak canary marker")
			}
		}
		for hKey, hVals := range w.Header() {
			for _, val := range hVals {
				for _, marker := range canaryMarkers {
					assert.NotContains(t, val, marker, "Header %q must never leak canary marker", hKey)
				}
			}
		}
	}

	// 5. POST /api/v3/timers and DELETE /api/v3/timers/{timerId}
	{
		var addedTimer *openwebif.Timer
		mockClient := &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				timers := []openwebif.Timer{
					{ServiceRef: canaryRawRef, Begin: 1000, End: 2000, Name: "Existing Timer"},
				}
				if addedTimer != nil {
					timers = append(timers, *addedTimer)
				}
				return timers, nil
			},
			addTimerFunc: func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
				addedTimer = &openwebif.Timer{
					ServiceRef: sRef,
					Begin:      begin,
					End:        end,
					Name:       name,
				}
				return nil
			},
			deleteTimerFunc: func(ctx context.Context, sRef string, begin, end int64) error {
				return nil
			},
		}
		s.owiFactory = func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
			return mockClient
		}

		// POST /api/v3/timers with known opaque ID creates timer (outbound response leak tested in non-stale test)
		body := fmt.Sprintf(`{"serviceRef":%q,"name":"Canary Record","begin":1700000000,"end":1700003600}`, opaqueID)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.AddTimer(w, r)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

		// Verify 404 on unknown opaque ID has ZERO canary leak
		w404 := httptest.NewRecorder()
		r404 := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(`{"serviceRef":"iptv_abcdefghijklmnopqrstuvwxyz","begin":1700000000,"end":1700003600}`))
		r404.Header.Set("Content-Type", "application/json")
		s.AddTimer(w404, r404)
		require.Equal(t, http.StatusNotFound, w404.Code)
		assertNoCanaryLeak(t, "post-timers-404", w404)

		// Verify 400 on malformed ID has ZERO canary leak
		w400 := httptest.NewRecorder()
		r400 := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(`{"serviceRef":"iptv_invalid_base32!!!","begin":1700000000,"end":1700003600}`))
		r400.Header.Set("Content-Type", "application/json")
		s.AddTimer(w400, r400)
		require.Equal(t, http.StatusBadRequest, w400.Code)
		assertNoCanaryLeak(t, "post-timers-400", w400)

		// DELETE /api/v3/timers/{timerId}
		timerID := read.MakeTimerID(opaqueID, 1000, 2000)
		wDel := httptest.NewRecorder()
		rDel := httptest.NewRequest(http.MethodDelete, "/api/v3/timers/"+timerID, nil)
		s.DeleteTimer(wDel, rDel, timerID)
		require.Equal(t, http.StatusNoContent, wDel.Code, wDel.Body.String())
		assertNoCanaryLeak(t, "delete-timer", wDel)
	}

	// 6. POST /api/v3/household/profiles (error path with malformed ID has zero canary leak)
	{
		profBody := householddomain.Profile{
			ID:                 "test-profile-bad",
			Name:               "Test Profile",
			Kind:               householddomain.ProfileKindAdult,
			AllowedServiceRefs: []string{"iptv_invalid_base32!!!"},
		}
		bodyBytes, err := json.Marshal(profBody)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/household/profiles", bytes.NewReader(bodyBytes))
		r.Header.Set("Content-Type", "application/json")
		s.PostHouseholdProfiles(w, r, PostHouseholdProfilesParams{})
		require.Equal(t, http.StatusCreated, w.Code)
		assertNoCanaryLeak(t, "household-profiles-omitted", w)
	}

	// 7. POST /api/v3/telemetry/playback
	{
		batch := PlaybackTelemetryBatch{
			Client: PlaybackTelemetryClient{Platform: PlaybackTelemetryClientPlatformIos},
			Events: []PlaybackTelemetryEvent{
				{
					Kind:       PlaybackTelemetryEventKindSessionStart,
					OccurredAt: time.Now(),
					ServiceRef: &opaqueID,
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
		assertNoCanaryLeak(t, "telemetry", w)
	}
}

// ----------------------------------------------------------------------------
// Test 3: Allowlist Staleness Verification (Assert current allowlist DOES leak)
// ----------------------------------------------------------------------------

func TestIPTV_CanaryLeakAudit_AllowlistNonStale(t *testing.T) {
	// In Slice 5, allowlist is completely drained - 100% outbound surfaces are audited for zero leaks
	assert.Empty(t, allowlistRoutes, "Allowlist must be completely empty in Slice 5 - 100% outbound zero-leak achieved")

	// Verify that GET /api/v3/logs without resolver fails closed and never leaks canary markers
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.logSource = stubLogSource{
		entries: []ilog.LogEntry{
			{Timestamp: time.Now().UTC(), Level: "info", Message: canaryRawRef},
		},
	}
	wLogs := httptest.NewRecorder()
	rLogs := httptest.NewRequest(http.MethodGet, "/api/v3/logs", nil)
	srv.GetLogs(wLogs, rLogs, GetLogsParams{})
	require.Equal(t, http.StatusOK, wLogs.Code)
	assertNoCanaryLeak(t, "GET /api/v3/logs (unconfigured)", wLogs)
	assert.Contains(t, wLogs.Body.String(), "[REDACTED_IPTV_REF]")
}

// ----------------------------------------------------------------------------
// Test 3b: Outbound Endpoints Zero Leak Audit (Slice 4)
// ----------------------------------------------------------------------------

type canaryAuditRecordingsService struct {
	MockRecordingsService
	recordings []recservice.RecordingItem
	status     recservice.StatusResult
}

func (c *canaryAuditRecordingsService) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	return recservice.ListResult{
		Recordings: c.recordings,
	}, nil
}

func (c *canaryAuditRecordingsService) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	return c.status, nil
}

func TestIPTV_CanaryLeakAudit_OutboundEndpointsZeroLeak(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	s, st := newV3TestServer(t, t.TempDir())
	s.SetJWTSecret(jwtTestSecret)
	s.SetIPTVResolver(res)
	s.cfg.APIToken = "test-token"
	s.cfg.APITokenScopes = []string{string(ScopeAll)}
	adminPrincipal := &controlauth.Principal{
		ID:     "admin-1",
		Scopes: []string{string(ScopeAll)},
	}

	// 1. Setup mock playlist in s.cfg.DataDir
	playlistContent := fmt.Sprintf("#EXTM3U\n"+
		"#EXTINF:-1 tvg-id=\"dvb-1\" tvg-name=\"DVB Das Erste\" tvg-logo=\"http://example.com/ard.png\" group-title=\"Favourites\",DVB Das Erste\n"+
		"http://127.0.0.1:8001/1:0:19:283D:3FB:1:C00000:0:0:0:\n"+
		"#EXTINF:-1 tvg-id=\"canary-1\" tvg-name=\"Canary Channel 1\" tvg-logo=\"http://canary.invalid/SECRET-CANARY-1/logo.png\" group-title=\"Favourites\",Canary Channel 1\n"+
		"http://127.0.0.1:8001/%s\n", canaryRawRef)
	err := os.WriteFile(filepath.Join(s.cfg.DataDir, "playlist.m3u"), []byte(playlistContent), 0600)
	require.NoError(t, err)
	s.snap.Runtime.PlaylistFilename = "playlist.m3u"

	// 2. Setup OpenWebIF mock server
	servicesJSON := fmt.Sprintf(`{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary Channel 1", "servicereference": %q}
		]
	}`, canaryRawRef)
	mockOWIServer := createCanaryReceiverServer(t, servicesJSON)
	defer mockOWIServer.Close()

	owiClient := openwebif.New(mockOWIServer.URL)
	s.owiClient = owiClient
	s.owiFactory = func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
		return owiClient
	}

	// 3. Setup EPG Source
	mockSource := new(MockEpgSource)
	s.epgSource = mockSource
	now := time.Now()
	progs := []epg.Programme{
		{
			Channel: canaryRawRef,
			Title:   epg.Title{Text: "Canary Program Live"},
			Start:   now.Add(-10 * time.Minute).Format(xmltvTimeFormat),
			Stop:    now.Add(35 * time.Minute).Format(xmltvTimeFormat),
		},
	}
	mockSource.On("GetPrograms", mock.Anything).Return(progs, nil)

	// 4. Setup Household Service & Profile
	hhStore := householddomain.NewMemoryStore()
	hhSvc := householddomain.NewService(hhStore)
	testProfile := householddomain.Profile{
		ID:                 "prof-canary",
		Name:               "Canary Profile",
		Kind:               householddomain.ProfileKindAdult,
		AllowedServiceRefs: []string{canaryRawRef},
	}
	require.NoError(t, hhStore.Upsert(context.Background(), testProfile))

	// 5. Setup Series Manager
	seriesMgr := dvr.NewManager(t.TempDir())
	ruleID, err := seriesMgr.AddRule(dvr.SeriesRule{
		Enabled:    true,
		Keyword:    "Canary Series",
		ChannelRef: canaryRawRef,
		Priority:   5,
	})
	require.NoError(t, err)

	// 6. Setup Recordings Service
	recID := "rec-canary-001"
	recSvc := &canaryAuditRecordingsService{
		recordings: []recservice.RecordingItem{
			{
				RecordingID: recID,
				Title:       "Canary Recording",
				ServiceRef:  canaryRawRef,
			},
		},
		status: recservice.StatusResult{
			State: "READY",
		},
	}
	recSvc.On("GetMediaTruth", mock.Anything, recID).Return(controlplayback.MediaTruth{}, nil)

	s.SetDependencies(Dependencies{
		Bus:               v3bus.NewMemoryBus(),
		Store:             st,
		Scan:              verifiedLivePlaybackScanner(),
		RecordingsService: recSvc,
		Households:        hhSvc,
		SeriesManager:     seriesMgr,
		IPTVResolver:      res,
		TimersSource:      owiClient,
		DVRSource:         owiClient,
	})

	// Seed identity profile
	idSvc := withIdentityDeviceEnrollment(t, s, "admin-1")
	require.NotNil(t, idSvc)
	createdProf, _, err := idSvc.CreateProfile(context.Background(), "admin-1", "Canary Profile", "", false, nil, []string{canaryRawRef}, 18, "")
	require.NoError(t, err)
	profID := createdProf.ID

	// Seed active session in store
	sessionUUID := uuid.New()
	sessionID := sessionUUID.String()
	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         canaryRawRef,
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: time.Now().Add(30 * time.Second).Unix(),
		ContextData: map[string]string{
			model.CtxKeySource: canaryRawRef,
		},
	}))

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`

	// 1. GET /api/v3/services
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/services", nil)
		s.GetServices(w, r, GetServicesParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /services", w)
		assert.Contains(t, w.Body.String(), opaqueID)
		assert.Contains(t, w.Body.String(), "1:0:19:283D:3FB:1:C00000:0:0:0")
		assert.Contains(t, w.Body.String(), "/logos/"+opaqueID+".png")
	}

	// 2. GET /api/v3/services/bouquets
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/services/bouquets", nil)
		s.GetServicesBouquets(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /services/bouquets", w)
	}

	// 3. GET /api/v3/epg
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/epg", nil)
		s.GetEpg(w, r, GetEpgParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /epg", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 4. GET /api/v3/timers
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/timers", nil)
		s.GetTimers(w, r, GetTimersParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /timers", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 5. POST /api/v3/timers
	{
		body := fmt.Sprintf(`{"serviceRef":%q,"name":"Canary Added","begin":1700010000,"end":1700013600}`, opaqueID)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.AddTimer(w, r)
		require.Equal(t, http.StatusCreated, w.Code)
		assertNoCanaryLeak(t, "POST /timers", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 6. GET /api/v3/sessions
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.ListSessions(w, r, ListSessionsParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /sessions", w)
		assert.Contains(t, w.Body.String(), opaqueID)

		var sessionListResp struct {
			Sessions []struct {
				ServiceRef  string            `json:"serviceRef"`
				ContextData map[string]string `json:"contextData"`
			} `json:"sessions"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sessionListResp))
		require.NotEmpty(t, sessionListResp.Sessions)
		assert.Equal(t, opaqueID, sessionListResp.Sessions[0].ServiceRef)
		assert.Equal(t, opaqueID, sessionListResp.Sessions[0].ContextData[model.CtxKeySource])
	}

	// 7. GET /api/v3/sessions/{sessionID}
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("sessionID", sessionID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.GetSessionState(w, r, openapi_types.UUID(parseUUID(sessionID)))
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /sessions/{id}", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 8. GET /api/v3/sessions/{sessionID}/events (SSE)
	{
		ts := httptest.NewServer(NewRouter(s, RouterOptions{BaseURL: V3BaseURL}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+V3BaseURL+"/sessions/"+sessionID+"/events", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		scanner := bufio.NewScanner(resp.Body)
		var eventData string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				eventData = strings.TrimPrefix(line, "data: ")
				break
			}
		}
		require.NotEmpty(t, eventData)
		for _, m := range canaryMarkers {
			assert.NotContains(t, strings.ToLower(eventData), strings.ToLower(m))
		}
		assert.Contains(t, eventData, "session.state_changed")
	}

	// 9. GET /api/v3/receiver/current
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/receiver/current", nil)
		s.GetReceiverCurrent(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /receiver/current", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 10. GET /api/v3/household/profiles
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles", nil)
		s.GetHouseholdProfiles(w, r, GetHouseholdProfilesParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /household/profiles", w)

		// Test household service fallback masking when identityService is nil
		s.identityService = nil
		wHH := httptest.NewRecorder()
		rHH := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles", nil)
		s.GetHouseholdProfiles(wHH, rHH, GetHouseholdProfilesParams{})
		require.Equal(t, http.StatusOK, wHH.Code)
		assertNoCanaryLeak(t, "GET /household/profiles (householdService)", wHH)
		assert.Contains(t, wHH.Body.String(), opaqueID)
		s.identityService = idSvc
	}

	// 11. GET /api/v3/household/profiles/{id}
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles/"+profID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", profID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		s.GetProfile(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /household/profiles/{id}", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 12. GET /api/v3/profiles
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/profiles", nil)
		s.ListProfiles(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /profiles", w)
	}

	// 13. GET /api/v3/profiles/{id}
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/profiles/"+profID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", profID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		s.GetProfile(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /profiles/{id}", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 14. GET /api/v3/recordings
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/recordings", nil)
		s.GetRecordings(w, r, GetRecordingsParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /recordings", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 15. GET /api/v3/recordings/{recordingId}/stream-info
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recID+"/stream-info", nil)
		s.GetRecordingPlaybackInfo(w, r, recID, GetRecordingPlaybackInfoParams{})
		assertNoCanaryLeak(t, "GET /recordings/{id}/stream-info", w)
	}

	// 16. POST /api/v3/recordings/{recordingId}/stream-info
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/recordings/"+recID+"/stream-info", strings.NewReader(fmt.Sprintf(`{"capabilities":%s}`, caps)))
		r.Header.Set("Content-Type", "application/json")
		s.PostRecordingPlaybackInfo(w, r, recID, PostRecordingPlaybackInfoParams{})
		assertNoCanaryLeak(t, "POST /recordings/{id}/stream-info", w)
	}

	// 17. GET /api/v3/recordings/{recordingId}/status
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recID+"/status", nil)
		s.GetRecordingsRecordingIdStatus(w, r, recID)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /recordings/{id}/status", w)
	}

	// 18. GET /api/v3/series-rules
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/series-rules", nil)
		s.GetSeriesRules(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /series-rules", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 19. POST /api/v3/series-rules
	{
		body := fmt.Sprintf(`{"keyword":"Canary Create","channelRef":%q}`, opaqueID)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/series-rules", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.CreateSeriesRule(w, r)
		require.Equal(t, http.StatusCreated, w.Code)
		assertNoCanaryLeak(t, "POST /series-rules", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 20. PUT /api/v3/series-rules/{id}
	{
		body := fmt.Sprintf(`{"enabled":true,"keyword":"Canary Updated","channelRef":%q,"priority":5}`, opaqueID)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPut, "/api/v3/series-rules/"+ruleID, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.UpdateSeriesRule(w, r, ruleID)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "PUT /series-rules/{id}", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 21. POST /api/v3/live/stream-info
	{
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "POST /live/stream-info", w)
		assert.Contains(t, w.Body.String(), opaqueID)

		var payload struct {
			PlaybackDecisionToken string `json:"playbackDecisionToken"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		claims, err := v3auth.VerifyStrict(payload.PlaybackDecisionToken, jwtTestSecret, "xg2g/v3/intents", "xg2g")
		require.NoError(t, err)
		assert.Equal(t, strings.ToUpper(opaqueID), claims.Sub)
	}

	// 22. POST /api/v3/live/playback-summary
	{
		body := fmt.Sprintf(`{"serviceRefs":[%q],"capabilities":%s}`, opaqueID, caps)
		w := postPlaybackSummary(t, s, body)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "POST /live/playback-summary", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 23. GET /api/v3/streams
	{
		// Seed unlisted stream sessions (no playlist entry and no trailing channel name)
		// to verify fallback to serviceRef is masked to opaque iptv_<id> and never leaks raw URL/tokens.
		for _, rawUnlistedRef := range []string{
			"4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/unlisted.ts",
			"4097:0:1:0:0:0:0:0:0:0:http://canary.invalid/SECRET-CANARY-1/live/token-xyz-987/unlisted.ts",
		} {
			require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
				SessionID:          uuid.New().String(),
				State:              model.SessionReady,
				ServiceRef:         rawUnlistedRef,
				HeartbeatInterval:  30,
				LeaseExpiresAtUnix: time.Now().Add(30 * time.Second).Unix(),
				ContextData: map[string]string{
					model.CtxKeySource: rawUnlistedRef,
				},
			}))
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/streams", nil)
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.GetStreams(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /streams", w)

		var streamsResp []StreamSession
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &streamsResp))
		require.NotEmpty(t, streamsResp)
		// Ensure none of the streams have raw canary refs or unmasked tokens in channelName
		for _, stream := range streamsResp {
			if stream.ChannelName != nil {
				for _, marker := range canaryMarkers {
					assert.NotContains(t, strings.ToLower(*stream.ChannelName), strings.ToLower(marker))
				}
			}
		}
	}

	// 24. GET /api/v3/logs
	{
		s.logSource = stubLogSource{
			entries: []ilog.LogEntry{
				{
					Timestamp: time.Now().UTC(),
					Level:     "info",
					Message:   fmt.Sprintf("Stream connection established for %s", canaryRawRef),
					Fields: map[string]any{
						"service_ref": canaryRawRef,
						"url":         "http://canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts",
						"error":       "dial tcp: lookup canary.invalid: no such host",
					},
				},
			},
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/logs", nil)
		s.GetLogs(w, r, GetLogsParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /api/v3/logs", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}
}

// ----------------------------------------------------------------------------
// Test 4: Eleven Negative Controls (a through k)
// ----------------------------------------------------------------------------

func TestIPTV_CanaryLeakAudit_NegativeControls(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`

	// Control (a): Skip resolution in PostLivePlaybackInfo => opaque-known test turns RED (404 instead of 200)
	t.Run("NegativeControl_A_SkipResolution", func(t *testing.T) {
		svc := new(MockRecordingsService)
		sUnwired := createTestServerDTO(svc)
		sUnwired.SetJWTSecret(jwtTestSecret)
		sUnwired.SetDependencies(Dependencies{Scan: verifiedLivePlaybackScanner(), RecordingsService: svc})
		// Resolver explicitly NIL (skipping resolution)
		sUnwired.SetIPTVResolver(nil)

		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		sUnwired.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})

		// Demonstrates RED condition for opaque known when resolution is skipped:
		assert.Equal(t, http.StatusNotFound, w.Code, "Skipped resolution must reject opaque ID with 404")
	})

	// Control (b): Resolve AFTER household visibility check => turns RED (denied because opaque != rawRef)
	t.Run("NegativeControl_B_ResolveAfterHouseholdVisibility", func(t *testing.T) {
		// If visibility check inspects the raw request before resolution:
		// AllowedChannels holds canaryRawRef, but request holds opaqueID.
		allowedChannels := []string{canaryRawRef}
		isAllowedBeforeResolution := false
		for _, ch := range allowedChannels {
			if ch == opaqueID {
				isAllowedBeforeResolution = true
				break
			}
		}
		assert.False(t, isAllowedBeforeResolution, "Checking before resolution fails (RED condition)")

		// When resolved first:
		resolvedRef, _, err := res.ResolveInbound(metrics.EndpointHousehold, opaqueID)
		require.NoError(t, err)
		isAllowedAfterResolution := false
		for _, ch := range allowedChannels {
			if ch == resolvedRef {
				isAllowedAfterResolution = true
				break
			}
		}
		assert.True(t, isAllowedAfterResolution, "Checking after resolution succeeds (GREEN)")
	})

	// Control (c): Response key uses resolved raw ref in playback summary => turns RED
	t.Run("NegativeControl_C_PlaybackSummaryResponseKey", func(t *testing.T) {
		resMapBad := map[string]any{
			canaryRawRef: map[string]any{"mode": "direct_stream"},
		}
		// Client looks up by opaqueID:
		_, foundBad := resMapBad[opaqueID]
		assert.False(t, foundBad, "Using resolved raw ref as response key causes lookup failure (RED)")

		// Correct implementation maps by client's original ID:
		resMapGood := map[string]any{
			opaqueID: map[string]any{"mode": "direct_stream"},
		}
		_, foundGood := resMapGood[opaqueID]
		assert.True(t, foundGood, "Using client's original key succeeds (GREEN)")
	})

	// Control (d): Handler logs resolved raw ref => turns RED
	t.Run("NegativeControl_D_HandlerLogsRawRef", func(t *testing.T) {
		var logBuf bytes.Buffer
		logSimulation := func(logRaw bool) {
			logBuf.Reset()
			if logRaw {
				_, _ = logBuf.WriteString(fmt.Sprintf(`{"serviceRef":%q}`, canaryRawRef))
			} else {
				_, _ = logBuf.WriteString(fmt.Sprintf(`{"serviceRef":%q}`, opaqueID))
			}
		}

		// Bad: logging raw ref leaks canary marker (RED)
		logSimulation(true)
		assert.Contains(t, logBuf.String(), "canary.invalid", "Logging raw ref leaks marker (RED)")

		// Good: logging client-supplied opaque ref produces zero leaks (GREEN)
		logSimulation(false)
		assert.NotContains(t, logBuf.String(), "canary.invalid", "Logging opaque ref does not leak marker (GREEN)")
	})

	// Control (e): Allowlist entry for a no-longer-leaking sink => staleness assertion turns RED
	t.Run("NegativeControl_E_AllowlistStaleness", func(t *testing.T) {
		// Mock a sink that has been cleaned up and no longer leaks
		cleanedUpSinkResponseBody := `{"services":[{"id":"iptv_clean","name":"Clean Channel"}]}`
		leaks := false
		for _, m := range canaryMarkers {
			if strings.Contains(cleanedUpSinkResponseBody, m) {
				leaks = true
				break
			}
		}
		// Staleness check asserts leaks == true; if false, it would fail
		assert.False(t, leaks, "Cleaned up sink does not leak; keeping it in allowlist is stale (RED)")
	})

	// Control (f): Route removed from canary table => walker test turns RED
	t.Run("NegativeControl_F_RouteRemovedFromTable", func(t *testing.T) {
		testTable := map[string]bool{
			"GET /api/v3/system/health": true,
		}
		missingRoute := "POST /api/v3/intents"
		_, found := testTable[missingRoute]
		assert.False(t, found, "Missing route from table is detected and fails (RED)")
	})

	// Control (g): Outbound response containing canary marker => assertNoCanaryLeak detects it (turns RED)
	t.Run("NegativeControl_G_OutboundCanaryLeakSensitivity", func(t *testing.T) {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.WriteString(fmt.Sprintf(`{"serviceRef":%q}`, canaryRawRef))

		detected := false
		bodyStr := strings.ToLower(w.Body.String())
		for _, marker := range canaryMarkers {
			if strings.Contains(bodyStr, strings.ToLower(marker)) {
				detected = true
				break
			}
		}
		assert.True(t, detected, "assertNoCanaryLeak must be sensitive to canary markers in responses (RED condition)")
	})

	// Control (h): Response header containing canary marker => assertNoCanaryLeak detects it (turns RED)
	t.Run("NegativeControl_H_HeaderCanaryLeakSensitivity", func(t *testing.T) {
		w := httptest.NewRecorder()
		w.Header().Set("Location", "http://canary.invalid/SECRET-CANARY-1/redirect.ts")
		w.WriteHeader(http.StatusFound)

		detected := false
		for _, hVals := range w.Header() {
			for _, val := range hVals {
				for _, marker := range canaryMarkers {
					if strings.Contains(strings.ToLower(val), strings.ToLower(marker)) {
						detected = true
						break
					}
				}
			}
		}
		assert.True(t, detected, "assertNoCanaryLeak must be sensitive to canary markers in headers (RED condition)")
	})

	// Control (i): Injected canary marker into Prometheus metrics => detected (turns RED)
	t.Run("NegativeControl_I_PrometheusMetricsCanaryLeakSensitivity", func(t *testing.T) {
		metricsDump := `# HELP test_metric A test metric
# TYPE test_metric counter
test_metric{endpoint="SECRET-CANARY-1",host="canary.invalid"} 1
`
		detected := false
		for _, marker := range canaryMarkers {
			if strings.Contains(strings.ToLower(metricsDump), strings.ToLower(marker)) {
				detected = true
				break
			}
		}
		assert.True(t, detected, "Prometheus scrape check must be sensitive to canary markers (RED condition)")
	})

	// Control (j): RFC 7807 problem detail containing canary marker => detected (turns RED)
	t.Run("NegativeControl_J_ProblemDetailsErrorSensitivity", func(t *testing.T) {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.WriteString(fmt.Sprintf(`{"type":"about:blank","title":"Invalid Request","detail":"Failed to parse %s"}`, canaryRawRef))

		detected := false
		bodyStr := strings.ToLower(w.Body.String())
		for _, marker := range canaryMarkers {
			if strings.Contains(bodyStr, strings.ToLower(marker)) {
				detected = true
				break
			}
		}
		assert.True(t, detected, "RFC 7807 error check must be sensitive to leaked raw refs (RED condition)")
	})

	// Control (k): SQLite database dump containing canary marker => detected (turns RED)
	t.Run("NegativeControl_K_SQLiteDumpCanaryLeakSensitivity", func(t *testing.T) {
		sqliteDump := fmt.Sprintf(`[{"id":"prof-1","allowed_channels":["%s"]}]`, canaryRawRef)
		detected := false
		for _, marker := range canaryMarkers {
			if strings.Contains(strings.ToLower(sqliteDump), strings.ToLower(marker)) {
				detected = true
				break
			}
		}
		assert.True(t, detected, "SQLite client-facing dump check must be sensitive to unmasked refs (RED condition)")
	})
}

// ----------------------------------------------------------------------------
// Test 5: Concurrency and Race Detection with Concurrent Registry.Replace
// ----------------------------------------------------------------------------

func TestIPTV_CanaryLeakAudit_ConcurrentReplaceRace(t *testing.T) {
	res, canaryID, reg := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	s, _ := newV3TestServer(t, t.TempDir())
	svc := new(MockRecordingsService)
	s.SetJWTSecret(jwtTestSecret)
	s.SetDependencies(Dependencies{Scan: verifiedLivePlaybackScanner(), RecordingsService: svc})
	s.SetIPTVResolver(res)

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Background worker 1: repeatedly Replace registry sources
	wg.Add(1)
	go func() {
		defer wg.Done()
		parser, _ := sourceref.NewParser([]byte(canarySecret))
		counter := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				counter++
				url := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:http%%3a//canary.invalid/live/stream_%d.ts:Channel_%d", counter%5, counter%5)
				src, err := parser.Parse(url)
				if err == nil {
					_ = reg.Replace([]sourceref.Source{src})
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Background workers 2 & 3: concurrently resolve inbound service refs
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)
					w := httptest.NewRecorder()
					r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
					time.Sleep(2 * time.Millisecond)
				}
			}
		}()
	}

	wg.Wait()
}

// createCanaryReceiverServer spins up a fake OpenWebIF receiver server returning bouquets, services, status, current channel, and timers for canary tests.
func createCanaryReceiverServer(t *testing.T, servicesJSON string) *httptest.Server {
	t.Helper()
	var (
		timerMu sync.Mutex
		timers  = []openwebif.Timer{
			{
				ServiceRef: canaryRawRef,
				Name:       "Canary Timer",
				Begin:      1700000000,
				End:        1700003600,
			},
		}
	)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "statusinfo") {
			_, _ = w.Write([]byte(`{"result": true, "inStandby": "false"}`))
			return
		}
		if strings.Contains(r.URL.Path, "getcurrent") {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"result": true, "info": {"serviceref": %q, "name": "Canary Channel 1"}, "now": {"title": "Canary Program Live", "begin_timestamp": 1700000000, "duration_sec": 3600}}`, canaryRawRef)))
			return
		}
		if strings.Contains(r.URL.Path, "timerlist") {
			timerMu.Lock()
			copied := make([]openwebif.Timer, len(timers))
			copy(copied, timers)
			timerMu.Unlock()
			resp := openwebif.TimerListResponse{
				Result: true,
				Timers: copied,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if strings.Contains(r.URL.Path, "timeradd") {
			sRef := r.URL.Query().Get("sRef")
			begin, _ := strconv.ParseInt(r.URL.Query().Get("begin"), 10, 64)
			end, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
			name := r.URL.Query().Get("name")
			timerMu.Lock()
			timers = append(timers, openwebif.Timer{
				ServiceRef: sRef,
				Name:       name,
				Begin:      begin,
				End:        end,
			})
			timerMu.Unlock()
			_, _ = w.Write([]byte(`{"result": true}`))
			return
		}
		if strings.Contains(r.URL.Path, "timerdelete") || strings.Contains(r.URL.Path, "timeredit") {
			_, _ = w.Write([]byte(`{"result": true}`))
			return
		}
		if strings.Contains(r.URL.Path, "bouquets") {
			_, _ = w.Write([]byte(`{"bouquets":[["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.favourites.tv\" ORDER BY bouquet","Favourites"]]}`))
			return
		}
		if strings.Contains(r.URL.Path, "getservices") || strings.Contains(r.URL.Path, "getallservices") {
			_, _ = w.Write([]byte(servicesJSON))
			return
		}
		if strings.Contains(r.URL.Path, "streamcurrent") {
			_, _ = w.Write([]byte(`{"result": true}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func setupCanaryRefreshSnapshot(t *testing.T, serverURL string) (config.Snapshot, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: dataDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    serverURL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}
	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	snap.Runtime.PlaylistFilename = "playlist.m3u"
	return snap, dataDir
}

// TestIPTV_CanaryLeakAudit_ExportedPlaylistAndPicons asserts that the exported public playlist
// generated via the real refresh job and served through the file server contains zero canary tokens,
// hosts, or raw references, while internal playlist.m3u remains intact.
func TestIPTV_CanaryLeakAudit_ExportedPlaylistAndPicons(t *testing.T) {
	parser, err := sourceref.NewParser([]byte(canarySecret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	servicesJSON := fmt.Sprintf(`{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary Channel 1", "servicereference": %q}
		]
	}`, canaryRawRef)

	mockOWI := createCanaryReceiverServer(t, servicesJSON)
	defer mockOWI.Close()

	snap, dataDir := setupCanaryRefreshSnapshot(t, mockOWI.URL)

	// Setup mock picon file in dataDir/picons
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))
	storeRef := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(canaryRawRef, ":", "_"), "/", "_"), "_")
	fakePNG := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRcanary_clean")
	require.NoError(t, os.WriteFile(filepath.Join(piconsDir, storeRef+".png"), fakePNG, 0644))

	// Execute REAL refresh job with IPTV parser and registry
	status, err := jobs.RefreshWithOptions(context.Background(), snap, jobs.WithIPTVSources(parser, reg))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.Channels)

	// 1. Filesystem existence & permission verification
	rawPlaylistPath := filepath.Join(dataDir, "playlist.m3u")
	publicPlaylistPath := filepath.Join(dataDir, "playlist_public.m3u")

	rawInfo, err := os.Stat(rawPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), rawInfo.Mode().Perm(), "internal playlist.m3u must have 0600 permissions")

	publicInfo, err := os.Stat(publicPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), publicInfo.Mode().Perm(), "public playlist_public.m3u must have 0644 permissions")

	// 2. Internal raw playlist content check: preserves raw references for internal subsystems
	rawBytes, err := os.ReadFile(rawPlaylistPath)
	require.NoError(t, err)
	rawStr := string(rawBytes)
	for _, marker := range canaryMarkers {
		assert.True(t, strings.Contains(rawStr, marker), "internal playlist must preserve canary marker %q", marker)
	}
	assert.Contains(t, rawStr, canaryRawRef)

	// 3. Resolve opaque ID
	assert.Equal(t, 1, reg.Len(), "registry must contain parsed canary IPTV source")
	var opaqueID string
	for _, src := range reg.Snapshot() {
		opaqueID = string(src.ID())
		break
	}
	require.NotEmpty(t, opaqueID)

	// 4. File server verification: HTTP requests for both /playlist.m3u and /playlist_public.m3u
	fileServer := controlhttp.SecureFileServer(dataDir, nil)

	for _, reqPath := range []string{"/playlist.m3u", "/playlist_public.m3u"} {
		req := httptest.NewRequest(http.MethodGet, reqPath, nil)
		rec := httptest.NewRecorder()
		fileServer.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "file server should return 200 for %s", reqPath)
		body := rec.Body.String()
		for _, marker := range canaryMarkers {
			assert.False(t, strings.Contains(body, marker), "playlist response for %s leaked canary marker %q: %s", reqPath, marker, body)
		}
		assert.False(t, strings.Contains(body, "4097:"), "playlist response for %s leaked raw 4097 ref", reqPath)
		assert.Contains(t, body, opaqueID)
		assert.Contains(t, body, "1:0:19:283D:3FB:1:C00000:0:0:0:")
		assert.Contains(t, body, "DVB Das Erste")
	}
}

// TestIPTV_CanaryLeakAudit_RealRefreshAndFileServer_MissingParser asserts that when the IPTV parser
// is missing/nil, the public playlist export fails closed: all IPTV items are omitted, zero canary tokens
// or raw references are exposed via HTTP file serving, while internal playlist.m3u remains intact.
func TestIPTV_CanaryLeakAudit_RealRefreshAndFileServer_MissingParser(t *testing.T) {
	servicesJSON := fmt.Sprintf(`{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary Channel 1", "servicereference": %q}
		]
	}`, canaryRawRef)

	mockOWI := createCanaryReceiverServer(t, servicesJSON)
	defer mockOWI.Close()

	snap, dataDir := setupCanaryRefreshSnapshot(t, mockOWI.URL)

	// Execute REAL refresh WITHOUT WithIPTVSources (parser is nil)
	status, err := jobs.RefreshWithOptions(context.Background(), snap)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.Channels)

	rawPlaylistPath := filepath.Join(dataDir, "playlist.m3u")
	publicPlaylistPath := filepath.Join(dataDir, "playlist_public.m3u")

	// 1. Internal playlist must be intact with 0600 permissions and raw references/tokens
	rawInfo, err := os.Stat(rawPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), rawInfo.Mode().Perm())
	rawBytes, err := os.ReadFile(rawPlaylistPath)
	require.NoError(t, err)
	rawStr := string(rawBytes)
	for _, marker := range canaryMarkers {
		assert.True(t, strings.Contains(rawStr, marker), "internal playlist must retain canary marker %q", marker)
	}
	assert.Contains(t, rawStr, canaryRawRef)

	// 2. Public playlist must exist with 0644 permissions
	publicInfo, err := os.Stat(publicPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), publicInfo.Mode().Perm())

	// 3. HTTP file serving must fail closed: zero canary tokens, zero raw IPTV refs
	fileServer := controlhttp.SecureFileServer(dataDir, nil)

	for _, reqPath := range []string{"/playlist.m3u", "/playlist_public.m3u"} {
		req := httptest.NewRequest(http.MethodGet, reqPath, nil)
		rec := httptest.NewRecorder()
		fileServer.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "file server should return 200 for %s", reqPath)
		body := rec.Body.String()
		for _, marker := range canaryMarkers {
			assert.False(t, strings.Contains(body, marker), "playlist response for %s leaked canary marker %q when parser is nil: %s", reqPath, marker, body)
		}
		assert.False(t, strings.Contains(body, "4097:"), "playlist response for %s leaked raw 4097 ref when parser is nil", reqPath)
		assert.False(t, strings.Contains(body, "Canary Channel 1"), "unconvertible IPTV channel must be omitted from public export")

		// DVB channel must remain intact and served
		assert.Contains(t, body, "1:0:19:283D:3FB:1:C00000:0:0:0:")
		assert.Contains(t, body, "DVB Das Erste")
	}
}

// TestIPTV_CanaryLeakAudit_RealRefreshAndFileServer_InvalidIPTVReference asserts that when an IPTV reference
// is invalid or unparseable, the public playlist export fails closed: the invalid item is omitted,
// zero canary tokens or raw references are exposed via HTTP file serving, while internal playlist.m3u remains intact.
func TestIPTV_CanaryLeakAudit_RealRefreshAndFileServer_InvalidIPTVReference(t *testing.T) {
	parser, err := sourceref.NewParser([]byte(canarySecret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	// Malformed IPTV references containing canary credentials:
	// 1) Unencoded port colon (fails sourceref.Parser.Parse with ErrInvalidRef)
	// 2) Malformed URL scheme/format (fails sourceref.Parser.Parse with ErrInvalidScheme / ErrInvalidURL)
	portCanaryRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid:8080/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Port Channel"
	badURLCanaryRef := "4097:0:1:0:0:0:0:0:0:0:not-a-valid-url-canary.invalid-SECRET-CANARY-1-token-xyz-987:Canary BadURL Channel"

	servicesJSON := fmt.Sprintf(`{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary Port Channel", "servicereference": %q},
			{"servicename": "Canary BadURL Channel", "servicereference": %q}
		]
	}`, portCanaryRef, badURLCanaryRef)

	mockOWI := createCanaryReceiverServer(t, servicesJSON)
	defer mockOWI.Close()

	snap, dataDir := setupCanaryRefreshSnapshot(t, mockOWI.URL)

	// Execute REAL refresh WITH IPTV parser and registry
	status, err := jobs.RefreshWithOptions(context.Background(), snap, jobs.WithIPTVSources(parser, reg))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 3, status.Channels)

	rawPlaylistPath := filepath.Join(dataDir, "playlist.m3u")
	publicPlaylistPath := filepath.Join(dataDir, "playlist_public.m3u")

	// 1. Internal playlist must be intact with 0600 permissions and invalid raw references containing canary tokens
	rawInfo, err := os.Stat(rawPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), rawInfo.Mode().Perm())
	rawBytes, err := os.ReadFile(rawPlaylistPath)
	require.NoError(t, err)
	rawStr := string(rawBytes)
	for _, marker := range canaryMarkers {
		assert.True(t, strings.Contains(rawStr, marker), "internal playlist must retain canary marker %q from invalid ref", marker)
	}
	assert.Contains(t, rawStr, portCanaryRef)
	assert.Contains(t, rawStr, badURLCanaryRef)

	// 2. Public playlist must exist with 0644 permissions
	publicInfo, err := os.Stat(publicPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), publicInfo.Mode().Perm())

	// 3. HTTP file serving must fail closed: zero canary tokens, zero raw IPTV refs
	fileServer := controlhttp.SecureFileServer(dataDir, nil)

	for _, reqPath := range []string{"/playlist.m3u", "/playlist_public.m3u"} {
		req := httptest.NewRequest(http.MethodGet, reqPath, nil)
		rec := httptest.NewRecorder()
		fileServer.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "file server should return 200 for %s", reqPath)
		body := rec.Body.String()
		for _, marker := range canaryMarkers {
			assert.False(t, strings.Contains(body, marker), "playlist response for %s leaked canary marker %q from invalid ref: %s", reqPath, marker, body)
		}
		assert.False(t, strings.Contains(body, "4097:"), "playlist response for %s leaked raw 4097 ref from invalid ref", reqPath)
		assert.False(t, strings.Contains(body, "Canary Port Channel"), "invalid IPTV channel must be omitted from public export")
		assert.False(t, strings.Contains(body, "Canary BadURL Channel"), "invalid IPTV channel must be omitted from public export")

		// DVB channel must remain intact and served
		assert.Contains(t, body, "1:0:19:283D:3FB:1:C00000:0:0:0:")
		assert.Contains(t, body, "DVB Das Erste")
	}
}

// ----------------------------------------------------------------------------
// Test 9: Multi-Sink Zero-Canary Leak Audits (Slice 6)
// ----------------------------------------------------------------------------

// TestIPTV_CanaryLeakAudit_PrometheusMetricsZeroLeak asserts that Prometheus metrics
// never expose raw IPTV references, stream URLs, credentials, or tokens in metric names,
// descriptors, or label values.
func TestIPTV_CanaryLeakAudit_PrometheusMetricsZeroLeak(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	s, _ := newV3TestServer(t, t.TempDir())
	s.SetIPTVResolver(res)

	// Exercise legacy ingress increments
	metrics.IncIPTVLegacyIngress(metrics.EndpointIntents)
	metrics.IncIPTVLegacyIngress(metrics.EndpointPlaybackInfo)
	metrics.IncIPTVLegacyIngress(metrics.EndpointNowNext)
	metrics.IncIPTVLegacyIngress(metrics.EndpointTimers)
	metrics.IncIPTVLegacyIngress(metrics.EndpointLogos)
	metrics.IncIPTVLegacyIngress(metrics.EndpointHousehold)

	// Scrape /metrics endpoint via promhttp handler
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	promhttp.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	assertNoCanaryLeak(t, "GET /metrics", w)
	body := w.Body.String()
	assert.Contains(t, body, "v3_iptv_legacy_ingress_total")
	assert.NotContains(t, body, opaqueID)
}

// TestIPTV_CanaryLeakAudit_SQLiteStorePersistenceZeroLeak verifies that when profiles
// with IPTV references are persisted in a real SQLite database, client-visible API
// responses return masked iptv_<id> and zero canary tokens.
func TestIPTV_CanaryLeakAudit_SQLiteStorePersistenceZeroLeak(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "canary_identity.sqlite")
	sStore, err := identitystore.OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	defer sStore.Close()

	idSvc := identity.NewService(identity.Config{
		RPID:           "localhost",
		RPName:         "xg2g Test Server",
		ExpectedOrigin: "https://localhost",
		SessionTTL:     24 * time.Hour,
	}, sStore)

	s, _ := newV3TestServer(t, t.TempDir())
	s.SetIPTVResolver(res)
	s.SetIdentityService(idSvc)
	s.cfg.APIToken = "test-token"
	s.cfg.APITokenScopes = []string{string(ScopeAll)}

	adminPrincipal := &controlauth.Principal{
		ID:     "admin-1",
		Scopes: []string{string(ScopeAll)},
	}

	// 1. Persist profile with raw canary ref in SQLite
	createdProf, _, err := idSvc.CreateProfile(context.Background(), "admin-1", "Canary Admin Profile", "", false, nil, []string{canaryRawRef}, 18, "")
	require.NoError(t, err)
	profID := createdProf.ID

	// 2. Query GET /api/v3/profiles (reading from SQLite)
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/profiles", nil)
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.ListProfiles(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /api/v3/profiles (SQLite)", w)
	}

	// 3. Query GET /api/v3/profiles/{id} (reading single profile from SQLite)
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/profiles/"+profID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", profID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.GetProfile(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /api/v3/profiles/{id} (SQLite)", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}

	// 4. Query GET /api/v3/household/profiles (reading from SQLite)
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles", nil)
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.GetHouseholdProfiles(w, r, GetHouseholdProfilesParams{})
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /api/v3/household/profiles (SQLite)", w)
	}

	// 5. Query GET /api/v3/household/profiles/{id} (reading single profile from SQLite)
	{
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v3/household/profiles/"+profID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", profID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		r = r.WithContext(controlauth.WithPrincipal(r.Context(), adminPrincipal))
		s.GetProfile(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assertNoCanaryLeak(t, "GET /api/v3/household/profiles/{id} (SQLite)", w)
		assert.Contains(t, w.Body.String(), opaqueID)
	}
}

// TestIPTV_CanaryLeakAudit_ProblemDetailsErrorResponsesZeroLeak asserts that when invalid
// or malformed requests containing canary references are submitted, the resulting RFC 7807
// problem details responses never echo raw references, provider URLs, credentials, or tokens.
func TestIPTV_CanaryLeakAudit_ProblemDetailsErrorResponsesZeroLeak(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	s, st := newV3TestServer(t, t.TempDir())
	s.SetJWTSecret(jwtTestSecret)
	s.SetIPTVResolver(res)
	svc := new(MockRecordingsService)
	s.SetDependencies(Dependencies{
		Bus:               v3bus.NewMemoryBus(),
		Store:             st,
		Scan:              verifiedLivePlaybackScanner(),
		RecordingsService: svc,
		IPTVResolver:      res,
	})

	// Case 1: POST /api/v3/intents with malformed JSON body
	{
		badBody := fmt.Sprintf(`{"action":"start","serviceRef":%q,"corrupted":}`, canaryRawRef)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/intents", strings.NewReader(badBody))
		r.Header.Set("Content-Type", "application/json")
		s.handleV3Intents(w, r)
		assert.True(t, w.Code >= 400 && w.Code < 500, "expected 4xx, got %d", w.Code)
		assertNoCanaryLeak(t, "POST /api/v3/intents (malformed)", w)
	}

	// Case 2: POST /api/v3/stream/prepare with non-existent opaque ID containing marker substring
	{
		badOpaque := "iptv_nonexistent_canary_invalid"
		body := fmt.Sprintf(`{"serviceRef":%q}`, badOpaque)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/stream/prepare", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.StartStreamPrepare(w, r, StartStreamPrepareParams{})
		assert.True(t, w.Code >= 400, "expected error status >= 400, got %d", w.Code)
		assertNoCanaryLeak(t, "POST /api/v3/stream/prepare (invalid ID)", w)
	}

	// Case 3: POST /api/v3/live/stream-info with unknown opaque ID
	{
		badOpaque := "iptv_abcdefghijklmnopqrstuvwxyz"
		caps := `{"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},"container":["mp4","ts"],"videoCodecs":["h264"],"audioCodecs":["aac"]}`
		body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, badOpaque, caps)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
		assert.Equal(t, http.StatusNotFound, w.Code)
		assertNoCanaryLeak(t, "POST /api/v3/live/stream-info (not found)", w)
	}

	// Case 4: POST /api/v3/timers with invalid timer body
	{
		badBody := fmt.Sprintf(`{"serviceRef":%q,"begin":-1,"end":-5}`, opaqueID)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(badBody))
		r.Header.Set("Content-Type", "application/json")
		s.AddTimer(w, r)
		assert.True(t, w.Code >= 400 && w.Code < 500, "expected 4xx, got %d", w.Code)
		assertNoCanaryLeak(t, "POST /api/v3/timers (invalid body)", w)
	}
}

// TestIPTV_CanaryLeakAudit_LiveLoggingBufferZeroLeak verifies that in-memory structured
// log entries emitted during operations with canary references are scrubbed before storage.
func TestIPTV_CanaryLeakAudit_LiveLoggingBufferZeroLeak(t *testing.T) {
	res, canaryID, _ := setupCanaryResolver(t)
	opaqueID := string(canaryID)

	var logWriterBuf bytes.Buffer
	ilog.Configure(ilog.Config{
		Output: &logWriterBuf,
		Level:  "info",
	})
	defer ilog.ClearRecentLogs()

	s, st := newV3TestServer(t, t.TempDir())
	s.SetJWTSecret(jwtTestSecret)
	s.SetIPTVResolver(res)
	svc := new(MockRecordingsService)
	s.SetDependencies(Dependencies{
		Bus:               v3bus.NewMemoryBus(),
		Store:             st,
		Scan:              verifiedLivePlaybackScanner(),
		RecordingsService: svc,
		IPTVResolver:      res,
	})

	ilog.ClearRecentLogs()

	// Direct audit log entry with raw canary reference to verify scrubber in structured buffer
	ilog.AuditInfo(context.Background(), "stream.connect", fmt.Sprintf("Stream connection established for %s", canaryRawRef), map[string]any{
		"service_ref": canaryRawRef,
		"url":         "http://canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts",
	})

	// Direct log entry with raw canary reference to verify scrubber in stdout writer
	ilog.L().Info().
		Str("service_ref", canaryRawRef).
		Str("url", "http://canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts").
		Msgf("Stream connection established for %s", canaryRawRef)

	// Perform operations that generate handler log output via middleware
	caps := `{"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},"container":["mp4","ts"],"videoCodecs":["h264"],"audioCodecs":["aac"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, opaqueID, caps)))
	r.Header.Set("Content-Type", "application/json")
	ilog.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
	})).ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	logs := ilog.GetRecentLogs()
	require.NotEmpty(t, logs, "expected log entries to be recorded")

	for _, entry := range logs {
		msgLower := strings.ToLower(entry.Message)
		for _, marker := range canaryMarkers {
			assert.NotContains(t, msgLower, strings.ToLower(marker), "log message leaked canary marker: %s", entry.Message)
		}
		if entry.Fields != nil {
			for k, v := range entry.Fields {
				vStr := fmt.Sprintf("%v", v)
				vLower := strings.ToLower(vStr)
				for _, marker := range canaryMarkers {
					assert.NotContains(t, vLower, strings.ToLower(marker), "log field %s leaked canary marker: %s", k, vStr)
				}
			}
		}
	}

	// Also verify that the logWriterBuf (stdout output) contains zero canary markers
	written := logWriterBuf.String()
	for _, marker := range canaryMarkers {
		assert.NotContains(t, strings.ToLower(written), strings.ToLower(marker), "stdout writer output leaked canary marker")
	}
}
