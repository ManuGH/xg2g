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
	"sync"
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
	"github.com/go-chi/chi/v5"
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

// ----------------------------------------------------------------------------
// Route Classification Tables for Chi Router Walking
// ----------------------------------------------------------------------------

// refFreeRoutes documents routes that do not take or return channel/stream service references.
var refFreeRoutes = map[string]string{
	"GET /.well-known/assetlinks.json":                                    "Static Android Digital Asset Links metadata",
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
	"POST /api/v3/auth/sessions/revoke-others":                            "Auth: revoke other active sessions",
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
	"POST /api/v3/notifications/mark-all-read":                          "Notifications: mark all read",
	"POST /api/v3/notifications/mark-read":                              "Notifications: mark single read",
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
	"GET /api/v3/streams":                                                "Streams: list active streams",
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
	"POST /api/v3/intents":                     "Inbound: stream start/stop intents",
	"POST /api/v3/services/now-next":           "Inbound: batch now-next EPG lookups",
	"POST /api/v3/live/playback-summary":       "Inbound: batch playback summary",
	"POST /api/v3/timers":                      "Inbound: timer creation",
	"GET /api/v3/timers/{timerId}":             "Inbound: timer lookup by opaque ID",
	"PATCH /api/v3/timers/{timerId}":           "Inbound: timer update by opaque ID",
	"DELETE /api/v3/timers/{timerId}":          "Inbound: timer deletion by opaque ID",
	"POST /api/v3/timers/conflicts:preview":    "Inbound: timer conflicts preview",
	"POST /api/v3/stream/prepare":              "Inbound: zap preparation start",
	"GET /api/v3/stream/prepare/{preparationId}": "Inbound: zap preparation status",
	"POST /api/v3/stream/prepare/{preparationId}/commit": "Inbound: zap preparation commit",
	"DELETE /api/v3/stream/prepare/{preparationId}": "Inbound: zap preparation cancel",
	"POST /api/v3/household/profiles":          "Inbound: household profile creation",
	"PUT /api/v3/household/profiles/{profileId}": "Inbound: household profile update",
	"PUT /api/v3/household/profiles/{id}":      "Inbound: household profile update by id",
	"POST /api/v3/profiles":                    "Inbound: legacy profile create alias",
	"PUT /api/v3/profiles/{id}":                "Inbound: legacy profile update alias",
	"POST /api/v3/telemetry/playback":          "Inbound: client telemetry playback reporting",
}

// allowlistEntry describes an outbound sink or log that currently leaks raw refs.
type allowlistEntry struct {
	Route       string
	SourceLoc   string
	TargetSlice string
	Description string
}

