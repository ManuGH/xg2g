// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

func init() {
	operationScopes["GetRecordingThumbnail"] = []string{"v3:read"}
	operationExposurePolicies["GetRecordingThumbnail"] = policy(
		ExposureClassRead,
		ExposureAuthBearerScope,
		ExposureRateLimitGlobal,
		ExposureBrowserTrustSameOrigin,
		false,
	)
}
