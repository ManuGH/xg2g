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
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test?token=query", nil)
	r.Header.Set("Authorization", "Bearer bearer-token ")
	r.Header.Set("X-API-Token", "header-token")
	r.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "session-token"})
	r.AddCookie(&http.Cookie{Name: "X-API-Token", Value: "legacy-cookie-token"})

	if got := ExtractToken(r, true); got != "bearer-token" {
		t.Fatalf("ExtractToken() = %q, want %q", got, "bearer-token")
	}
}

func TestExtractToken_AllowQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.local/test?token=query-token", nil)

	if got := ExtractToken(r, false); got != "" {
		t.Fatalf("ExtractToken(allowQuery=false) = %q, want empty", got)
	}

	if got := ExtractToken(r, true); got != "query-token" {
		t.Fatalf("ExtractToken(allowQuery=true) = %q, want %q", got, "query-token")
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
}

func TestAuthorizeRequest(t *testing.T) {
	expected := "secret"

	r := httptest.NewRequest(http.MethodGet, "http://example.local/test?token=secret", nil)
	if AuthorizeRequest(r, expected, true) != true {
		t.Fatal("AuthorizeRequest should accept query token when allowQuery=true")
	}
	if AuthorizeRequest(r, expected, false) != false {
		t.Fatal("AuthorizeRequest should reject query token when allowQuery=false")
	}
}
