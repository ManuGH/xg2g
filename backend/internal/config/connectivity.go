// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"strings"

	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
)

// BuildPublishedEndpoints validates and canonicalizes operator-published
// endpoint truth from config. Runtime callers should use this instead of
// duplicating transport policy or endpoint sorting.
func BuildPublishedEndpoints(cfg AppConfig) ([]connectivitydomain.PublishedEndpoint, error) {
	provider := connectivitydomain.NewProvider(connectivitydomain.ProviderOptions{
		AllowLocalHTTP: cfg.Connectivity.AllowLocalHTTP,
	})
	return provider.Build(publishedEndpointSpecsFromConfig(cfg.Connectivity.PublishedEndpoints))
}

// BuildConnectivityContract evaluates the declared deployment contract from
// explicit config only. It never derives topology from request headers.
func BuildConnectivityContract(cfg AppConfig) (connectivitydomain.ContractReport, error) {
	endpoints, err := BuildPublishedEndpoints(cfg)
	if err != nil {
		return connectivitydomain.ContractReport{}, err
	}

	return connectivitydomain.EvaluateContract(connectivitydomain.ContractInput{
		Profile:            connectivitydomain.DeploymentProfile(cfg.Connectivity.Profile),
		AllowLocalHTTP:     cfg.Connectivity.AllowLocalHTTP,
		PublishedEndpoints: endpoints,
		AllowedOrigins:     append([]string(nil), cfg.AllowedOrigins...),
		TrustedProxies:     splitCSVNonEmptyCSV(strings.TrimSpace(cfg.TrustedProxies)),
		TLSEnabled:         cfg.TLSEnabled,
		ForceHTTPS:         cfg.ForceHTTPS,
	}), nil
}

func publishedEndpointSpecsFromConfig(values []PublishedEndpointConfig) []connectivitydomain.PublishedEndpoint {
	if len(values) == 0 {
		return []connectivitydomain.PublishedEndpoint{}
	}

	specs := make([]connectivitydomain.PublishedEndpoint, 0, len(values))
	for _, value := range values {
		source := strings.TrimSpace(value.Source)
		if source == "" {
			source = string(connectivitydomain.EndpointSourceConfig)
		}

		tlsMode := strings.TrimSpace(value.TLSMode)
		if tlsMode == "" {
			tlsMode = defaultPublishedEndpointTLSMode(value.Kind)
		}

		specs = append(specs, connectivitydomain.PublishedEndpoint{
			URL:             strings.TrimSpace(value.URL),
			Kind:            connectivitydomain.EndpointKind(strings.TrimSpace(value.Kind)),
			Priority:        value.Priority,
			TLSMode:         connectivitydomain.TLSMode(tlsMode),
			AllowPairing:    value.AllowPairing,
			AllowStreaming:  value.AllowStreaming,
			AllowWeb:        value.AllowWeb,
			AllowNative:     value.AllowNative,
			AdvertiseReason: strings.TrimSpace(value.AdvertiseReason),
			Source:          connectivitydomain.EndpointSource(source),
		})
	}

	return specs
}

func defaultPublishedEndpointTLSMode(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(connectivitydomain.EndpointKindLocalHTTP):
		return string(connectivitydomain.TLSModeProhibited)
	case string(connectivitydomain.EndpointKindPublicHTTPS), string(connectivitydomain.EndpointKindLocalHTTPS):
		return string(connectivitydomain.TLSModeRequired)
	default:
		return ""
	}
}

func splitCSVNonEmptyCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
