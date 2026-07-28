// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package api

import (
	"fmt"
	"net/http"
	"strings"
)

// RouteDeadlineClass categorizes static routes by their write deadline and connection lifetime requirements.
// NOTE: RouteDeadlineUnknown is 0 so that uninitialized zero-values are explicitly invalid.
type RouteDeadlineClass uint8

const (
	// RouteDeadlineUnknown represents an unclassified or uninitialized route deadline class.
	// This zero-value MUST be rejected by inventory validation gates.
	RouteDeadlineUnknown RouteDeadlineClass = iota
	// RouteDeadlineAPIBounded represents normal JSON/REST endpoints with a tight, bounded runtime.
	RouteDeadlineAPIBounded
	// RouteDeadlineMediaBounded represents file/media serving endpoints (e.g. HLS segments, static UI files, media metadata probes) with fixed content or bounded runtime.
	RouteDeadlineMediaBounded
	// RouteDeadlineStreaming represents explicitly unbounded streams (SSE, continuous mp4 streaming).
	RouteDeadlineStreaming
)

func (c RouteDeadlineClass) String() string {
	switch c {
	case RouteDeadlineUnknown:
		return "Unknown"
	case RouteDeadlineAPIBounded:
		return "APIBounded"
	case RouteDeadlineMediaBounded:
		return "MediaBounded"
	case RouteDeadlineStreaming:
		return "Streaming"
	default:
		return fmt.Sprintf("UnknownRouteDeadlineClass(%d)", c)
	}
}

// CapabilityState represents a four-stage capability verification lifecycle model.
type CapabilityState uint8

const (
	// CapabilityUnknown indicates an uninitialized capability state.
	CapabilityUnknown CapabilityState = iota
	// CapabilityUnsupported indicates that the stack structurally cannot support the capability.
	CapabilityUnsupported
	// CapabilityDeclared indicates a target declared expectation that is structurally required but pending Phase 2 empirical runtime verification.
	CapabilityDeclared
	// CapabilityVerified indicates a capability that has been empirically verified via runtime TCP/HTTP probes.
	CapabilityVerified
)

func (s CapabilityState) String() string {
	switch s {
	case CapabilityUnknown:
		return "Unknown"
	case CapabilityUnsupported:
		return "Unsupported"
	case CapabilityDeclared:
		return "Declared"
	case CapabilityVerified:
		return "Verified"
	default:
		return fmt.Sprintf("UnknownCapabilityState(%d)", uint8(s))
	}
}

// ResponseWriterEquivalenceClass uniquely identifies a distinct ResponseWriter wrapper stack construction.
type ResponseWriterEquivalenceClass string

const (
	EquivalenceClassOuterStandard   ResponseWriterEquivalenceClass = "outer-standard"
	EquivalenceClassOuterCompressed ResponseWriterEquivalenceClass = "outer-compressed"
	EquivalenceClassV3Standard      ResponseWriterEquivalenceClass = "v3-standard"
	EquivalenceClassV3Compressed    ResponseWriterEquivalenceClass = "v3-compressed"
)

// VerifiedStackEvidence records empirical proof gathered from HTTP/TCP probes for a specific ResponseWriter equivalence class.
type VerifiedStackEvidence struct {
	EquivalenceClass          ResponseWriterEquivalenceClass
	SetWriteDeadlineVerified  bool
	FlushVerified             bool
	HijackVerified            bool
	UpgradeTransitionVerified bool
}

// ConfigVariant distinguishes the concrete configuration variant under which the router is built.
type ConfigVariant string

const (
	ConfigVariantProdStatic ConfigVariant = "ui-prod-static"
	ConfigVariantDevDir     ConfigVariant = "ui-dev-dir"
	ConfigVariantDevProxy   ConfigVariant = "ui-dev-proxy"
)

// RoutePolicy captures route-specific static deadline classification and request-dependent capabilities.
type RoutePolicy struct {
	Class                RouteDeadlineClass
	RequiresFlush        bool // Set to true for SSE endpoints (GET /api/v3/sessions/{sessionID}/events)
	MayUpgradePerRequest bool // Set to true for GET /ui and GET /ui/* under DevProxy
}

