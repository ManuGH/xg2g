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
