// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package api

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetDefaultPhase1VerifiedEvidenceRegistry returns the empirical evidence registry gathered from HTTP/TCP probes for Phase 1.
func GetDefaultPhase1VerifiedEvidenceRegistry() map[ResponseWriterEquivalenceClass]VerifiedStackEvidence {
	return map[ResponseWriterEquivalenceClass]VerifiedStackEvidence{
		EquivalenceClassOuterStandard: {
			EquivalenceClass:          EquivalenceClassOuterStandard,
			SetWriteDeadlineVerified:  true,
			FlushVerified:             true,
			HijackVerified:            false, // Phase 2 pending
			UpgradeTransitionVerified: false, // Phase 2 pending
		},
		EquivalenceClassOuterCompressed: {
			EquivalenceClass:          EquivalenceClassOuterCompressed,
			SetWriteDeadlineVerified:  true,
			FlushVerified:             true,
			HijackVerified:            false, // Phase 2 pending
			UpgradeTransitionVerified: false, // Phase 2 pending
		},
		EquivalenceClassV3Standard: {
			EquivalenceClass:          EquivalenceClassV3Standard,
			SetWriteDeadlineVerified:  true,
			FlushVerified:             true,
			HijackVerified:            false, // Phase 2 pending
			UpgradeTransitionVerified: false, // Phase 2 pending
		},
		EquivalenceClassV3Compressed: {
			EquivalenceClass:          EquivalenceClassV3Compressed,
			SetWriteDeadlineVerified:  true,
			FlushVerified:             true,
			HijackVerified:            false, // Phase 2 pending
			UpgradeTransitionVerified: false, // Phase 2 pending
		},
	}
}

// ValidateRouterInventory walks the outer router (via canonical s.buildRouter()) and v3 subrouter for a Server, collects all route registrations, and validates that every instance is cleanly classified.
func ValidateRouterInventory(s *Server, variant ConfigVariant) ([]RouteRegistration, error) {
	outerRouter := s.buildRouter()
	return ValidateCustomRouterInventory(outerRouter, s.v3Handler, s.cfg, variant)
}

// ValidateCustomRouterInventory walks custom outer and v3 routers for inventory validation.
func ValidateCustomRouterInventory(outerRouter chi.Router, v3Server *v3.Server, cfg config.AppConfig, variant ConfigVariant) ([]RouteRegistration, error) {
	v3Handler, err := v3.NewHandler(v3Server, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build v3 handler for inventory validation: %w", err)
	}
	return ValidateCustomRouterInventoryWithV3Handler(outerRouter, v3Handler, cfg, variant)
}