// StackConfig describes the concrete middleware stack configuration for capability resolution.
type StackConfig struct {
	RouterID         string        // "outer" or "v3"
	UIMode           ConfigVariant // ConfigVariantProdStatic, ConfigVariantDevDir, ConfigVariantDevProxy
	RateLimitEnabled bool
	Compression      bool
}

// EquivalenceClass returns the ResponseWriterEquivalenceClass for a StackConfig.
func (s StackConfig) EquivalenceClass() ResponseWriterEquivalenceClass {
	if s.RouterID == "v3" {
		if s.Compression {
			return EquivalenceClassV3Compressed
		}
		return EquivalenceClassV3Standard
	}
	if s.Compression {
		return EquivalenceClassOuterCompressed
	}
	return EquivalenceClassOuterStandard
}

// DeclaredMiddlewareCapabilities describes declared capability expectations for a router stack.
type DeclaredMiddlewareCapabilities struct {
	EquivalenceClass          ResponseWriterEquivalenceClass
	SetWriteDeadline          CapabilityState
	Flush                     CapabilityState
	Hijack                    CapabilityState
	UpgradeDeadlineTransition CapabilityState
}

// RegistrationKey uniquely identifies a registration instance within a router inventory.
type RegistrationKey struct {
	RouterID string
	Method   string
	Pattern  string
	Ordinal  int // 0-indexed per (RouterID, Method, Pattern) pair
}

func (k RegistrationKey) String() string {
	return fmt.Sprintf("[%s#%d] %s %s", k.RouterID, k.Ordinal, k.Method, k.Pattern)
}

// RouteRegistration captures metadata for a single route registration instance within a router setup.
type RouteRegistration struct {
	Key           RegistrationKey
	HandlerType   string  // Diagnostic type string (e.g. "http.HandlerFunc")
	Pointer       uintptr // Diagnostic function pointer
	Policy        RoutePolicy
	ConfigVariant ConfigVariant
	Capabilities  DeclaredMiddlewareCapabilities
}

// ValidateStructural checks whether a RouteRegistration contains valid non-zero attributes and a known RouteDeadlineClass (Phase 1 structural gate).
func (r RouteRegistration) ValidateStructural() error {
	if r.Key.Method == "" {
		return fmt.Errorf("empty method in registration key")
	}
	if r.Key.Pattern == "" {
		return fmt.Errorf("empty pattern in registration key")
	}
	if r.Policy.Class == RouteDeadlineUnknown {
		return fmt.Errorf("unclassified route (RouteDeadlineUnknown) for %s", r.Key)
	}
	if r.Policy.Class > RouteDeadlineStreaming {
		return fmt.Errorf("invalid numeric RouteDeadlineClass (%d) for %s", uint8(r.Policy.Class), r.Key)
	}
	return nil
}

// ValidateDeclaredCompatibility checks whether a route's policy requirements match the declared target capabilities of the stack (Phase 1 declarative gate).
func (r RouteRegistration) ValidateDeclaredCompatibility() error {
	if err := r.ValidateStructural(); err != nil {
		return err
	}
	if r.Policy.Class == RouteDeadlineStreaming {
		if r.Capabilities.SetWriteDeadline != CapabilityDeclared && r.Capabilities.SetWriteDeadline != CapabilityVerified {
			return fmt.Errorf("streaming route %s requires declared or verified SetWriteDeadline capability (got %s)", r.Key, r.Capabilities.SetWriteDeadline)
		}
	}
	if r.Policy.RequiresFlush {
		if r.Capabilities.Flush != CapabilityDeclared && r.Capabilities.Flush != CapabilityVerified {
			return fmt.Errorf("flushing route %s requires declared or verified Flush capability (got %s)", r.Key, r.Capabilities.Flush)
		}
	}
	if r.Policy.MayUpgradePerRequest {
		if r.Capabilities.Hijack != CapabilityDeclared && r.Capabilities.Hijack != CapabilityVerified {
			return fmt.Errorf("upgrade route %s requires declared or verified Hijack capability (got %s)", r.Key, r.Capabilities.Hijack)
		}
		if r.Capabilities.UpgradeDeadlineTransition != CapabilityDeclared && r.Capabilities.UpgradeDeadlineTransition != CapabilityVerified {
			return fmt.Errorf("upgrade route %s requires declared or verified UpgradeDeadlineTransition capability (got %s)", r.Key, r.Capabilities.UpgradeDeadlineTransition)
		}
	}
	return nil
}

