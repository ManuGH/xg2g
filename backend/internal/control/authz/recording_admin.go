// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

func init() {
	operationScopes["PostRecordingDelete"] = []string{"v3:write"}
	operationExposurePolicies["PostRecordingDelete"] = policy(
		ExposureClassWrite,
		ExposureAuthBearerScope,
		ExposureRateLimitGlobal,
		ExposureBrowserTrustSameOrigin,
		true,
	)

	operationScopes["PostRecordingRename"] = []string{"v3:write"}
	operationExposurePolicies["PostRecordingRename"] = policy(
		ExposureClassWrite,
		ExposureAuthBearerScope,
		ExposureRateLimitGlobal,
		ExposureBrowserTrustSameOrigin,
		true,
	)
}
