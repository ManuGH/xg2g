// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
)

type AssetLinkTarget struct {
	Namespace              string   `json:"namespace"`
	PackageName            string   `json:"package_name"`
	SHA256CertFingerprints []string `json:"sha256_cert_fingerprints"`
}

type AssetLinkStatement struct {
	Relation []string        `json:"relation"`
	Target   AssetLinkTarget `json:"target"`
}

// AssetLinks handles GET /.well-known/assetlinks.json
func (s *Server) AssetLinks(w http.ResponseWriter, r *http.Request) {
	// Digital Asset Links Contract:
	// - Must serve 200 OK
	// - Must serve Content-Type: application/json
	// - Must NEVER issue 301/302 redirects
	packageName := s.cfg.AndroidPackageName
	if packageName == "" {
		packageName = "de.matrixcentral.xg2g"
	}

	fingerprints := s.cfg.AndroidSHA256Fingerprints
	if len(fingerprints) == 0 {
		fingerprints = []string{
			"FA:C6:17:45:DC:09:03:78:6F:B9:ED:E6:2A:96:2B:39:9F:73:48:F0:BB:6F:89:9B:83:32:66:75:91:03:3B:9C",
		}
	}

	statement := []AssetLinkStatement{
		{
			Relation: []string{
				"delegate_permission/common.handle_all_urls",
				"delegate_permission/common.get_login_creds",
			},
			Target: AssetLinkTarget{
				Namespace:              "android_app",
				PackageName:            packageName,
				SHA256CertFingerprints: fingerprints,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(statement)
}