// ValidateRuntimeReadiness checks whether all required capabilities of a route are empirically verified by registered evidence for its ResponseWriter equivalence class (Phase 2 readiness gate).
func (r RouteRegistration) ValidateRuntimeReadiness() error {
	if err := r.ValidateDeclaredCompatibility(); err != nil {
		return err
	}
	if r.Policy.Class == RouteDeadlineStreaming {
		if r.Capabilities.SetWriteDeadline != CapabilityVerified {
			return fmt.Errorf("streaming route %s pending empirical SetWriteDeadline verification (got %s)", r.Key, r.Capabilities.SetWriteDeadline)
		}
	}
	if r.Policy.RequiresFlush {
		if r.Capabilities.Flush != CapabilityVerified {
			return fmt.Errorf("flushing route %s pending empirical Flush verification (got %s)", r.Key, r.Capabilities.Flush)
		}
	}
	if r.Policy.MayUpgradePerRequest {
		if r.Capabilities.Hijack != CapabilityVerified {
			return fmt.Errorf("upgrade route %s pending Phase 2 empirical hijack verification (got %s)", r.Key, r.Capabilities.Hijack)
		}
		if r.Capabilities.UpgradeDeadlineTransition != CapabilityVerified {
			return fmt.Errorf("upgrade route %s pending Phase 2 empirical upgrade deadline transition verification (got %s)", r.Key, r.Capabilities.UpgradeDeadlineTransition)
		}
	}
	return nil
}

// Validate maintains backward compatibility by executing structural validation.
func (r RouteRegistration) Validate() error {
	return r.ValidateStructural()
}

// ClassifyRoute determines the RoutePolicy for a given route registration instance.
// NOTE: This function serves as the canonical policy source of truth for route deadline classification in xg2g.
func ClassifyRoute(routerID string, variant ConfigVariant, method string, pattern string) RoutePolicy {
	// Guard: Explicitly reject known unclassified/test mutation patterns
	if strings.Contains(pattern, "__deadline-unclassified") {
		return RoutePolicy{Class: RouteDeadlineUnknown}
	}

	// 1. Unbounded Streaming routes (ONLY GET requests for SSE sessions/events and stream.mp4)
	// NOTE: SSE sessions/events route requires explicit Flush capability (RequiresFlush = true).
	if method == http.MethodGet {
		if strings.HasSuffix(pattern, "/events") {
			return RoutePolicy{
				Class:                RouteDeadlineStreaming,
				RequiresFlush:        true,
				MayUpgradePerRequest: false,
			}
		}
		if strings.HasSuffix(pattern, "/stream.mp4") {
			return RoutePolicy{
				Class:                RouteDeadlineStreaming,
				RequiresFlush:        false,
				MayUpgradePerRequest: false,
			}
		}
	}

	// 2. DevProxy UI routes
	if strings.HasPrefix(pattern, "/ui") && variant == ConfigVariantDevProxy {
		if method == http.MethodGet {
			return RoutePolicy{
				Class:                RouteDeadlineMediaBounded,
				RequiresFlush:        false,
				MayUpgradePerRequest: true,
			}
		}
		return RoutePolicy{
			Class:                RouteDeadlineAPIBounded,
			RequiresFlush:        false,
			MayUpgradePerRequest: false,
		}
	}

	// 3. Media Bounded routes (GET/HEAD requests for HLS segments, picon images, static UI files, and HEAD stream.mp4 probes)
	// HEAD /stream.mp4 resolves file readiness, reads os.Stat, and prepares range/content headers without body copy.
	if (method == http.MethodGet || method == http.MethodHead) &&
		(strings.HasPrefix(pattern, "/logos/") || strings.Contains(pattern, "/hls") || strings.HasPrefix(pattern, "/ui") || strings.HasSuffix(pattern, "/stream.mp4")) {
		return RoutePolicy{
			Class:                RouteDeadlineMediaBounded,
			RequiresFlush:        false,
			MayUpgradePerRequest: false,
		}
	}

	// 4. Recognized API Bounded JSON/REST routes
	if isRecognizedAPIRoute(method, pattern) {
		return RoutePolicy{
			Class:                RouteDeadlineAPIBounded,
			RequiresFlush:        false,
			MayUpgradePerRequest: false,
		}
	}

	// Unrecognized / new route -> RouteDeadlineUnknown (fails inventory gate validation!)
	return RoutePolicy{Class: RouteDeadlineUnknown}
}

