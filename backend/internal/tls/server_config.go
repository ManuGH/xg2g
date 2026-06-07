// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package tls

import "crypto/tls"

// HardenedServerConfig returns a tls.Config tuned for a modern, browser-facing
// media server.
//
// Choices and the reasoning behind them:
//
//   - MinVersion TLS 1.2 (hard floor). We deliberately do NOT pin a TLS 1.3
//     floor: some embedded/TV browsers still cap at 1.2, in-process HTTPS here
//     is opt-in, and it often fronts a reverse proxy. A 1.3 floor would lock out
//     real clients for little gain over the AEAD-only 1.2 fallback below.
//   - CipherSuites constrains the TLS 1.2 fallback to forward-secret AEAD
//     ciphers only (ECDHE + AES-GCM / ChaCha20-Poly1305). No CBC, no static-RSA
//     key exchange. Every browser from roughly the last eight years negotiates
//     one of these, so dropping CBC costs no practical compatibility. Go ignores
//     this field for TLS 1.3 (those suites are AEAD-only and not configurable).
//   - CurvePreferences puts X25519 first, then the NIST P-256/P-384 curves.
//
// Net effect: an SSL-Labs "A"/"A+"-grade handshake by default, without the
// operator having to configure anything — secure by default rather than secure
// only behind a correctly tuned reverse proxy.
func HardenedServerConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		},
	}
}
