// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package tls

import (
	"crypto/tls"
	"testing"
)

func TestHardenedServerConfig_FloorAndCurves(t *testing.T) {
	cfg := HardenedServerConfig()

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %#x, want TLS 1.2 (%#x)", cfg.MinVersion, tls.VersionTLS12)
	}

	// X25519 must be the first curve preference.
	if len(cfg.CurvePreferences) == 0 || cfg.CurvePreferences[0] != tls.X25519 {
		t.Fatalf("CurvePreferences[0] = %v, want X25519", cfg.CurvePreferences)
	}
}

// TestHardenedServerConfig_NoWeakCiphers is the negative control: it fails if any
// configured TLS 1.2 cipher is a non-AEAD / non-forward-secret suite. Without the
// explicit allowlist (i.e. relying on Go defaults) this would let CBC suites
// through, so the assertion only passes because of the hardening.
func TestHardenedServerConfig_NoWeakCiphers(t *testing.T) {
	cfg := HardenedServerConfig()

	if len(cfg.CipherSuites) == 0 {
		t.Fatal("expected an explicit CipherSuites allowlist, got none")
	}

	// Build the set of suites Go itself classifies as insecure, plus an explicit
	// CBC denylist, and prove none of ours appear.
	insecure := map[uint16]string{}
	for _, s := range tls.InsecureCipherSuites() {
		insecure[s.ID] = s.Name
	}
	denylist := map[uint16]string{
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:   "ECDHE_RSA_AES128_CBC_SHA",
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:   "ECDHE_RSA_AES256_CBC_SHA",
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA: "ECDHE_ECDSA_AES128_CBC_SHA",
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256:      "RSA_AES128_GCM (no forward secrecy)",
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384:      "RSA_AES256_GCM (no forward secrecy)",
		tls.TLS_RSA_WITH_AES_128_CBC_SHA:         "RSA_AES128_CBC_SHA",
	}

	for _, id := range cfg.CipherSuites {
		if name, bad := insecure[id]; bad {
			t.Errorf("configured cipher %#x is in Go's insecure set (%s)", id, name)
		}
		if name, bad := denylist[id]; bad {
			t.Errorf("configured cipher %#x is denylisted (%s)", id, name)
		}
	}
}
