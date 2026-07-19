// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

type ExposureClass string

const (
	ExposureClassRead    ExposureClass = "read"
	ExposureClassWrite   ExposureClass = "write"
	ExposureClassAdmin   ExposureClass = "admin"
	ExposureClassDevice  ExposureClass = "device"
	ExposureClassPairing ExposureClass = "pairing"
	ExposureClassSession ExposureClass = "session"
	ExposureClassHealth  ExposureClass = "health"
	ExposureClassSystem  ExposureClass = "system"
)

type ExposureAuthKind string

const (
	ExposureAuthBearerScope    ExposureAuthKind = "bearer_scope"
	ExposureAuthNone           ExposureAuthKind = "none"
	ExposureAuthPairingSecret  ExposureAuthKind = "pairing_secret"
	ExposureAuthDeviceGrant    ExposureAuthKind = "device_grant"
	ExposureAuthBootstrapToken ExposureAuthKind = "bootstrap_token"
)

type ExposureRateLimitClass string

const (
	ExposureRateLimitNone          ExposureRateLimitClass = "none"
	ExposureRateLimitGlobal        ExposureRateLimitClass = "global"
	ExposureRateLimitAuth          ExposureRateLimitClass = "auth"
	ExposureRateLimitPairingStart  ExposureRateLimitClass = "pairing_start"
	ExposureRateLimitPairingPoll   ExposureRateLimitClass = "pairing_poll"
	ExposureRateLimitPairingSecret ExposureRateLimitClass = "pairing_secret"
	ExposureRateLimitDeviceGrant   ExposureRateLimitClass = "device_grant"
	ExposureRateLimitBootstrap     ExposureRateLimitClass = "web_bootstrap"
)

type ExposureBrowserTrust string

const (
	ExposureBrowserTrustSameOrigin ExposureBrowserTrust = "same_origin_or_allowed_origin"
	ExposureBrowserTrustNotBrowser ExposureBrowserTrust = "not_browser"
	ExposureBrowserTrustNone       ExposureBrowserTrust = "none"
)

type ExposurePolicy struct {
	Class          ExposureClass
	AuthKind       ExposureAuthKind
	BrowserTrust   ExposureBrowserTrust
	RateLimitClass ExposureRateLimitClass
	AuditRequired  bool
	RedactErrors   bool
}

func ExposurePolicyForOperation(operationID string) (ExposurePolicy, bool) {
	policy, ok := operationExposurePolicies[operationID]
	return policy, ok
}

func OperationIDs() []string {
	out := make([]string, 0, len(operationScopes))
	for operationID := range operationScopes {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func MissingExposurePolicies() []string {
	missing := make([]string, 0)
	for operationID := range operationScopes {
		if _, ok := operationExposurePolicies[operationID]; !ok {
			missing = append(missing, operationID)
		}
	}
	sort.Strings(missing)
	return missing
}

func ExposurePolicies() map[string]ExposurePolicy {
	out := make(map[string]ExposurePolicy, len(operationExposurePolicies))
	maps.Copy(out, operationExposurePolicies)
	return out
}

func ValidateExposurePolicy(operationID, method string, scopes []string, p ExposurePolicy) error {
	operationID = strings.TrimSpace(operationID)
	method = strings.ToUpper(strings.TrimSpace(method))
	if operationID == "" {
		return fmt.Errorf("operation id is required")
	}
	if p.Class == "" {
		return fmt.Errorf("%s: exposure class is required", operationID)
	}
	if p.AuthKind == "" {
		return fmt.Errorf("%s: exposure auth kind is required", operationID)
	}
	if p.RateLimitClass == "" {
		return fmt.Errorf("%s: exposure rate limit class is required", operationID)
	}
	if p.BrowserTrust == "" {
		return fmt.Errorf("%s: exposure browser trust is required", operationID)
	}
	if p.AuthKind == ExposureAuthBearerScope && len(scopes) == 0 {
		return fmt.Errorf("%s: bearer-scope exposure requires scoped auth", operationID)
	}
	if p.AuthKind != ExposureAuthBearerScope && len(scopes) > 0 {
		return fmt.Errorf("%s: non-bearer exposure must not also require bearer scopes", operationID)
	}
	if p.AuthKind != ExposureAuthBearerScope && !IsUnscopedAllowed(operationID) {
		return fmt.Errorf("%s: non-bearer exposure must be explicitly allowlisted as unscoped", operationID)
	}
	if p.RequiresAbuseControls() {
		if !p.HasDedicatedRateLimit() {
			return fmt.Errorf("%s: sensitive exposure requires dedicated rate limit", operationID)
		}
		if !p.AuditRequired {
			return fmt.Errorf("%s: sensitive exposure requires audit", operationID)
		}
		if !p.RedactErrors {
			return fmt.Errorf("%s: sensitive exposure requires redacted errors", operationID)
		}
	}
	if p.Class == ExposureClassAdmin {
		if !hasScope(scopes, "v3:admin") {
			return fmt.Errorf("%s: admin exposure requires v3:admin", operationID)
		}
		if p.RateLimitClass != ExposureRateLimitAuth {
			return fmt.Errorf("%s: admin exposure requires auth rate-limit class", operationID)
		}
		if !p.AuditRequired {
			return fmt.Errorf("%s: admin exposure requires audit", operationID)
		}
	}
	if p.Class == ExposureClassWrite {
		if !hasScope(scopes, "v3:write") {
			return fmt.Errorf("%s: write exposure requires v3:write", operationID)
		}
		if !p.AuditRequired {
			return fmt.Errorf("%s: write exposure requires audit", operationID)
		}
	}
	if p.Class == ExposureClassPairing && p.RateLimitClass == ExposureRateLimitGlobal {
		return fmt.Errorf("%s: pairing exposure must not use global rate limit", operationID)
	}
	if p.Class == ExposureClassDevice && p.RateLimitClass == ExposureRateLimitGlobal {
		return fmt.Errorf("%s: device exposure must not use global rate limit", operationID)
	}
	if method == "GET" && p.AuthKind == ExposureAuthNone {
		return fmt.Errorf("%s: unauthenticated exposure must not be GET-cacheable", operationID)
	}
	if p.BrowserTrust == ExposureBrowserTrustSameOrigin && p.AuthKind == ExposureAuthNone {
		return fmt.Errorf("%s: browser-reachable unauthenticated exposure is not allowed", operationID)
	}
	return nil
}

func (p ExposurePolicy) RequiresAbuseControls() bool {
	return p.AuthKind != ExposureAuthBearerScope || p.Class == ExposureClassPairing || p.Class == ExposureClassDevice
}

func (p ExposurePolicy) HasDedicatedRateLimit() bool {
	return p.RateLimitClass != ExposureRateLimitNone && p.RateLimitClass != ExposureRateLimitGlobal
}

func (p ExposurePolicy) IsBrowserReachable() bool {
	return strings.TrimSpace(string(p.BrowserTrust)) == string(ExposureBrowserTrustSameOrigin)
}

func (p ExposurePolicy) RequiresNoStore() bool {
	return p.AuditRequired || p.AuthKind != ExposureAuthBearerScope || p.Class == ExposureClassPairing || p.Class == ExposureClassDevice
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if strings.EqualFold(strings.TrimSpace(scope), required) {
			return true
		}
	}
	return false
}
