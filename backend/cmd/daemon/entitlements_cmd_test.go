package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

func TestResolveV3APIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		port    int
		want    string
		wantErr bool
	}{
		{
			name: "default localhost",
			port: 8088,
			want: "http://localhost:8088/api/v3",
		},
		{
			name: "root url gets api suffix",
			raw:  "https://example.com",
			want: "https://example.com/api/v3",
		},
		{
			name: "existing api prefix is preserved",
			raw:  "https://example.com/api/v3",
			want: "https://example.com/api/v3",
		},
		{
			name: "custom path keeps suffix",
			raw:  "https://example.com/xg2g",
			want: "https://example.com/xg2g/api/v3",
		},
		{
			name:    "relative urls fail",
			raw:     "/api/v3",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveV3APIBaseURL(tt.raw, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveV3APIBaseURL(%q, %d) = %q, want %q", tt.raw, tt.port, got, tt.want)
			}
		})
	}
}

func TestRunEntitlementsListJSON(t *testing.T) {
	var seenAuth string
	var seenQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenQuery = r.URL.RawQuery
		if r.URL.Path != "/api/v3/system/entitlements" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"principalId":"viewer","unlocked":true,"grantedScopes":["xg2g:unlock"]}`))
	}))
	defer ts.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runEntitlementsCLIWithIO([]string{
		"list",
		"--base-url", ts.URL,
		"--token", "admin-token",
		"--principal-id", "viewer",
		"--json",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stderr.String())
	}
	if seenAuth != "Bearer admin-token" {
		t.Fatalf("expected bearer token, got %q", seenAuth)
	}
	if seenQuery != "principalId=viewer" {
		t.Fatalf("expected principalId query, got %q", seenQuery)
	}
	if !strings.Contains(stdout.String(), `"principalId":"viewer"`) {
		t.Fatalf("expected JSON output, got %q", stdout.String())
	}
}

func TestRunEntitlementsGrantAndRevoke(t *testing.T) {
	var grantRequest v3.PostSystemEntitlementOverrideJSONRequestBody
	var revokedPaths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/system/entitlements/overrides":
			if err := json.NewDecoder(r.Body).Decode(&grantRequest); err != nil {
				t.Fatalf("decode grant request: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v3/system/entitlements/overrides/"):
			revokedPaths = append(revokedPaths, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	var grantStdout bytes.Buffer
	var grantStderr bytes.Buffer
	grantCode := runEntitlementsCLIWithIO([]string{
		"grant",
		"--base-url", ts.URL,
		"--token", "admin-token",
		"--principal-id", "viewer",
		"--scope", "xg2g:unlock",
		"--scope", "xg2g:dvr",
		"--expires", "24h",
	}, &grantStdout, &grantStderr)

	if grantCode != 0 {
		t.Fatalf("expected grant exit 0, got %d: %s", grantCode, grantStderr.String())
	}
	if grantRequest.PrincipalId == nil || *grantRequest.PrincipalId != "viewer" {
		t.Fatalf("unexpected principal id: %+v", grantRequest.PrincipalId)
	}
	if len(grantRequest.Scopes) != 2 || grantRequest.Scopes[0] != "xg2g:unlock" || grantRequest.Scopes[1] != "xg2g:dvr" {
		t.Fatalf("unexpected scopes: %+v", grantRequest.Scopes)
	}
	if grantRequest.ExpiresAt == nil {
		t.Fatal("expected expiresAt to be set")
	}
	if !grantRequest.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("expected expiresAt in the future, got %s", grantRequest.ExpiresAt.UTC())
	}

	var revokeStdout bytes.Buffer
	var revokeStderr bytes.Buffer
	revokeCode := runEntitlementsCLIWithIO([]string{
		"revoke",
		"--base-url", ts.URL,
		"--token", "admin-token",
		"--principal-id", "viewer",
		"--scope", "xg2g:unlock,xg2g:dvr",
	}, &revokeStdout, &revokeStderr)

	if revokeCode != 0 {
		t.Fatalf("expected revoke exit 0, got %d: %s", revokeCode, revokeStderr.String())
	}
	if len(revokedPaths) != 2 {
		t.Fatalf("expected 2 revoke requests, got %d (%v)", len(revokedPaths), revokedPaths)
	}
	if revokedPaths[0] != "/api/v3/system/entitlements/overrides/viewer/xg2g:dvr" && revokedPaths[0] != "/api/v3/system/entitlements/overrides/viewer/xg2g:unlock" {
		t.Fatalf("unexpected revoke path: %s", revokedPaths[0])
	}
}
