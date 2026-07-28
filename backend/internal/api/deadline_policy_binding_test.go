// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type expectedRoute struct {
	method string
	path   string
}

// getPhase1CanonicalBaselineMap is deliberately independent of ClassifyRoute
// and router construction. It is the static Phase 1 acceptance snapshot.
func getPhase1CanonicalBaselineMap() map[RegistrationKey]RoutePolicy {
	apiPolicy := RoutePolicy{Class: RouteDeadlineAPIBounded}
	mediaPolicy := RoutePolicy{Class: RouteDeadlineMediaBounded}
	streamPolicy := RoutePolicy{Class: RouteDeadlineStreaming}
	ssePolicy := RoutePolicy{Class: RouteDeadlineStreaming, RequiresFlush: true}

	baseline := make(map[RegistrationKey]RoutePolicy, 103)
	add := func(routerID, method, path string, policy RoutePolicy) {
		key := RegistrationKey{RouterID: routerID, Method: method, Pattern: path}
		if _, exists := baseline[key]; exists {
			panic("duplicate static Phase 1 key: " + key.String())
		}
		baseline[key] = policy
	}

	for _, route := range []expectedRoute{
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/readyz"},
		{http.MethodPost, "/ui/*"},
		{http.MethodPut, "/ui/*"},
		{http.MethodDelete, "/ui/*"},
		{http.MethodOptions, "/ui/*"},
		{http.MethodPatch, "/ui/*"},
		{http.MethodTrace, "/ui/*"},
		{http.MethodConnect, "/ui/*"},
		{http.MethodGet, "/index.html"},
		{http.MethodGet, "/"},
		{http.MethodPost, "/internal/system/config/reload"},
		{http.MethodGet, "/api/v3/status"},
		{http.MethodPost, "/internal/setup/validate"},
		{http.MethodGet, "/api/v3/vod/{recordingId}"},
		{http.MethodPut, "/api/v3/recordings/{recordingId}/resume"},
		{http.MethodGet, "/api/v3/recordings/continue"},
		{http.MethodPost, "/Items/{itemId}/PlaybackInfo"},
	} {
		add("outer", route.method, route.path, apiPolicy)
	}
	for _, route := range []expectedRoute{
		{http.MethodGet, "/ui/*"},
		{http.MethodHead, "/ui/*"},
		{http.MethodGet, "/ui"},
		{http.MethodGet, "/logos/{filename}"},
		{http.MethodHead, "/api/v3/recordings/{recordingId}/stream.mp4"},
	} {
		add("outer", route.method, route.path, mediaPolicy)
	}

	v3Routes := []expectedRoute{
		{http.MethodPost, "/timers"},
		{http.MethodPost, "/pairing/{pairingId}/approve"},
		{http.MethodGet, "/auth/web-bootstrap/{bootstrapId}"},
		{http.MethodPost, "/auth/device/session"},
		{http.MethodPost, "/intents"},
		{http.MethodPost, "/series-rules"},
		{http.MethodPost, "/auth/session"},
		{http.MethodPost, "/auth/web-bootstrap"},
		{http.MethodDelete, "/household/profiles/{profileId}"},
		{http.MethodDelete, "/household/unlock"},
		{http.MethodDelete, "/recordings/{recordingId}"},
		{http.MethodDelete, "/series-rules/{id}"},
		{http.MethodDelete, "/auth/session"},
		{http.MethodDelete, "/streams/{id}"},
		{http.MethodDelete, "/system/entitlements/overrides/{principalId}/{scope}"},
		{http.MethodDelete, "/timers/{timerId}"},
		{http.MethodPost, "/pairing/{pairingId}/exchange"},
		{http.MethodGet, "/dvr/capabilities"},
		{http.MethodGet, "/dvr/status"},
		{http.MethodGet, "/epg"},
		{http.MethodGet, "/errors"},
		{http.MethodGet, "/household/profiles"},
		{http.MethodGet, "/household/unlock"},
		{http.MethodGet, "/logs"},
		{http.MethodPost, "/pairing/{pairingId}/status"},
		{http.MethodGet, "/receiver/current"},
		{http.MethodGet, "/recordings/{recordingId}/{segment}"},
		{http.MethodHead, "/recordings/{recordingId}/{segment}"},
		{http.MethodGet, "/recordings/{recordingId}/playlist.m3u8"},
		{http.MethodHead, "/recordings/{recordingId}/playlist.m3u8"},
		{http.MethodGet, "/recordings/{recordingId}/timeshift.m3u8"},
		{http.MethodHead, "/recordings/{recordingId}/timeshift.m3u8"},
		{http.MethodGet, "/recordings/{recordingId}/stream-info"},
		{http.MethodGet, "/recordings/{recordingId}/scrub.jpg"},
		{http.MethodGet, "/recordings/{recordingId}/thumbnail.jpg"},
		{http.MethodGet, "/recordings"},
		{http.MethodGet, "/recordings/{recordingId}/status"},
		{http.MethodGet, "/series-rules"},
		{http.MethodGet, "/services"},
		{http.MethodGet, "/services/bouquets"},
		{http.MethodGet, "/sessions/{sessionID}/events"},
		{http.MethodGet, "/sessions/{sessionID}"},
		{http.MethodGet, "/streams"},
		{http.MethodGet, "/system/config"},
		{http.MethodGet, "/system/connectivity"},
		{http.MethodGet, "/system/entitlements"},
		{http.MethodGet, "/system/health"},
		{http.MethodGet, "/system/healthz"},
		{http.MethodGet, "/system/info"},
		{http.MethodGet, "/system/scan"},
		{http.MethodGet, "/timers/{timerId}"},
		{http.MethodGet, "/timers"},
		{http.MethodGet, "/sessions"},
		{http.MethodPost, "/household/profiles"},
		{http.MethodPost, "/household/unlock"},
		{http.MethodPost, "/live/stream-info"},
		{http.MethodPost, "/live/playback-summary"},
		{http.MethodPost, "/recordings/{recordingId}/delete"},
		{http.MethodPost, "/recordings/{recordingId}/stream-info"},
		{http.MethodPost, "/recordings/{recordingId}/rename"},
		{http.MethodPost, "/services/{id}/toggle"},
		{http.MethodPost, "/services/now-next"},
		{http.MethodPost, "/sessions/{sessionID}/heartbeat"},
		{http.MethodPost, "/system/entitlements/overrides"},
		{http.MethodPost, "/system/entitlements/receipts"},
		{http.MethodPost, "/system/refresh"},
		{http.MethodPost, "/timers/conflicts:preview"},
		{http.MethodHead, "/recordings/{recordingId}/stream.mp4"},
		{http.MethodPut, "/household/profiles/{profileId}"},
		{http.MethodPut, "/system/config"},
		{http.MethodPost, "/sessions/{sessionId}/feedback"},
		{http.MethodPost, "/series-rules/run"},
		{http.MethodPost, "/series-rules/{id}/run"},
		{http.MethodGet, "/sessions/{sessionID}/hls/{filename}"},
		{http.MethodHead, "/sessions/{sessionID}/hls/{filename}"},
		{http.MethodPost, "/pairing/start"},
		{http.MethodGet, "/recordings/{recordingId}/stream.mp4"},
		{http.MethodPost, "/system/scan"},
		{http.MethodPut, "/series-rules/{id}"},
		{http.MethodPatch, "/timers/{timerId}"},
	}
	for _, route := range v3Routes {
		add("v3", route.method, "/api/v3"+route.path, apiPolicy)
	}
	setV3Policy := func(method, path string, policy RoutePolicy) {
		key := RegistrationKey{RouterID: "v3", Method: method, Pattern: "/api/v3" + path}
		if _, exists := baseline[key]; !exists {
			panic("static policy override references unknown key: " + key.String())
		}
		baseline[key] = policy
	}
	setV3Policy(http.MethodGet, "/sessions/{sessionID}/events", ssePolicy)
	setV3Policy(http.MethodGet, "/sessions/{sessionID}/hls/{filename}", mediaPolicy)
	setV3Policy(http.MethodHead, "/sessions/{sessionID}/hls/{filename}", mediaPolicy)
	setV3Policy(http.MethodHead, "/recordings/{recordingId}/stream.mp4", mediaPolicy)
	setV3Policy(http.MethodGet, "/recordings/{recordingId}/stream.mp4", streamPolicy)

	return baseline
}

