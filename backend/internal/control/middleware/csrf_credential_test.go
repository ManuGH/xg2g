// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/auth"
)

// Cross-site request forgery turns on one property: whether the browser attaches
// the caller's credentials without the page asking. These pin that the protection
// follows that property rather than a list of addresses — a list is what made the
// native app's channel changes fail over a LAN address while the same app worked
// over a domain name, with identical, equally strong credentials on both.

const (
	trustedOrigin = "https://xg2g.example.test"
	foreignOrigin = "https://evil.example.test"
)

func csrfChain(t *testing.T) http.Handler {
	t.Helper()
	reached := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return CSRFProtection([]string{trustedOrigin})(reached)
}

func post(t *testing.T, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://xg2g.example.test/api/v3/stream/prepare", nil)
	req.Host = "xg2g.example.test"
	req.TLS = nil
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	csrfChain(t).ServeHTTP(rec, req)
	return rec
}

func withSessionCookie(r *http.Request) {
	r.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "a-session-id"})
}

//  1. A browser request carrying the victim's cookie from a foreign page is exactly
//     what forgery is, and is refused.
func TestCSRF_BrowserWithAmbientCredentialFromForeignOriginIsRefused(t *testing.T) {
	rec := post(t, func(r *http.Request) {
		withSessionCookie(r)
		r.Header.Set("Origin", foreignOrigin)
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for a forged browser request, got %d", rec.Code)
	}
}

// A cookie-bearing request with no origin at all is refused too: an unsafe method
// carrying ambient credentials has to prove where it came from.
func TestCSRF_BrowserWithAmbientCredentialAndNoOriginIsRefused(t *testing.T) {
	rec := post(t, withSessionCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 without an origin, got %d", rec.Code)
	}
}

//  2. The same browser from an allowed origin is admitted, and normal authentication
//     decides the rest.
func TestCSRF_BrowserWithAmbientCredentialFromTrustedOriginPasses(t *testing.T) {
	rec := post(t, func(r *http.Request) {
		withSessionCookie(r)
		r.Header.Set("Origin", trustedOrigin)
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want the request admitted, got %d", rec.Code)
	}
}

//  3. A native client authenticating with a device-bound credential is not refused
//     for arriving from an address nobody wrote down. Its credential is one no
//     foreign page can produce, so there is nothing here to forge.
func TestCSRF_NativeClientWithExplicitCredentialIsNotRefusedOnOrigin(t *testing.T) {
	for _, scheme := range []string{"DPoP", "Bearer"} {
		rec := post(t, func(r *http.Request) {
			r.Header.Set("Authorization", scheme+" a-token")
			// A LAN address, a VPN address, or nothing at all — none of it decides.
			r.Header.Set("Origin", "http://10.10.55.14:8089")
		})
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s: want the request admitted for authentication to judge, got %d", scheme, rec.Code)
		}
	}

	rec := post(t, func(r *http.Request) {
		r.Header.Set("Authorization", "DPoP a-token")
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want a native request with no origin admitted, got %d", rec.Code)
	}
}

// 5. Passing this middleware is not authentication and grants nothing.
//
// The header may be nonsense, expired or absent entirely — this layer does not
// look at it and does not have to. What it decides is whether a foreign page could
// have caused this request *with credentials already attached*, and without an
// ambient credential it could not. Whether the caller may act is decided after,
// by authentication, which is what makes the branch safe rather than a bypass.
func TestCSRF_AdmittingARequestIsNotAuthentication(t *testing.T) {
	authenticated := false
	protected := CSRFProtection([]string{trustedOrigin})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Stand-in for the authentication middleware that really follows.
			if r.Header.Get("Authorization") != "Bearer the-only-valid-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			authenticated = true
			w.WriteHeader(http.StatusNoContent)
		}))

	req := httptest.NewRequest(http.MethodPost, "https://xg2g.example.test/api/v3/stream/prepare", nil)
	req.Header.Set("Authorization", "Bearer forged-nonsense")
	req.Header.Set("Origin", foreignOrigin)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a merely syntactic credential must be refused downstream, got %d", rec.Code)
	}
	if authenticated {
		t.Fatal("the request must never have counted as authenticated")
	}
}