// ValidateCustomRouterInventoryWithV3Handler walks custom outer router and an explicit v3Handler instance.
func ValidateCustomRouterInventoryWithV3Handler(outerRouter chi.Router, v3Handler http.Handler, cfg config.AppConfig, variant ConfigVariant) ([]RouteRegistration, error) {
	var registrations []RouteRegistration
	seenOrdinals := make(map[string]int)
	evidenceRegistry := GetDefaultPhase1VerifiedEvidenceRegistry()

	// 1. Walk Outer Router (ignoring delegating /api/v3/* mount)
	err := chi.Walk(outerRouter, func(method string, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/api/v3/*" {
			return nil // Skip delegate mount point
		}
		routerID := "outer"
		pairKey := fmt.Sprintf("%s:%s:%s", routerID, method, route)
		ordinal := seenOrdinals[pairKey]
		seenOrdinals[pairKey]++

		// Separate Policy & Capability sources
		policy := ClassifyRoute(routerID, variant, method, route)
		stackCfg := StackConfig{
			RouterID:         routerID,
			UIMode:           variant,
			RateLimitEnabled: cfg.RateLimitEnabled,
			Compression:      true,
		}
		declaredCaps, err := DeclaredCapabilitiesForStack(stackCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve declared capabilities for outer stack: %w", err)
		}
		caps := ApplyVerifiedEvidence(declaredCaps, evidenceRegistry)

		ptr := uintptr(0)
		if val := reflect.ValueOf(handler); val.IsValid() && val.Kind() == reflect.Func {
			ptr = val.Pointer()
		}

		registrations = append(registrations, RouteRegistration{
			Key: RegistrationKey{
				RouterID: routerID,
				Method:   method,
				Pattern:  route,
				Ordinal:  ordinal,
			},
			HandlerType:   fmt.Sprintf("%T", handler),
			Pointer:       ptr,
			Policy:        policy,
			ConfigVariant: variant,
			Capabilities:  caps,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk outer router inventory: %w", err)
	}

	// 2. Walk v3 Subrouter
	if v3Router, ok := v3Handler.(chi.Router); ok {
		err = chi.Walk(v3Router, func(method string, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
			fullPattern := route
			if !strings.HasPrefix(fullPattern, "/api/v3") {
				fullPattern = "/api/v3" + fullPattern
			}
			routerID := "v3"
			pairKey := fmt.Sprintf("%s:%s:%s", routerID, method, fullPattern)
			ordinal := seenOrdinals[pairKey]
			seenOrdinals[pairKey]++

			// Separate Policy & Capability sources
			policy := ClassifyRoute(routerID, variant, method, fullPattern)
			stackCfg := StackConfig{
				RouterID:         routerID,
				UIMode:           variant,
				RateLimitEnabled: cfg.RateLimitEnabled,
				Compression:      true,
			}
			declaredCaps, err := DeclaredCapabilitiesForStack(stackCfg)
			if err != nil {
				return fmt.Errorf("failed to resolve declared capabilities for v3 stack: %w", err)
			}
			caps := ApplyVerifiedEvidence(declaredCaps, evidenceRegistry)

			ptr := uintptr(0)
			if val := reflect.ValueOf(handler); val.IsValid() && val.Kind() == reflect.Func {
				ptr = val.Pointer()
			}

			registrations = append(registrations, RouteRegistration{
				Key: RegistrationKey{
					RouterID: routerID,
					Method:   method,
					Pattern:  fullPattern,
					Ordinal:  ordinal,
				},
				HandlerType:   fmt.Sprintf("%T", handler),
				Pointer:       ptr,
				Policy:        policy,
				ConfigVariant: variant,
				Capabilities:  caps,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk v3 subrouter inventory: %w", err)
		}
	}

	for _, reg := range registrations {
		if err := reg.ValidateStructural(); err != nil {
			return nil, fmt.Errorf("invalid route structural registration in inventory: %w", err)
		}
		if err := reg.ValidateDeclaredCompatibility(); err != nil {
			return nil, fmt.Errorf("invalid route declared capability compatibility in inventory: %w", err)
		}
	}

	return registrations, nil
}

func TestOuterStack_ResponseControllerPassThrough(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	r := s.buildRouter()

	ts := httptest.NewServer(r)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestOuterStack_WithCompressionPassThrough(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	r := s.buildRouter()

	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))

	gzReader, err := gzip.NewReader(resp.Body)
	require.NoError(t, err)
	defer gzReader.Close()

	body, err := io.ReadAll(gzReader)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"status":`)
}

func TestVerifiedStackEvidence_MappingAndIsolation(t *testing.T) {
	declared := DeclaredMiddlewareCapabilities{
		EquivalenceClass:          EquivalenceClassOuterStandard,
		SetWriteDeadline:          CapabilityDeclared,
		Flush:                     CapabilityDeclared,
		Hijack:                    CapabilityDeclared,
		UpgradeDeadlineTransition: CapabilityDeclared,
	}

	t.Run("Without evidence, capabilities remain CapabilityDeclared", func(t *testing.T) {
		registry := map[ResponseWriterEquivalenceClass]VerifiedStackEvidence{}
		caps := ApplyVerifiedEvidence(declared, registry)
		assert.Equal(t, CapabilityDeclared, caps.SetWriteDeadline)
		assert.Equal(t, CapabilityDeclared, caps.Flush)
		assert.Equal(t, CapabilityDeclared, caps.Hijack)
		assert.Equal(t, CapabilityDeclared, caps.UpgradeDeadlineTransition)
	})

	t.Run("Evidence for a different equivalence class is NOT applied", func(t *testing.T) {
		registry := map[ResponseWriterEquivalenceClass]VerifiedStackEvidence{
			EquivalenceClassV3Compressed: {
				EquivalenceClass:         EquivalenceClassV3Compressed,
				SetWriteDeadlineVerified: true,
				FlushVerified:            true,
			},
		}
		caps := ApplyVerifiedEvidence(declared, registry)
		assert.Equal(t, CapabilityDeclared, caps.SetWriteDeadline, "evidence for v3-compressed must not alter outer-standard capabilities")
		assert.Equal(t, CapabilityDeclared, caps.Flush)
	})

	t.Run("Evidence for matching equivalence class elevates capabilities to CapabilityVerified", func(t *testing.T) {
		registry := map[ResponseWriterEquivalenceClass]VerifiedStackEvidence{
			EquivalenceClassOuterStandard: {
				EquivalenceClass:         EquivalenceClassOuterStandard,
				SetWriteDeadlineVerified: true,
				FlushVerified:            true,
			},
		}
		caps := ApplyVerifiedEvidence(declared, registry)
		assert.Equal(t, CapabilityVerified, caps.SetWriteDeadline)
		assert.Equal(t, CapabilityVerified, caps.Flush)
		assert.Equal(t, CapabilityDeclared, caps.Hijack, "unverified Hijack remains CapabilityDeclared")
	})
}

func TestDeadlineClassification_NegativeValidation(t *testing.T) {
	t.Run("Default empty RouteRegistration is invalid", func(t *testing.T) {
		reg := RouteRegistration{}
		err := reg.ValidateStructural()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty method")
	})

	t.Run("RouteDeadlineUnknown (zero value) is invalid", func(t *testing.T) {
		reg := RouteRegistration{
			Key: RegistrationKey{
				RouterID: "outer",
				Method:   "GET",
				Pattern:  "/test",
				Ordinal:  0,
			},
			Policy: RoutePolicy{Class: RouteDeadlineUnknown},
		}
		err := reg.ValidateStructural()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unclassified route (RouteDeadlineUnknown)")
	})

	t.Run("Unknown numeric enum value is invalid", func(t *testing.T) {
		reg := RouteRegistration{
			Key: RegistrationKey{
				RouterID: "outer",
				Method:   "GET",
				Pattern:  "/test",
				Ordinal:  0,
			},
			Policy: RoutePolicy{Class: RouteDeadlineClass(255)},
		}
		err := reg.ValidateStructural()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid numeric RouteDeadlineClass")
	})

	t.Run("Four-state CapabilityState string representation", func(t *testing.T) {
		assert.Equal(t, "Unknown", CapabilityUnknown.String())
		assert.Equal(t, "Unsupported", CapabilityUnsupported.String())
		assert.Equal(t, "Declared", CapabilityDeclared.String())
		assert.Equal(t, "Verified", CapabilityVerified.String())
		assert.Equal(t, "UnknownCapabilityState(255)", CapabilityState(255).String())
	})
}

func TestRouteRegistration_MayUpgradeRequiresHijackCapability(t *testing.T) {
	t.Run("Route requesting MayUpgradePerRequest fails declared compatibility when stack has CapabilityUnsupported Hijack", func(t *testing.T) {
		reg := RouteRegistration{
			Key: RegistrationKey{
				RouterID: "outer",
				Method:   "GET",
				Pattern:  "/ui/*",
				Ordinal:  0,
			},
			Policy: RoutePolicy{
				Class:                RouteDeadlineMediaBounded,
				MayUpgradePerRequest: true,
			},
			Capabilities: DeclaredMiddlewareCapabilities{
				SetWriteDeadline: CapabilityDeclared,
				Flush:            CapabilityDeclared,
				Hijack:           CapabilityUnsupported, // Stack unsupported
			},
		}

		err := reg.ValidateDeclaredCompatibility()
		require.Error(t, err, "must reject route demanding MayUpgradePerRequest when stack has Unsupported Hijack capability")
		assert.Contains(t, err.Error(), "requires declared or verified Hijack capability (got Unsupported)")
	})

	t.Run("Route requesting MayUpgradePerRequest passes declared compatibility but pending runtime readiness when Hijack is Declared", func(t *testing.T) {
		reg := RouteRegistration{
			Key: RegistrationKey{
				RouterID: "outer",
				Method:   "GET",
				Pattern:  "/ui/*",
				Ordinal:  0,
			},
			Policy: RoutePolicy{
				Class:                RouteDeadlineMediaBounded,
				MayUpgradePerRequest: true,
			},
			Capabilities: DeclaredMiddlewareCapabilities{
				SetWriteDeadline:          CapabilityVerified,
				Flush:                     CapabilityVerified,
				Hijack:                    CapabilityDeclared, // Target declared expectation in Phase 1
				UpgradeDeadlineTransition: CapabilityDeclared,
			},
		}

		err := reg.ValidateDeclaredCompatibility()
		require.NoError(t, err, "must pass declared compatibility gate in Phase 1")

		errRuntime := reg.ValidateRuntimeReadiness()
		require.Error(t, errRuntime, "must report pending Phase 2 empirical hijack verification")
		assert.Contains(t, errRuntime.Error(), "pending Phase 2 empirical hijack verification (got Declared)")
	})

	t.Run("SSE route requiring RequiresFlush fails runtime readiness when Flush is only CapabilityDeclared", func(t *testing.T) {
		reg := RouteRegistration{
			Key: RegistrationKey{
				RouterID: "v3",
				Method:   "GET",
				Pattern:  "/api/v3/sessions/{sessionID}/events",
				Ordinal:  0,
			},
			Policy: RoutePolicy{
				Class:         RouteDeadlineStreaming,
				RequiresFlush: true,
			},
			Capabilities: DeclaredMiddlewareCapabilities{
				SetWriteDeadline: CapabilityVerified,
				Flush:            CapabilityDeclared, // Unverified Flush
			},
		}

		err := reg.ValidateDeclaredCompatibility()
		require.NoError(t, err)

		errRuntime := reg.ValidateRuntimeReadiness()
		require.Error(t, errRuntime, "SSE route must require verified Flush capability for runtime readiness")
		assert.Contains(t, errRuntime.Error(), "pending empirical Flush verification (got Declared)")
	})
}

func TestDeclaredCapabilitiesForStack_TableAndNegativeValidation(t *testing.T) {
	t.Run("Negative validation for empty or invalid StackConfig", func(t *testing.T) {
		_, err := DeclaredCapabilitiesForStack(StackConfig{})
		require.Error(t, err, "empty StackConfig must return error (fail-closed)")
		assert.Contains(t, err.Error(), "invalid router ID")

		_, err = DeclaredCapabilitiesForStack(StackConfig{RouterID: "invalid"})
		require.Error(t, err, "invalid RouterID must return error")
		assert.Contains(t, err.Error(), "invalid router ID")

		_, err = DeclaredCapabilitiesForStack(StackConfig{RouterID: "outer", UIMode: "invalid-ui-mode"})
		require.Error(t, err, "invalid UIMode must return error")
		assert.Contains(t, err.Error(), "invalid UI mode variant")
	})

	t.Run("Table-driven validation across all 24 declarative stack combinations", func(t *testing.T) {
		routerIDs := []string{"outer", "v3"}
		uiModes := []ConfigVariant{ConfigVariantProdStatic, ConfigVariantDevDir, ConfigVariantDevProxy}
		rateLimitStates := []bool{true, false}
		compressionStates := []bool{true, false}

		var count int
		for _, rID := range routerIDs {
			for _, ui := range uiModes {
				for _, rl := range rateLimitStates {
					for _, comp := range compressionStates {
						count++
						name := fmt.Sprintf("Case=%d_Router=%s_UI=%s_RL=%t_Comp=%t", count, rID, ui, rl, comp)
						t.Run(name, func(t *testing.T) {
							stack := StackConfig{
								RouterID:         rID,
								UIMode:           ui,
								RateLimitEnabled: rl,
								Compression:      comp,
							}
							caps, err := DeclaredCapabilitiesForStack(stack)
							require.NoError(t, err)

							// Metadata stack function returns CapabilityDeclared for target capabilities across all 24 configurations
							assert.Equal(t, CapabilityDeclared, caps.SetWriteDeadline)
							assert.Equal(t, CapabilityDeclared, caps.Flush)
							assert.Equal(t, CapabilityDeclared, caps.Hijack)
							assert.Equal(t, CapabilityDeclared, caps.UpgradeDeadlineTransition)
						})
					}
				}
			}
		}
		assert.Equal(t, 24, count, "must execute exactly 24 distinct declarative stack configuration cases reducing to 4 ResponseWriter equivalence classes")
	})
}

func TestDeadlineClassification_DevProxyReportsPendingHijackVerification(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	regs, err := ValidateRouterInventory(s, ConfigVariantDevProxy)
	require.NoError(t, err, "DevProxy inventory walk must pass structural and declared compatibility validation")
	assert.Equal(t, 157, len(regs))

	var totalCount, structuralCount, declaredCount int
	var runtimeReadyBoundedOrStreamingCount, pendingUpgradeCount, runtimeReadyUpgradeCount int
	var declaredUpgradeRoutes []string

	for _, r := range regs {
		totalCount++

		if err := r.ValidateStructural(); err == nil {
			structuralCount++
		}
		if err := r.ValidateDeclaredCompatibility(); err == nil {
			declaredCount++
		}

		if r.Policy.MayUpgradePerRequest {
			declaredUpgradeRoutes = append(declaredUpgradeRoutes, fmt.Sprintf("%s %s", r.Key.Method, r.Key.Pattern))
			errRuntime := r.ValidateRuntimeReadiness()
			if errRuntime != nil {
				pendingUpgradeCount++
				assert.Contains(t, errRuntime.Error(), "pending Phase 2 empirical hijack verification")
			} else {
				runtimeReadyUpgradeCount++
			}
		} else {
			if errRuntime := r.ValidateRuntimeReadiness(); errRuntime == nil {
				runtimeReadyBoundedOrStreamingCount++
			}
		}
	}

	t.Logf("DevProxy Readiness Diagnostics:")
	t.Logf("Total registrations:                    %d", totalCount)
	t.Logf("Structurally valid:                     %d", structuralCount)
	t.Logf("Declared-compatible:                    %d", declaredCount)
	t.Logf("Runtime-ready bounded/streaming routes: %d", runtimeReadyBoundedOrStreamingCount)
	t.Logf("Pending upgrade routes:                 %d", pendingUpgradeCount)
	t.Logf("Runtime-ready upgrade routes:           %d", runtimeReadyUpgradeCount)

	assert.Equal(t, 157, totalCount)
	assert.Equal(t, 157, structuralCount)
	assert.Equal(t, 157, declaredCount)
	assert.Equal(t, 155, runtimeReadyBoundedOrStreamingCount, "all 155 bounded and streaming routes must be runtime-ready with verified evidence")
	assert.Equal(t, 2, pendingUpgradeCount, "exactly 2 DevProxy MayUpgradePerRequest routes must report pending Phase 2 empirical hijack verification")
	assert.Equal(t, 0, runtimeReadyUpgradeCount, "0 upgrade routes are runtime-ready until Phase 2 empirical hijack probe")
	assert.Contains(t, declaredUpgradeRoutes, "GET /ui")
	assert.Contains(t, declaredUpgradeRoutes, "GET /ui/*")
}

func TestDeadlineClassification_RejectsNewUnclassifiedRoute(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	t.Run("Mutation A: New unclassified path on outer router fails validation gate", func(t *testing.T) {
		outerRouter := s.buildRouter()
		outerRouter.Get("/__deadline-unclassified", func(w http.ResponseWriter, r *http.Request) {})

		_, err := ValidateCustomRouterInventory(outerRouter, s.v3Handler, s.cfg, ConfigVariantProdStatic)
		require.Error(t, err, "must reject new unclassified outer route")
		assert.Contains(t, err.Error(), "unclassified route (RouteDeadlineUnknown)")
		assert.Contains(t, err.Error(), "GET /__deadline-unclassified")
	})

	t.Run("Mutation B: New unclassified path on v3 subrouter fails validation gate", func(t *testing.T) {
		outerRouter := s.buildRouter()
		v3Handler, err := v3.NewHandler(s.v3Handler, s.cfg)
		require.NoError(t, err)

		if v3Router, ok := v3Handler.(chi.Router); ok {
			v3Router.Get("/__deadline-unclassified", func(w http.ResponseWriter, r *http.Request) {})

			_, err := ValidateCustomRouterInventoryWithV3Handler(outerRouter, v3Handler, s.cfg, ConfigVariantProdStatic)
			require.Error(t, err, "must reject new unclassified v3 route")
			assert.Contains(t, err.Error(), "unclassified route (RouteDeadlineUnknown)")
			assert.Contains(t, err.Error(), "GET /api/v3/__deadline-unclassified")
		}
	})

	t.Run("Mutation C: Known path with unclassified method fails validation gate", func(t *testing.T) {
		outerRouter := s.buildRouter()
		v3Handler, err := v3.NewHandler(s.v3Handler, s.cfg)
		require.NoError(t, err)

		if v3Router, ok := v3Handler.(chi.Router); ok {
			v3Router.Post("/sessions/{sessionID}/events", func(w http.ResponseWriter, r *http.Request) {})

			_, err := ValidateCustomRouterInventoryWithV3Handler(outerRouter, v3Handler, s.cfg, ConfigVariantProdStatic)
			require.Error(t, err, "must reject POST on streaming events path")
			assert.Contains(t, err.Error(), "unclassified route (RouteDeadlineUnknown)")
			assert.Contains(t, err.Error(), "POST /api/v3/sessions/{sessionID}/events")
		}
	})
}

// TestAssignRegistrationOrdinals_SyntheticInventory validates ordinal numbering for duplicate registrations in an inventory list.
// NOTE: Ordinals distinguish multiple registrations of the same method and pattern IF they are visible as separate items in the walked inventory.
// The walker cannot reconstruct earlier handler registrations that were already overwritten on the same chi.Router node.
func TestAssignRegistrationOrdinals_SyntheticInventory(t *testing.T) {
	seenOrdinals := make(map[string]int)
	getOrdinal := func(routerID, method, pattern string) int {
		key := fmt.Sprintf("%s:%s:%s", routerID, method, pattern)
		ord := seenOrdinals[key]
		seenOrdinals[key]++
		return ord
	}

	ord1 := getOrdinal("v3", "GET", "/api/v3/test")
	assert.Equal(t, 0, ord1)

	ord2 := getOrdinal("v3", "POST", "/api/v3/test")
	assert.Equal(t, 0, ord2)

	ord3 := getOrdinal("v3", "GET", "/api/v3/test")
	assert.Equal(t, 1, ord3)

	ord4 := getOrdinal("outer", "GET", "/api/v3/test")
	assert.Equal(t, 0, ord4)
}

func TestDeadlineClassification_RouterMatrix(t *testing.T) {
	variants := []ConfigVariant{
		ConfigVariantProdStatic,
		ConfigVariantDevDir,
		ConfigVariantDevProxy,
	}
	rateLimitStates := []bool{true, false}

	for _, variant := range variants {
		for _, rl := range rateLimitStates {
			name := fmt.Sprintf("Variant=%s_RateLimit=%t", variant, rl)
			t.Run(name, func(t *testing.T) {
				cfg := config.AppConfig{
					APIToken:         "admin-token",
					APITokenScopes:   []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
					RateLimitEnabled: rl,
				}
				s := mustNewServer(t, cfg, config.NewManager(""))

				regs, err := ValidateRouterInventory(s, variant)
				require.NoError(t, err)
				require.NotEmpty(t, regs)

				for _, reg := range regs {
					require.NoError(t, reg.ValidateStructural(), "every route registration in inventory must pass structural validation")
					require.NoError(t, reg.ValidateDeclaredCompatibility(), "every route registration in inventory must pass declared capability compatibility validation")
				}
			})
		}
	}
}

func TestDeadlineClassification_InventoryCountsAndClassificationList(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	variants := []ConfigVariant{
		ConfigVariantProdStatic,
		ConfigVariantDevDir,
		ConfigVariantDevProxy,
	}

	for _, variant := range variants {
		t.Run(string(variant), func(t *testing.T) {
			regs, err := ValidateRouterInventory(s, variant)
			require.NoError(t, err)

			var streaming, mediaBounded, apiBounded int
			var mayUpgradeCount int

			for _, r := range regs {
				switch r.Policy.Class {
				case RouteDeadlineStreaming:
					streaming++
				case RouteDeadlineMediaBounded:
					mediaBounded++
				case RouteDeadlineAPIBounded:
					apiBounded++
				}
				if r.Policy.MayUpgradePerRequest {
					mayUpgradeCount++
				}
			}

			t.Logf("Variant: %s -> Total: %d [APIBounded: %d, MediaBounded: %d, Streaming: %d, MayUpgradePerRequest: %d]",
				variant, len(regs), apiBounded, mediaBounded, streaming, mayUpgradeCount)

			if variant == ConfigVariantDevProxy {
				assert.Equal(t, 157, len(regs), "total registrable instances must equal 157 under DevProxy")
				assert.Equal(t, 157, apiBounded+mediaBounded+streaming)
				assert.Equal(t, 12, mediaBounded, "RouteDeadlineMediaBounded count is 12 under DevProxy")
				assert.Equal(t, 142, apiBounded, "RouteDeadlineAPIBounded count is 142 under DevProxy")
				assert.Equal(t, 2, mayUpgradeCount, "DevProxy has 2 MayUpgradePerRequest routes (GET /ui and GET /ui/*)")
			} else {
				assert.Equal(t, 157, len(regs), "total registrable instances must equal 157 under ProdStatic/DevDir")
				assert.Equal(t, 157, apiBounded+mediaBounded+streaming)
				assert.Equal(t, 13, mediaBounded, "RouteDeadlineMediaBounded count is 13 under ProdStatic/DevDir")
				assert.Equal(t, 141, apiBounded, "RouteDeadlineAPIBounded count is 141 under ProdStatic/DevDir")
				assert.Equal(t, 0, mayUpgradeCount)
			}
		})
	}

	regs, err := ValidateRouterInventory(s, ConfigVariantProdStatic)
	require.NoError(t, err)

	for _, r := range regs {
		if r.Key.Method == "HEAD" && r.Key.Pattern == "/api/v3/recordings/{recordingId}/stream.mp4" {
			assert.Equal(t, RouteDeadlineMediaBounded, r.Policy.Class, "HEAD stream.mp4 must be RouteDeadlineMediaBounded")
		}
	}
}

func TestDeadlineClassification_RawInventoryDiagnostics(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	outerRouter := s.buildRouter()
	v3Handler, err := v3.NewHandler(s.v3Handler, s.cfg)
	require.NoError(t, err)

	var rawOuterCount, rawV3Count, filteredDelegateCount int
	var filteredDelegates []string

	_ = chi.Walk(outerRouter, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		rawOuterCount++
		if route == "/api/v3/*" {
			filteredDelegateCount++
			filteredDelegates = append(filteredDelegates, fmt.Sprintf("%s %s", method, route))
		}
		return nil
	})

	if v3Router, ok := v3Handler.(chi.Router); ok {
		_ = chi.Walk(v3Router, func(_ string, _ string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			rawV3Count++
			return nil
		})
	}

	t.Logf("=== Raw Inventory Diagnostics ===")
	t.Logf("Raw Outer Walk Entries: %d (26 classifiable routes + %d filtered delegate mount methods)", rawOuterCount, filteredDelegateCount)
	t.Logf("Raw V3 Walk Entries: %d", rawV3Count)
	t.Logf("Filtered Delegate Mounts (%d): %v", filteredDelegateCount, filteredDelegates)
	t.Logf("Classifiable Outer Registrations: %d", rawOuterCount-filteredDelegateCount)
	t.Logf("Classifiable V3 Registrations: %d", rawV3Count)
	t.Logf("Combined Classifiable Registrations: %d", (rawOuterCount-filteredDelegateCount)+rawV3Count)

	assert.Equal(t, 35, rawOuterCount, "raw outer walk contains 26 routes + 9 method expansions of /api/v3/* delegate mount")
	assert.Equal(t, 120, rawV3Count, "raw v3 walk contains 120 routes")
	assert.Equal(t, 9, filteredDelegateCount, "chi expands /api/v3/* wildcard mount to 9 HTTP methods")
	assert.Equal(t, 26, rawOuterCount-filteredDelegateCount)
	assert.Equal(t, 146, (rawOuterCount-filteredDelegateCount)+rawV3Count)
}

func TestDeadlineClassification_MethodSpecificUI(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	t.Run("ProdStatic UI method-specific classification", func(t *testing.T) {
		regs, err := ValidateRouterInventory(s, ConfigVariantProdStatic)
		require.NoError(t, err)

		for _, r := range regs {
			if strings.HasPrefix(r.Key.Pattern, "/ui") {
				if r.Key.Method == http.MethodGet || r.Key.Method == http.MethodHead {
					assert.Equal(t, RouteDeadlineMediaBounded, r.Policy.Class, "GET/HEAD /ui must be MediaBounded")
				} else {
					assert.Equal(t, RouteDeadlineAPIBounded, r.Policy.Class, "non-GET/HEAD /ui in prod must be APIBounded")
				}
				assert.Equal(t, CapabilityDeclared, r.Capabilities.Hijack, "When evidence registry is applied, Hijack state remains CapabilityDeclared in Phase 1")
				assert.False(t, r.Policy.MayUpgradePerRequest)
			}
		}
	})

	t.Run("DevProxy UI method-specific classification", func(t *testing.T) {
		regs, err := ValidateRouterInventory(s, ConfigVariantDevProxy)
		require.NoError(t, err)

		for _, r := range regs {
			if strings.HasPrefix(r.Key.Pattern, "/ui") {
				if r.Key.Method == http.MethodGet {
					assert.Equal(t, RouteDeadlineMediaBounded, r.Policy.Class, "GET /ui under DevProxy is MediaBounded by default for normal HTTP GETs")
					assert.True(t, r.Policy.MayUpgradePerRequest, "GET /ui under DevProxy may upgrade per request when Upgrade headers are present")
				} else {
					assert.Equal(t, RouteDeadlineAPIBounded, r.Policy.Class, "non-GET /ui under DevProxy is APIBounded")
					assert.False(t, r.Policy.MayUpgradePerRequest, "non-GET /ui does not support per-request upgrade policy")
				}
			}
		}
	})
}
