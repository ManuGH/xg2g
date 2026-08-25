// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import (
	"net/http"
	"strings"
)

// How a caller presented its credentials, and whether a browser would have
// attached them on its own.
//
// This is the distinction cross-site request forgery turns on, and it is a property
// of the credential channel rather than of the client. A forged request is one a
// foreign page can cause the victim's browser to send *with the victim's
// credentials already on it*. That is only possible where the browser attaches
// them automatically: cookies. A credential the caller has to set deliberately -
// an Authorization header - cannot be obtained by a page that does not already
// hold the token, and a page that holds the token does not need forgery.
//
// The classification lives here, next to the extraction it describes, so the rule
// cannot drift from the sources it is about. A new source must be classified in
// this file or it is refused as unknown.
type CredentialKind string

const (
	// CredentialNone means the request presented no credential at all.
	CredentialNone CredentialKind = "none"

	// CredentialAmbient means the browser attaches it without the page asking:
	// cookies. Requests carrying one are forgeable and must prove their origin.
	CredentialAmbient CredentialKind = "ambient"

	// CredentialExplicit means the caller set it deliberately on this request.
	// A cross-site page cannot produce one it does not already possess.
	CredentialExplicit CredentialKind = "explicit"
)

// Ambient reports whether a browser would attach this credential unprompted.
func (k CredentialKind) Ambient() bool { return k == CredentialAmbient }

// SourceCredentialKind classifies an extraction source.
//
// Unknown sources are treated as ambient. A source nobody has classified is one
// nobody has reasoned about, and the safe reading of "I do not know" is "assume it
// can be forged".
func SourceCredentialKind(source string) CredentialKind {
	switch source {
	case BearerSource, DPoPSource, LegacyHeaderSource:
		return CredentialExplicit
	case SessionCookieSource, LegacyCookieSource:
		return CredentialAmbient
	case "":
		return CredentialNone
	default:
		return CredentialAmbient
	}
}

// RequestCredentialKind classifies how a request presents credentials, before any
// of them have been validated.
//
// Ambient wins over explicit when both are present. Extraction prefers the header,
// but the browser sent the cookie regardless, and a forged request is exactly one
// where the attacker sets no header and rides on that cookie. So the question is
// whether any ambient credential is present, not which one would win.
//
// A request presenting nothing at all is CredentialNone, not explicit. It is
// unauthenticated, and nothing here should read the absence of a credential as
// evidence about the caller.
func RequestCredentialKind(r *http.Request) CredentialKind {
	if r == nil {
		return CredentialNone
	}
	for _, name := range ambientCookieNames {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return CredentialAmbient
		}
	}
	if header := r.Header.Get("Authorization"); header != "" {
		if strings.HasPrefix(header, "DPoP ") || strings.HasPrefix(header, "Bearer ") {
			return CredentialExplicit
		}
	}
	if r.Header.Get("X-API-Token") != "" {
		return CredentialExplicit
	}
	return CredentialNone
}

// ambientCookieNames are every cookie this server will read a credential from.
// Kept beside the extraction constants so adding a source without classifying it
// is visible here.
var ambientCookieNames = []string{sessionCookieName, legacyCookieName}