// A cookie alongside a header still counts as ambient. Extraction prefers the
// header, but the browser sent the cookie — and a forged request is precisely one
// where the attacker sets no header and rides on it.
func TestCSRF_ACookieBesideAHeaderStillCountsAsForgeable(t *testing.T) {
	rec := post(t, func(r *http.Request) {
		withSessionCookie(r)
		r.Header.Set("Authorization", "Bearer a-token")
		r.Header.Set("Origin", foreignOrigin)
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 when an ambient credential is present, got %d", rec.Code)
	}
}

// Safe methods were never in scope and stay out of it.
func TestCSRF_SafeMethodsAreUntouched(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "https://xg2g.example.test/api/v3/status", nil)
		withSessionCookie(req)
		req.Header.Set("Origin", foreignOrigin)
		rec := httptest.NewRecorder()
		csrfChain(t).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s must not be subject to CSRF, got %d", method, rec.Code)
		}
	}
}

// The legacy cookie is a credential too, and classifying it as anything else would
// leave a forgeable channel unprotected.
func TestCSRF_LegacyCookieIsAmbientToo(t *testing.T) {
	rec := post(t, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "X-API-Token", Value: "legacy"})
		r.Header.Set("Origin", foreignOrigin)
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for the legacy cookie, got %d", rec.Code)
	}
}

// The classification itself, stated directly. An unclassified source is treated as
// forgeable, because "nobody reasoned about this" must not read as "it is safe".
func TestCredentialKind_ClassifiesEverySourceAndFailsClosed(t *testing.T) {
	cases := map[string]auth.CredentialKind{
		auth.BearerSource:        auth.CredentialExplicit,
		auth.DPoPSource:          auth.CredentialExplicit,
		auth.LegacyHeaderSource:  auth.CredentialExplicit,
		auth.SessionCookieSource: auth.CredentialAmbient,
		auth.LegacyCookieSource:  auth.CredentialAmbient,
		"":                       auth.CredentialNone,
		"some future source":     auth.CredentialAmbient,
	}
	for source, want := range cases {
		if got := auth.SourceCredentialKind(source); got != want {
			t.Errorf("source %q: want %q, got %q", source, want, got)
		}
	}
	if !auth.CredentialAmbient.Ambient() || auth.CredentialExplicit.Ambient() {
		t.Fatal("Ambient() must answer for exactly the ambient kind")
	}
}

// A request presenting nothing at all keeps the full check.
//
// This is the line between the exemption being sound and being a hole. The absence
// of a credential is not evidence about the caller, and treating "no cookie" as
// "not a browser" would drop the protection from every endpoint reachable without
// authentication. Only a credential the caller had to set deliberately is exempt.
func TestCSRF_ARequestWithNoCredentialAtAllKeepsTheFullCheck(t *testing.T) {
	rec := post(t, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for an unauthenticated unsafe request with no origin, got %d", rec.Code)
	}

	rec = post(t, func(r *http.Request) { r.Header.Set("Origin", foreignOrigin) })
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for an unauthenticated unsafe request from a foreign origin, got %d", rec.Code)
	}
}

// The request-level classification, stated directly.
func TestRequestCredentialKind_AmbientWinsAndAbsenceIsNotExplicit(t *testing.T) {
	newReq := func(mutate func(*http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "https://xg2g.example.test/x", nil)
		if mutate != nil {
			mutate(r)
		}
		return r
	}

	if got := auth.RequestCredentialKind(newReq(nil)); got != auth.CredentialNone {
		t.Errorf("no credential: want none, got %q", got)
	}
	if got := auth.RequestCredentialKind(newReq(func(r *http.Request) {
		r.Header.Set("Authorization", "DPoP t")
	})); got != auth.CredentialExplicit {
		t.Errorf("DPoP: want explicit, got %q", got)
	}
	if got := auth.RequestCredentialKind(newReq(withSessionCookie)); got != auth.CredentialAmbient {
		t.Errorf("session cookie: want ambient, got %q", got)
	}
	// Both present: ambient wins, because that is the one an attacker rides on.
	if got := auth.RequestCredentialKind(newReq(func(r *http.Request) {
		withSessionCookie(r)
		r.Header.Set("Authorization", "Bearer t")
	})); got != auth.CredentialAmbient {
		t.Errorf("cookie beside header: want ambient, got %q", got)
	}
	// An Authorization header of an unknown scheme is not an explicit credential
	// this server accepts, so it must not buy the exemption.
	if got := auth.RequestCredentialKind(newReq(func(r *http.Request) {
		r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	})); got != auth.CredentialNone {
		t.Errorf("unknown scheme: want none, got %q", got)
	}
}
