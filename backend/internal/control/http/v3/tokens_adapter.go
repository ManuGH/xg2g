// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

// verifyLivePlaybackDecision verifies a live playback decision token using the tokens subpackage.
func (s *Server) verifyLivePlaybackDecision(token, principal, serviceRef, mode string) bool {
	if s.tokensService == nil {
		return false
	}
	return s.tokensService.VerifyLivePlaybackDecision(token, principal, serviceRef, mode)
}

//nolint:unused // Used by tests to mint deterministic attestation tokens.
func (s *Server) attestLivePlaybackDecision(requestID, principal, serviceRef, mode string) string {
	if s.tokensService == nil {
		return ""
	}
	return s.tokensService.AttestLivePlaybackDecision(requestID, principal, serviceRef, mode)
}
