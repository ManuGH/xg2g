package main

import (
	"path/filepath"
	"testing"
)

func TestLoadCatalogPreservesSecurityContract(t *testing.T) {
	entries, err := loadCatalog(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("operation catalog must not be empty")
	}

	byID := make(map[string]operationEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	assertScopes(t, byID, "GetSystemHealthz", "v3:read")
	assertScopes(t, byID, "GetSystemConnectivity", "v3:admin")
	assertScopes(t, byID, "GetSystemConfig", "v3:read")
	assertScopes(t, byID, "GetSystemScanStatus", "v3:admin")
	assertScopes(t, byID, "TriggerSystemScan", "v3:admin")
	assertScopes(t, byID, "CreateDeviceSession")
	if got := byID["CreateDeviceSession"].Policy.Auth; got != "device_grant" {
		t.Fatalf("CreateDeviceSession auth = %q, want device_grant", got)
	}
}

func TestValidatePolicyRejectsSecurityMismatch(t *testing.T) {
	policy := operationPolicy{
		Class:        "read",
		Auth:         "none",
		BrowserTrust: "none",
		RateLimit:    "global",
	}
	if err := validatePolicy("Example", []string{"v3:read"}, policy); err == nil {
		t.Fatal("expected non-bearer policy with bearer scopes to fail")
	}

	policy.Auth = "bearer_scope"
	if err := validatePolicy("Example", nil, policy); err == nil {
		t.Fatal("expected bearer policy without scopes to fail")
	}
}

func assertScopes(t *testing.T, entries map[string]operationEntry, id string, want ...string) {
	t.Helper()
	entry, ok := entries[id]
	if !ok {
		t.Fatalf("missing operation %s", id)
	}
	if len(entry.Scopes) != len(want) {
		t.Fatalf("%s scopes = %v, want %v", id, entry.Scopes, want)
	}
	for i := range want {
		if entry.Scopes[i] != want[i] {
			t.Fatalf("%s scopes = %v, want %v", id, entry.Scopes, want)
		}
	}
}
