// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strings"
)

const minPublicSecretLength = 32

type PublicExposurePolicyError struct {
	Field   string
	Message string
	Value   any
}

func PublicExposureSecurityError(cfg AppConfig) error {
	errs := PublicExposureSecurityFindings(cfg)
	if len(errs) == 0 {
		return nil
	}

	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, fmt.Sprintf("%s: %s", err.Field, err.Message))
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

func PublicExposureSecurityFindings(cfg AppConfig) []PublicExposurePolicyError {
	if !isPublicConnectivityProfile(cfg.Connectivity.Profile) {
		return nil
	}

	var errs []PublicExposurePolicyError
	add := func(field, message string, value any) {
		errs = append(errs, PublicExposurePolicyError{Field: field, Message: message, Value: value})
	}

	if strings.TrimSpace(cfg.APIToken) == "" && len(cfg.APITokens) == 0 {
		add("APIToken", "public profiles require at least one scoped API token", "")
	}
	if token := strings.TrimSpace(cfg.APIToken); token != "" && weakPublicSecret(token) {
		add("APIToken", "public profile API token must be at least 32 non-default characters", "")
	}
	for _, token := range cfg.APITokens {
		value := strings.TrimSpace(token.Token)
		if weakPublicSecret(value) {
			add("APITokens", "public profile scoped API tokens must be at least 32 non-default characters", "")
		}
	}
	if !cfg.APIDisableLegacyTokenSources {
		add("APIDisableLegacyTokenSources", "legacy token sources must be disabled in public profiles", "")
	}
	if publicStreamingPublished(cfg) && weakPublicSecret(strings.TrimSpace(cfg.PlaybackDecisionSecret)) {
		add("PlaybackDecisionSecret", "public streaming requires a playback decision secret with at least 32 non-default characters", "")
	}

	return errs
}

func isPublicConnectivityProfile(profile string) bool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "reverse_proxy", "tunnel", "vps":
		return true
	default:
		return false
	}
}

func publicStreamingPublished(cfg AppConfig) bool {
	for _, endpoint := range cfg.Connectivity.PublishedEndpoints {
		if strings.EqualFold(strings.TrimSpace(endpoint.Kind), "public_https") && endpoint.AllowStreaming {
			return true
		}
	}
	return false
}

func weakPublicSecret(secret string) bool {
	secret = strings.TrimSpace(secret)
	if len(secret) < minPublicSecretLength {
		return true
	}
	switch strings.ToLower(secret) {
	case strings.Repeat("x", minPublicSecretLength),
		strings.Repeat("a", minPublicSecretLength),
		strings.Repeat("0", minPublicSecretLength),
		"change-me-change-me-change-me-1234",
		"development-development-development",
		"test-token-test-token-test-token-1":
		return true
	default:
		return false
	}
}
