// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package connectivity

import (
	"errors"
	"testing"
)

func TestProviderBuild_RejectsIllegalPublishedEndpoints(t *testing.T) {
	base := PublishedEndpoint{
		Kind:            EndpointKindLocalHTTPS,
		Priority:        10,
		TLSMode:         TLSModeRequired,
		AllowPairing:    true,
		AllowStreaming:  true,
		AllowNative:     true,
		AdvertiseReason: "lan",
		Source:          EndpointSourceConfig,
	}

	cases := []struct {
		name    string
		spec    PublishedEndpoint
		options ProviderOptions
		match   func(error) bool
	}{
		{
			name: "reject localhost",
			spec: withURL(base, "https://localhost"),
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject loopback IPv4",
			spec: withURL(base, "https://127.0.0.1:8443"),
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject loopback IPv6",
			spec: withURL(base, "https://[::1]:8443"),
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject docker hostname",
			spec: withURL(base, "https://host.docker.internal"),
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject single-label compose host",
			spec: withURL(base, "https://xg2g:8443"),
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject default docker bridge ip",
			spec: withURL(base, "https://172.17.0.2:8443"),
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject schemeless url",
			spec: withURL(base, "xg2g.example.com"),
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointURL)
			},
		},
		{
			name: "reject userinfo",
			spec: withURL(base, "https://user:pass@xg2g.example.com"),
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointURL)
			},
		},
		{
			name: "reject query",
			spec: withURL(base, "https://xg2g.example.com?foo=bar"),
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointURL)
			},
		},
		{
			name: "reject fragment",
			spec: withURL(base, "https://xg2g.example.com#frag"),
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointURL)
			},
		},
		{
			name: "reject non-origin path",
			spec: withURL(base, "https://xg2g.example.com/ui/"),
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointURL)
			},
		},
		{
			name: "reject local http without opt-in",
			spec: PublishedEndpoint{
				URL:             "http://192.168.1.10:8080",
				Kind:            EndpointKindLocalHTTP,
				Priority:        20,
				TLSMode:         TLSModeProhibited,
				AllowPairing:    true,
				AllowStreaming:  true,
				AllowNative:     true,
				AdvertiseReason: "local-http",
				Source:          EndpointSourceConfig,
			},
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointLocalHTTPDisabled)
			},
		},
		{
			name: "reject allow_web on local http",
			spec: PublishedEndpoint{
				URL:             "http://192.168.1.10:8080",
				Kind:            EndpointKindLocalHTTP,
				Priority:        20,
				TLSMode:         TLSModeProhibited,
				AllowWeb:        true,
				AllowNative:     true,
				AdvertiseReason: "local-http",
				Source:          EndpointSourceConfig,
			},
			options: ProviderOptions{AllowLocalHTTP: true},
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointCapability)
			},
		},
		{
			name: "reject public https on private ip",
			spec: PublishedEndpoint{
				URL:             "https://192.168.1.10",
				Kind:            EndpointKindPublicHTTPS,
				Priority:        1,
				TLSMode:         TLSModeRequired,
				AllowWeb:        true,
				AllowNative:     true,
				AdvertiseReason: "public",
				Source:          EndpointSourceConfig,
			},
			match: func(err error) bool {
				return errors.Is(err, ErrEndpointHostRejected)
			},
		},
		{
			name: "reject unknown source",
			spec: PublishedEndpoint{
				URL:             "https://xg2g.example.com",
				Kind:            EndpointKindPublicHTTPS,
				Priority:        1,
				TLSMode:         TLSModeRequired,
				AllowWeb:        true,
				AllowNative:     true,
				AdvertiseReason: "public",
				Source:          "magic",
			},
			match: func(err error) bool {
				return errors.Is(err, ErrInvalidEndpointSource)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewProvider(tc.options).Build([]PublishedEndpoint{tc.spec})
			if err == nil {
				t.Fatal("expected provider to reject endpoint, got nil")
			}
			if tc.match != nil && !tc.match(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProviderBuild_RejectsDuplicateCanonicalEndpoints(t *testing.T) {
	specs := []PublishedEndpoint{
		{
			URL:             "https://xg2g.example.com/",
			Kind:            EndpointKindPublicHTTPS,
			Priority:        1,
			TLSMode:         TLSModeRequired,
			AllowWeb:        true,
			AllowNative:     true,
			AdvertiseReason: "public",
			Source:          EndpointSourceConfig,
		},
		{
			URL:             "https://xg2g.example.com:443",
			Kind:            EndpointKindPublicHTTPS,
			Priority:        2,
			TLSMode:         TLSModeRequired,
			AllowWeb:        true,
			AllowNative:     true,
			AdvertiseReason: "public-duplicate",
			Source:          EndpointSourceConfig,
		},
	}

	_, err := NewProvider(ProviderOptions{}).Build(specs)
	if !errors.Is(err, ErrEndpointDuplicate) {
		t.Fatalf("expected duplicate endpoint error, got %v", err)
	}
}

func TestProviderBuild_NormalizesAndSortsPublishedEndpoints(t *testing.T) {
	specs := []PublishedEndpoint{
		{
			URL:             "https://xg2g.local:7443/",
			Kind:            EndpointKindLocalHTTPS,
			Priority:        20,
			TLSMode:         TLSModeRequired,
			AllowPairing:    true,
			AllowStreaming:  true,
			AllowNative:     true,
			AdvertiseReason: "lan-https",
			Source:          EndpointSourceEnv,
		},
		{
			URL:             "https://XG2G.EXAMPLE.COM:443",
			Kind:            EndpointKindPublicHTTPS,
			Priority:        10,
			TLSMode:         TLSModeRequired,
			AllowPairing:    true,
			AllowStreaming:  true,
			AllowWeb:        true,
			AllowNative:     true,
			AdvertiseReason: "public-https",
			Source:          EndpointSourceConfig,
		},
		{
			URL:             "http://192.168.1.20:8080/",
			Kind:            EndpointKindLocalHTTP,
			Priority:        30,
			TLSMode:         TLSModeProhibited,
			AllowStreaming:  true,
			AllowNative:     true,
			AdvertiseReason: "lan-http",
			Source:          EndpointSourceOperator,
		},
	}

	got, err := NewProvider(ProviderOptions{AllowLocalHTTP: true}).Build(specs)
	if err != nil {
		t.Fatalf("build published endpoints: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(got))
	}

	if got[0].URL != "https://xg2g.example.com" {
		t.Fatalf("expected canonical public endpoint first, got %#v", got[0])
	}
	if got[1].URL != "https://xg2g.local:7443" {
		t.Fatalf("expected local https second, got %#v", got[1])
	}
	if got[2].URL != "http://192.168.1.20:8080" {
		t.Fatalf("expected canonical local http third, got %#v", got[2])
	}
	if got[0].Source != EndpointSourceConfig || got[2].Source != EndpointSourceOperator {
		t.Fatalf("expected normalized sources, got %#v", got)
	}
}

func withURL(spec PublishedEndpoint, raw string) PublishedEndpoint {
	spec.URL = raw
	return spec
}