// DeclaredCapabilitiesForStack derives declared capabilities independently from the actual middleware wrapper stack configuration.
// Returns an error if stack specifies invalid or unrecognized router or UI mode variants (fail-closed).
// NOTE: All valid outer and v3 stacks declare SetWriteDeadline, Flush, Hijack, and UpgradeDeadlineTransition target expectations (CapabilityDeclared).
func DeclaredCapabilitiesForStack(stack StackConfig) (DeclaredMiddlewareCapabilities, error) {
	if stack.RouterID != "outer" && stack.RouterID != "v3" {
		return DeclaredMiddlewareCapabilities{}, fmt.Errorf("invalid router ID (%q) in stack config", stack.RouterID)
	}

	switch stack.UIMode {
	case ConfigVariantProdStatic, ConfigVariantDevDir, ConfigVariantDevProxy:
		return DeclaredMiddlewareCapabilities{
			EquivalenceClass:          stack.EquivalenceClass(),
			SetWriteDeadline:          CapabilityDeclared,
			Flush:                     CapabilityDeclared,
			Hijack:                    CapabilityDeclared,
			UpgradeDeadlineTransition: CapabilityDeclared,
		}, nil
	default:
		return DeclaredMiddlewareCapabilities{}, fmt.Errorf("invalid UI mode variant (%q) in stack config", stack.UIMode)
	}
}

// ApplyVerifiedEvidence elevates declared capabilities to CapabilityVerified ONLY IF matching empirical probe evidence is registered for the stack's ResponseWriter equivalence class.
func ApplyVerifiedEvidence(declared DeclaredMiddlewareCapabilities, evidenceRegistry map[ResponseWriterEquivalenceClass]VerifiedStackEvidence) DeclaredMiddlewareCapabilities {
	res := declared
	evidence, ok := evidenceRegistry[declared.EquivalenceClass]
	if !ok {
		return res
	}
	if evidence.SetWriteDeadlineVerified && res.SetWriteDeadline == CapabilityDeclared {
		res.SetWriteDeadline = CapabilityVerified
	}
	if evidence.FlushVerified && res.Flush == CapabilityDeclared {
		res.Flush = CapabilityVerified
	}
	if evidence.HijackVerified && res.Hijack == CapabilityDeclared {
		res.Hijack = CapabilityVerified
	}
	if evidence.UpgradeTransitionVerified && res.UpgradeDeadlineTransition == CapabilityDeclared {
		res.UpgradeDeadlineTransition = CapabilityVerified
	}
	return res
}

// isRecognizedAPIRoute checks if a method and pattern belong to known application route patterns.
func isRecognizedAPIRoute(method string, pattern string) bool {
	switch pattern {
	case "/healthz", "/readyz", "/index.html", "/",
		"/internal/system/config/reload", "/internal/setup/validate",
		"/Items/{itemId}/PlaybackInfo", "/ui", "/ui/*":
		return true
	}

	// Specific path method restrictions:
	// /events and /stream.mp4 streaming endpoints only accept GET/HEAD; other methods are unclassified.
	if strings.HasSuffix(pattern, "/events") || strings.HasSuffix(pattern, "/stream.mp4") {
		return false
	}

	if (strings.HasPrefix(pattern, "/api/v3") || strings.HasPrefix(pattern, "/logos/")) &&
		(method == http.MethodGet || method == http.MethodHead || method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch || method == http.MethodOptions || method == http.MethodConnect || method == http.MethodTrace) {
		return true
	}
	return false
}
