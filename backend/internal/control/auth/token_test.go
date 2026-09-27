// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractToken_PriorityOrder(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test?foo=bar", nil)
	r.Header.Set("Authorization", "Bearer bearer-token ")
	r.Header.Set("X-API-Token", "header-token")
	r.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "session-token"})
	r.AddCookie(&http.Cookie{Name: "X-API-Token", Value: "legacy-cookie-token"})

	if got := ExtractToken(r); got != "bearer-token" {
		t.Fatalf("ExtractToken() = %q, want %q", got, "bearer-token")
	}
}

func TestExtractToken_IgnoresQueryParams(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test?foo=bar", nil)

	if got := ExtractToken(r); got != "" {
		t.Fatalf("ExtractToken() = %q, want empty", got)
	}
}

func TestExtractToken_DefaultDisablesLegacySources(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test", nil)
	r.Header.Set("X-API-Token", "legacy-header-token")
	r.AddCookie(&http.Cookie{Name: "X-API-Token", Value: "legacy-cookie-token"})

	if got := ExtractToken(r); got != "" {
		t.Fatalf("ExtractToken() = %q, want empty when only legacy sources are present", got)
	}
}

func TestExtractTokenDetailedWithOptions_DisablesLegacySources(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test", nil)
	r.Header.Set("X-API-Token", "legacy-header-token")
	r.AddCookie(&http.Cookie{Name: "X-API-Token", Value: "legacy-cookie-token"})

	got, src := ExtractTokenDetailedWithOptions(r, TokenExtractOptions{
		AllowLegacySources: false,
	})
	if got != "" || src != "" {
		t.Fatalf("expected no token with legacy disabled, got token=%q source=%q", got, src)
	}
}

func TestExtractTokenDetailedWithOptions_AllowsSessionCookieWhenLegacyDisabled(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test", nil)
	r.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "session-token"})
	r.Header.Set("X-API-Token", "legacy-header-token")

	got, src := ExtractTokenDetailedWithOptions(r, TokenExtractOptions{
		AllowLegacySources: false,
	})
	if got != "session-token" {
		t.Fatalf("expected session token, got %q", got)
	}
	if src != "xg2g_session cookie" {
		t.Fatalf("expected session cookie source, got %q", src)
	}
}

func TestExtractTokenDetailedWithOptions_ResolvesSessionCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test", nil)
	r.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "opaque-session-id"})

	got, src := ExtractTokenDetailedWithOptions(r, TokenExtractOptions{
		AllowLegacySources: false,
		ResolveSessionToken: func(sessionID string) (string, bool) {
			if sessionID == "opaque-session-id" {
				return "resolved-token", true
			}
			return "", false
		},
	})
	if got != "resolved-token" {
		t.Fatalf("expected resolved token, got %q", got)
	}
	if src != "xg2g_session cookie" {
		t.Fatalf("expected session cookie source, got %q", src)
	}
}

func TestExtractTokenDetailedWithOptions_RejectsUnknownSessionID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test", nil)
	r.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "unknown-session-id"})

	got, src := ExtractTokenDetailedWithOptions(r, TokenExtractOptions{
		AllowLegacySources: false,
		ResolveSessionToken: func(sessionID string) (string, bool) {
			return "", false
		},
	})
	if got != "" || src != "" {
		t.Fatalf("expected unresolved session to be rejected, got token=%q source=%q", got, src)
	}
}

func TestAuthorizeToken(t *testing.T) {
	if AuthorizeToken("secret", "secret") != true {
		t.Fatal("AuthorizeToken should accept exact match")
	}
	if AuthorizeToken("secret", "other") != false {
		t.Fatal("AuthorizeToken should reject mismatch")
	}
	if AuthorizeToken("", "secret") != false {
		t.Fatal("AuthorizeToken should reject empty got token")
	}
	if AuthorizeToken("secret", "") != false {
		t.Fatal("AuthorizeToken should reject empty expected token")
	}
	if AuthorizeToken("secret", "secret-token") != false {
		t.Fatal("AuthorizeToken should reject length mismatch")
	}
}

func TestAuthorizeRequest(t *testing.T) {
	expected := "secret"

	r := httptest.NewRequest(http.MethodGet, "http://example.local/test?foo=bar", nil)
	if AuthorizeRequest(r, expected) != false {
		t.Fatal("AuthorizeRequest should reject missing token")
	}

	r = httptest.NewRequest(http.MethodGet, "http://example.local/test", nil)
	r.Header.Set("Authorization", "Bearer secret")
	if AuthorizeRequest(r, expected) != true {
		t.Fatal("AuthorizeRequest should accept bearer token")
	}
}

func TestExtractToken_CaseInsensitiveSchemes(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantToken  string
		wantSource string
	}{
		{"standard Bearer", "Bearer my-secret-token", "my-secret-token", BearerSource},
		{"lowercase bearer", "bearer my-secret-token", "my-secret-token", BearerSource},
		{"mixed case bEaReR", "bEaReR my-secret-token", "my-secret-token", BearerSource},
		{"standard DPoP", "DPoP dpop-access-token", "dpop-access-token", DPoPSource},
		{"lowercase dpop", "dpop dpop-access-token", "dpop-access-token", DPoPSource},
		{"mixed case DpOp", "DpOp dpop-access-token", "dpop-access-token", DPoPSource},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", tt.header)
			token, source := ExtractTokenDetailedWithOptions(req, TokenExtractOptions{})
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
			if source != tt.wantSource {
				t.Errorf("source = %q, want %q", source, tt.wantSource)
			}
		})
	}
}

func TestRequestCredentialKind_CaseInsensitiveSchemes(t *testing.T) {
	reqBearer, _ := http.NewRequest(http.MethodPost, "/test", nil)
	reqBearer.Header.Set("Authorization", "bearer test-token")
	if got := RequestCredentialKind(reqBearer); got != CredentialExplicit {
		t.Errorf("got %v, want CredentialExplicit for lowercase bearer", got)
	}

	reqDPoP, _ := http.NewRequest(http.MethodPost, "/test", nil)
	reqDPoP.Header.Set("Authorization", "dpop test-token")
	if got := RequestCredentialKind(reqDPoP); got != CredentialExplicit {
		t.Errorf("got %v, want CredentialExplicit for lowercase dpop", got)
	}
}