func snapshotAsMap(snapshot PolicyBindingSnapshot) map[RegistrationKey]RoutePolicy {
	result := make(map[RegistrationKey]RoutePolicy, snapshot.Len())
	for _, key := range snapshot.Keys() {
		policy, ok := snapshot.Lookup(key)
		if !ok {
			panic("snapshot key disappeared: " + key.String())
		}
		result[key] = policy
	}
	return result
}

func validatePolicyBindingParity(actual, expected map[RegistrationKey]RoutePolicy) error {
	for key, expectedPolicy := range expected {
		actualPolicy, ok := actual[key]
		if !ok {
			return fmt.Errorf("missing policy binding %s", key)
		}
		if actualPolicy != expectedPolicy {
			return fmt.Errorf("wrong policy for %s: got %+v, want %+v", key, actualPolicy, expectedPolicy)
		}
	}
	for key := range actual {
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("unexpected policy binding %s", key)
		}
	}
	return nil
}

func TestPhase1RegistrationIdentityParity(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	router, snapshot, err := s.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)
	require.NotNil(t, router)

	expected := getPhase1CanonicalBaselineMap()
	actual := snapshotAsMap(snapshot)
	require.Len(t, expected, 103)
	require.Len(t, actual, 103)
	require.NoError(t, validatePolicyBindingParity(actual, expected))

	counts := map[string]int{}
	for key := range actual {
		counts[key.RouterID]++
	}
	require.Equal(t, 23, counts["outer"])
	require.Equal(t, 80, counts["v3"])
}