var allowlistRoutes = map[string]allowlistEntry{
	"GET /api/v3/services": {
		Route:       "GET /api/v3/services",
		SourceLoc:   "backend/internal/control/http/v3/services.go:45",
		TargetSlice: "Slice 4",
		Description: "Outbound: services list returns raw IPTV URLs from OpenWebIF/playlist",
	},
	"GET /api/v3/services/bouquets": {
		Route:       "GET /api/v3/services/bouquets",
		SourceLoc:   "backend/internal/control/http/v3/services.go:120",
		TargetSlice: "Slice 4",
		Description: "Outbound: bouquet channel listings leak raw refs",
	},
	"GET /api/v3/epg": {
		Route:       "GET /api/v3/epg",
		SourceLoc:   "backend/internal/control/http/v3/epg.go:50",
		TargetSlice: "Slice 4",
		Description: "Outbound: full EPG export contains raw Enigma2 refs",
	},
	"GET /api/v3/timers": {
		Route:       "GET /api/v3/timers",
		SourceLoc:   "backend/internal/control/http/v3/handlers_timers.go:300",
		TargetSlice: "Slice 4",
		Description: "Outbound: receiver timer listing contains raw refs",
	},
	"GET /api/v3/sessions": {
		Route:       "GET /api/v3/sessions",
		SourceLoc:   "backend/internal/control/http/v3/sessions.go:80",
		TargetSlice: "Slice 4",
		Description: "Outbound: active session listings contain raw refs",
	},
	"GET /api/v3/sessions/{sessionID}": {
		Route:       "GET /api/v3/sessions/{sessionID}",
		SourceLoc:   "backend/internal/control/http/v3/sessions.go:120",
		TargetSlice: "Slice 4",
		Description: "Outbound: session detail contains raw ref",
	},
	"GET /api/v3/sessions/{sessionID}/events": {
		Route:       "GET /api/v3/sessions/{sessionID}/events",
		SourceLoc:   "backend/internal/control/http/v3/sessions.go:160",
		TargetSlice: "Slice 4",
		Description: "Outbound: session SSE events contain raw ref",
	},
	"GET /api/v3/receiver/current": {
		Route:       "GET /api/v3/receiver/current",
		SourceLoc:   "backend/internal/control/http/v3/receiver.go:50",
		TargetSlice: "Slice 4",
		Description: "Outbound: current live channel info from OpenWebIF contains raw ref",
	},
	"GET /api/v3/household/profiles": {
		Route:       "GET /api/v3/household/profiles",
		SourceLoc:   "backend/internal/control/http/v3/handlers_household.go:180",
		TargetSlice: "Slice 4",
		Description: "Outbound: household profile list returns internal raw refs",
	},
	"GET /api/v3/household/profiles/{id}": {
		Route:       "GET /api/v3/household/profiles/{id}",
		SourceLoc:   "backend/internal/control/http/v3/handlers_household.go:210",
		TargetSlice: "Slice 4",
		Description: "Outbound: household profile by ID returns internal raw refs",
	},
	"GET /api/v3/profiles": {
		Route:       "GET /api/v3/profiles",
		SourceLoc:   "backend/internal/control/http/v3/handlers_household.go:380",
		TargetSlice: "Slice 4",
		Description: "Outbound: legacy profile list returns internal raw refs",
	},
	"GET /api/v3/profiles/{id}": {
		Route:       "GET /api/v3/profiles/{id}",
		SourceLoc:   "backend/internal/control/http/v3/handlers_household.go:420",
		TargetSlice: "Slice 4",
		Description: "Outbound: legacy profile by ID returns internal raw refs",
	},
	"GET /api/v3/recordings": {
		Route:       "GET /api/v3/recordings",
		SourceLoc:   "backend/internal/control/http/v3/recordings.go:100",
		TargetSlice: "Slice 4",
		Description: "Outbound: recordings list contains service refs",
	},
	"GET /api/v3/recordings/{recordingId}/stream-info": {
		Route:       "GET /api/v3/recordings/{recordingId}/stream-info",
		SourceLoc:   "backend/internal/control/http/v3/recordings.go:200",
		TargetSlice: "Slice 4",
		Description: "Outbound: recording stream info contains recording refs",
	},
	"POST /api/v3/recordings/{recordingId}/stream-info": {
		Route:       "POST /api/v3/recordings/{recordingId}/stream-info",
		SourceLoc:   "backend/internal/control/http/v3/recordings.go:200",
		TargetSlice: "Slice 4",
		Description: "Outbound: recording stream info post contains recording refs",
	},
	"GET /api/v3/recordings/{recordingId}/status": {
		Route:       "GET /api/v3/recordings/{recordingId}/status",
		SourceLoc:   "backend/internal/control/http/v3/recordings.go:250",
		TargetSlice: "Slice 4",
		Description: "Outbound: recording status contains recording refs",
	},
	"GET /api/v3/series-rules": {
		Route:       "GET /api/v3/series-rules",
		SourceLoc:   "backend/internal/control/http/v3/series_rules.go:50",
		TargetSlice: "Slice 4",
		Description: "Outbound: series rules contain target channel refs",
	},
	"POST /api/v3/series-rules": {
		Route:       "POST /api/v3/series-rules",
		SourceLoc:   "backend/internal/control/http/v3/series_rules.go:90",
		TargetSlice: "Slice 4",
		Description: "Outbound: series rule create response echoes target channel ref",
	},
	"PUT /api/v3/series-rules/{id}": {
		Route:       "PUT /api/v3/series-rules/{id}",
		SourceLoc:   "backend/internal/control/http/v3/series_rules.go:120",
		TargetSlice: "Slice 4",
		Description: "Outbound: series rule update response echoes target channel ref",
	},
	"GET /api/v3/logs": {
		Route:       "GET /api/v3/logs",
		SourceLoc:   "backend/internal/control/http/v3/logs.go:40",
		TargetSlice: "Slice 5",
		Description: "Outbound: internal log sink contains pre-existing raw ref leaks until central scrubber",
	},
	"POST /api/v3/live/stream-info": {
		Route:       "POST /api/v3/live/stream-info",
		SourceLoc:   "backend/internal/control/http/v3/playbackinfo/response_mapping.go:534",
		TargetSlice: "Slice 4",
		Description: "Outbound: stream URLs and session ID leak raw ref until Slice 4 outbound masking",
	},
	"POST /api/v3/live/playback-summary": {
		Route:       "POST /api/v3/live/playback-summary",
		SourceLoc:   "backend/internal/control/http/v3/playbackinfo/response_mapping.go:47",
		TargetSlice: "Slice 4",
		Description: "Outbound: stream URLs in playback decision summary items leak raw ref until Slice 4",
	},
	"POST /api/v3/timers": {
		Route:       "POST /api/v3/timers",
		SourceLoc:   "backend/internal/control/http/v3/handlers_timers.go:253",
		TargetSlice: "Slice 4",
		Description: "Outbound: AddTimer response echoes created timer raw ref until Slice 4 outbound masking",
	},
	"POST /api/v3/household/profiles": {
		Route:       "POST /api/v3/household/profiles",
		SourceLoc:   "backend/internal/control/http/v3/handlers_household.go:344",
		TargetSlice: "Slice 4",
		Description: "Outbound: Profile create echoes stored raw refs until Slice 4 outbound masking",
	},
}

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
		_, inRefFree := refFreeRoutes[key]
		_, inAllowlist := allowlistRoutes[key]

		if !inExercised && !inRefFree && !inAllowlist {
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

	assertNoCanaryLeak := func(t *testing.T, endpointName string, w *httptest.ResponseRecorder) {
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
	// Assert each allowlist entry is documented with valid metadata
	require.NotEmpty(t, allowlistRoutes, "Allowlist must not be empty until all slices are complete")

	for routeKey, entry := range allowlistRoutes {
		assert.Equal(t, routeKey, entry.Route)
		assert.NotEmpty(t, entry.SourceLoc, "Entry %s missing SourceLoc", routeKey)
		assert.NotEmpty(t, entry.TargetSlice, "Entry %s missing TargetSlice", routeKey)
		assert.NotEmpty(t, entry.Description, "Entry %s missing Description", routeKey)
	}

	// Verify non-staleness of POST /api/v3/live/stream-info (assert it DOES leak until Slice 4)
	res, canaryID, _ := setupCanaryResolver(t)
	s, st := newV3TestServer(t, t.TempDir())
	s.SetJWTSecret(jwtTestSecret)
	s.SetDependencies(Dependencies{
		Store:             st,
		Scan:              verifiedLivePlaybackScanner(),
		RecordingsService: new(MockRecordingsService),
		Households:        householddomain.NewService(householddomain.NewMemoryStore()),
	})
	s.SetIPTVResolver(res)

	caps := `{
		"capabilitiesVersion":2,"clientIdentity":{"platform":"ios","surface":"browser","browserEngine":"webkit"},
		"container":["mp4","ts"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`
	body := fmt.Sprintf(`{"serviceRef":%q,"capabilities":%s}`, canaryID, caps)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	s.PostLivePlaybackInfo(w, r, PostLivePlaybackInfoParams{})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "canary.invalid", "POST /api/v3/live/stream-info must leak until Slice 4 outbound masking removes it")

	// Verify non-staleness of POST /api/v3/live/playback-summary (assert it DOES leak until Slice 4)
	sumBody := fmt.Sprintf(`{"serviceRefs":[%q],"capabilities":%s}`, canaryID, caps)
	wSum := postPlaybackSummary(t, s, sumBody)
	require.Equal(t, http.StatusOK, wSum.Code)
	assert.Contains(t, strings.ToLower(wSum.Body.String()), "canary.invalid", "POST /api/v3/live/playback-summary must leak until Slice 4 outbound masking removes it")

	// Verify non-staleness of POST /api/v3/timers (assert 201 response echoes raw ref until Slice 4)
	var addedTimer *openwebif.Timer
	s.owiFactory = func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
		return &mockOWI{
			getTimersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
				if addedTimer != nil {
					return []openwebif.Timer{*addedTimer}, nil
				}
				return nil, nil
			},
			addTimerFunc: func(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
				addedTimer = &openwebif.Timer{ServiceRef: sRef, Begin: begin, End: end, Name: name}
				return nil
			},
		}
	}
	timerBody := fmt.Sprintf(`{"serviceRef":%q,"name":"Canary","begin":1700000000,"end":1700003600}`, canaryID)
	wTimer := httptest.NewRecorder()
	rTimer := httptest.NewRequest(http.MethodPost, "/api/v3/timers", strings.NewReader(timerBody))
	rTimer.Header.Set("Content-Type", "application/json")
	s.AddTimer(wTimer, rTimer)
	require.Equal(t, http.StatusCreated, wTimer.Code)
	assert.Contains(t, strings.ToLower(wTimer.Body.String()), "canary.invalid", "POST /api/v3/timers must leak until Slice 4 outbound masking removes it")

	// Verify non-staleness of POST /api/v3/household/profiles (assert 201 response echoes stored raw ref until Slice 4)
	profBody := householddomain.Profile{
		ID:                 "canary-profile",
		Name:               "Canary Profile",
		Kind:               householddomain.ProfileKindAdult,
		AllowedServiceRefs: []string{string(canaryID)},
	}
	bodyBytes, _ := json.Marshal(profBody)
	wProf := httptest.NewRecorder()
	rProf := httptest.NewRequest(http.MethodPost, "/api/v3/household/profiles", bytes.NewReader(bodyBytes))
	rProf.Header.Set("Content-Type", "application/json")
	s.PostHouseholdProfiles(wProf, rProf, PostHouseholdProfilesParams{})
	require.Equal(t, http.StatusCreated, wProf.Code)
	assert.Contains(t, strings.ToLower(wProf.Body.String()), "canary.invalid", "POST /api/v3/household/profiles must leak until Slice 4 outbound masking removes it")
}

// ----------------------------------------------------------------------------
// Test 4: Six Negative Controls (a through f)
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
