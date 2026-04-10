// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

import "testing"

func TestExposurePoliciesCoverEveryScopedOperation(t *testing.T) {
	missing := MissingExposurePolicies()
	if len(missing) > 0 {
		t.Fatalf("missing exposure policies: %v", missing)
	}

	policies := ExposurePolicies()
	for _, operationID := range OperationIDs() {
		policy, ok := policies[operationID]
		if !ok {
			t.Fatalf("operation %s has no exposure policy", operationID)
		}
		if policy.Class == "" {
			t.Fatalf("operation %s has empty exposure class", operationID)
		}
		if policy.AuthKind == "" {
			t.Fatalf("operation %s has empty auth kind", operationID)
		}
		if policy.RateLimitClass == "" {
			t.Fatalf("operation %s has empty rate limit class", operationID)
		}
		if policy.BrowserTrust == "" {
			t.Fatalf("operation %s has empty browser trust policy", operationID)
		}
	}
}

func TestUnscopedOperationsHaveAbuseControls(t *testing.T) {
	for operationID := range unscopedOperations {
		policy, ok := ExposurePolicyForOperation(operationID)
		if !ok {
			t.Fatalf("unscoped operation %s has no exposure policy", operationID)
		}
		if policy.AuthKind == ExposureAuthBearerScope {
			t.Fatalf("unscoped operation %s must declare its non-bearer auth semantics", operationID)
		}
		if !policy.HasDedicatedRateLimit() {
			t.Fatalf("unscoped operation %s must have a dedicated abuse-control rate limit", operationID)
		}
		if !policy.AuditRequired {
			t.Fatalf("unscoped operation %s must be audit-required", operationID)
		}
		if !policy.RedactErrors {
			t.Fatalf("unscoped operation %s must redact sensitive error semantics", operationID)
		}
	}
}

func TestPairingAndDeviceOperationsAreDedicatedSecurityClasses(t *testing.T) {
	tests := map[string]ExposureClass{
		"StartPairing":         ExposureClassPairing,
		"GetPairingStatus":     ExposureClassPairing,
		"ApprovePairing":       ExposureClassPairing,
		"ExchangePairing":      ExposureClassPairing,
		"CreateDeviceSession":  ExposureClassDevice,
		"CreateWebBootstrap":   ExposureClassDevice,
		"CompleteWebBootstrap": ExposureClassDevice,
	}

	for operationID, wantClass := range tests {
		policy, ok := ExposurePolicyForOperation(operationID)
		if !ok {
			t.Fatalf("operation %s has no exposure policy", operationID)
		}
		if policy.Class != wantClass {
			t.Fatalf("operation %s class = %s, want %s", operationID, policy.Class, wantClass)
		}
		if !policy.RequiresAbuseControls() {
			t.Fatalf("operation %s must require abuse controls", operationID)
		}
		if !policy.AuditRequired {
			t.Fatalf("operation %s must require audit events", operationID)
		}
	}
}

func TestValidateExposurePolicyEnforcesSemanticDuties(t *testing.T) {
	if err := ValidateExposurePolicy("StartPairing", "POST", nil, operationExposurePolicies["StartPairing"]); err != nil {
		t.Fatalf("expected StartPairing exposure policy to validate: %v", err)
	}

	weakPairing := operationExposurePolicies["StartPairing"]
	weakPairing.RateLimitClass = ExposureRateLimitGlobal
	if err := ValidateExposurePolicy("StartPairing", "POST", nil, weakPairing); err == nil {
		t.Fatal("expected global-rate-limited pairing policy to be rejected")
	}

	weakAdmin := operationExposurePolicies["GetSystemConnectivity"]
	if err := ValidateExposurePolicy("GetSystemConnectivity", "GET", []string{"v3:read"}, weakAdmin); err == nil {
		t.Fatal("expected admin exposure without v3:admin scope to be rejected")
	}

	weakBrowserPublic := operationExposurePolicies["StartPairing"]
	weakBrowserPublic.BrowserTrust = ExposureBrowserTrustSameOrigin
	if err := ValidateExposurePolicy("StartPairing", "POST", nil, weakBrowserPublic); err == nil {
		t.Fatal("expected browser-reachable unauthenticated exposure to be rejected")
	}
}