func TestPolicyBindingSnapshotTracksBuildSpecificUIVariant(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	apiPolicy := RoutePolicy{Class: RouteDeadlineAPIBounded}
	mediaPolicy := RoutePolicy{Class: RouteDeadlineMediaBounded}
	devProxyPolicy := RoutePolicy{Class: RouteDeadlineMediaBounded, MayUpgradePerRequest: true}

	for _, test := range []struct {
		name      string
		variant   ConfigVariant
		uiGet     RoutePolicy
		uiHead    RoutePolicy
		uiRootGet RoutePolicy
	}{
		{"prod_static", ConfigVariantProdStatic, mediaPolicy, mediaPolicy, mediaPolicy},
		{"dev_dir", ConfigVariantDevDir, mediaPolicy, mediaPolicy, mediaPolicy},
		{"dev_proxy", ConfigVariantDevProxy, devProxyPolicy, apiPolicy, devProxyPolicy},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, snapshot, err := s.buildRouterWithBindings(test.variant)
			require.NoError(t, err)
			require.Equal(t, 103, snapshot.Len())

			for key, expected := range map[RegistrationKey]RoutePolicy{
				{RouterID: "outer", Method: http.MethodGet, Pattern: "/ui/*"}:  test.uiGet,
				{RouterID: "outer", Method: http.MethodHead, Pattern: "/ui/*"}: test.uiHead,
				{RouterID: "outer", Method: http.MethodGet, Pattern: "/ui"}:    test.uiRootGet,
			} {
				actual, ok := snapshot.Lookup(key)
				require.True(t, ok, "missing %s", key)
				require.Equal(t, expected, actual, "%s", key)
			}
		})
	}
}

func getDefaultPhase2VerifiedEvidenceRegistry() map[ResponseWriterEquivalenceClass]VerifiedStackEvidence {
	evidence := GetDefaultPhase1VerifiedEvidenceRegistry()
	for _, class := range []ResponseWriterEquivalenceClass{
		EquivalenceClassOuterStandard,
		EquivalenceClassOuterCompressed,
	} {
		entry := evidence[class]
		entry.HijackVerified = true
		entry.UpgradeTransitionVerified = true
		evidence[class] = entry
	}
	return evidence
}

func TestPhase2RuntimeReadinessAll103Routes(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	registrations, err := ValidateRouterInventory(s, ConfigVariantDevProxy)
	require.NoError(t, err)
	require.Len(t, registrations, 103)

	evidence := getDefaultPhase2VerifiedEvidenceRegistry()
	runtimeReady := 0
	for _, registration := range registrations {
		declared, declaredErr := DeclaredCapabilitiesForStack(StackConfig{
			RouterID:    registration.Key.RouterID,
			UIMode:      ConfigVariantDevProxy,
			Compression: true,
		})
		require.NoError(t, declaredErr, "%s", registration.Key)
		registration.Capabilities = ApplyVerifiedEvidence(declared, evidence)
		require.NoError(t, registration.ValidateRuntimeReadiness(), "%s", registration.Key)
		runtimeReady++
	}
	require.Equal(t, 103, runtimeReady)
}

func TestPolicyBindingGovernanceDetectsSnapshotMutations(t *testing.T) {
	expected := getPhase1CanonicalBaselineMap()
	known := RegistrationKey{RouterID: "outer", Method: http.MethodGet, Pattern: "/healthz"}

	t.Run("missing", func(t *testing.T) {
		actual := clonePolicyMap(expected)
		delete(actual, known)
		require.ErrorContains(t, validatePolicyBindingParity(actual, expected), "missing policy binding")
	})
	t.Run("extra", func(t *testing.T) {
		actual := clonePolicyMap(expected)
		actual[RegistrationKey{RouterID: "outer", Method: http.MethodGet, Pattern: "/extra"}] = RoutePolicy{Class: RouteDeadlineAPIBounded}
		require.ErrorContains(t, validatePolicyBindingParity(actual, expected), "unexpected policy binding")
	})
	t.Run("wrong_policy", func(t *testing.T) {
		actual := clonePolicyMap(expected)
		actual[known] = RoutePolicy{Class: RouteDeadlineMediaBounded}
		require.ErrorContains(t, validatePolicyBindingParity(actual, expected), "wrong policy")
	})
	t.Run("router_id_shift_with_unchanged_counts", func(t *testing.T) {
		actual := clonePolicyMap(expected)
		v3Key := RegistrationKey{RouterID: "v3", Method: http.MethodPost, Pattern: "/api/v3/auth/session"}
		outerPolicy := actual[known]
		v3Policy := actual[v3Key]
		delete(actual, known)
		delete(actual, v3Key)
		actual[RegistrationKey{RouterID: "v3", Method: known.Method, Pattern: known.Pattern}] = outerPolicy
		actual[RegistrationKey{RouterID: "outer", Method: v3Key.Method, Pattern: v3Key.Pattern}] = v3Policy
		require.Len(t, actual, 103)
		require.Error(t, validatePolicyBindingParity(actual, expected))
	})
}

func clonePolicyMap(source map[RegistrationKey]RoutePolicy) map[RegistrationKey]RoutePolicy {
	clone := make(map[RegistrationKey]RoutePolicy, len(source))
	for key, policy := range source {
		clone[key] = policy
	}
	return clone
}

func TestMountedV3RoutesRetainAuthAndScopeProtection(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{
		APIToken:       "read-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	router, _, err := s.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)

	t.Run("authentication", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})
	t.Run("read_scope_reaches_handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz", nil)
		req.Header.Set("Authorization", "Bearer read-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("write_scope_denied", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v3/system/refresh", nil)
		req.Header.Set("Authorization", "Bearer read-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestMountedV3RoutePreservesChiURLParams(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	const expectedTimerID = "phase2-route-param"
	var capturedTimerID string
	var capturedDeadlineState *middleware.DeadlineState
	s.v3Handler.AuthMiddlewareOverride = func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedTimerID = chi.URLParam(r, "timerId")
			capturedDeadlineState, _ = middleware.DeadlineStateFromContext(r.Context())
			w.WriteHeader(218)
		})
	}

	router, _, err := s.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/timers/"+expectedTimerID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, 218, rec.Code)
	require.Equal(t, expectedTimerID, capturedTimerID)
	require.NotNil(t, capturedDeadlineState)
	require.True(t, capturedDeadlineState.IsBound())
	require.Equal(t, middleware.RuntimeEnforced, capturedDeadlineState.Mode)
	require.Equal(t, RouteDeadlineAPIBounded, capturedDeadlineState.Policy.Class)
}

func TestUIWildcardMethodPolicyParityExecutesSelectedHandler(t *testing.T) {
	devDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(devDir, "index.html"),
		[]byte("<!doctype html><html><body>phase2 UI</body></html>"),
		0o600,
	))
	t.Setenv("XG2G_UI_DEV_DIR", devDir)
	t.Setenv("XG2G_UI_DEV_PROXY_URL", "")

	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	router, _, err := s.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)

	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodTrace,
		http.MethodConnect,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/ui/phase2-handler-selection", nil)
			if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
				req.Header.Set("Origin", "http://example.com")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			expectedStatus := http.StatusOK
			if method == http.MethodOptions {
				expectedStatus = http.StatusNoContent
			}
			require.Equal(t, expectedStatus, rec.Code)
			if method != http.MethodOptions {
				require.NotEmpty(t, rec.Header().Get("Content-Security-Policy"), "UI handler was not selected")
			}
		})
	}
}
