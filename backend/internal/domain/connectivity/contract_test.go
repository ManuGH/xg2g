package connectivity

import "testing"

func TestEvaluateContractProfileMatrix(t *testing.T) {
	t.Parallel()

	publicEndpoint := PublishedEndpoint{
		URL:             "https://tv.example.net",
		Kind:            EndpointKindPublicHTTPS,
		Priority:        10,
		TLSMode:         TLSModeRequired,
		AllowPairing:    true,
		AllowStreaming:  true,
		AllowWeb:        true,
		AllowNative:     true,
		AdvertiseReason: "public origin",
		Source:          EndpointSourceConfig,
	}

	tests := []struct {
		name             string
		input            ContractInput
		wantSeverity     FindingSeverity
		wantStartupFatal bool
		wantReadyBlocked bool
		wantPairingBlock bool
		wantWebBlocked   bool
	}{
		{
			name: "lan without published endpoints stays non-blocking",
			input: ContractInput{
				Profile: DeploymentProfileLAN,
			},
			wantSeverity: FindingSeverityOK,
		},
		{
			name: "reverse proxy without trusted proxies is fatal",
			input: ContractInput{
				Profile:            DeploymentProfileReverseProxy,
				PublishedEndpoints: []PublishedEndpoint{publicEndpoint},
				AllowedOrigins:     []string{"https://tv.example.net"},
			},
			wantSeverity:     FindingSeverityFatal,
			wantStartupFatal: true,
			wantReadyBlocked: true,
			wantPairingBlock: true,
			wantWebBlocked:   true,
		},
		{
			name: "public profile missing native endpoint blocks pairing only",
			input: ContractInput{
				Profile: DeploymentProfileTunnel,
				PublishedEndpoints: []PublishedEndpoint{
					{
						URL:             publicEndpoint.URL,
						Kind:            publicEndpoint.Kind,
						Priority:        publicEndpoint.Priority,
						TLSMode:         publicEndpoint.TLSMode,
						AllowPairing:    true,
						AllowStreaming:  true,
						AllowWeb:        true,
						AllowNative:     false,
						AdvertiseReason: publicEndpoint.AdvertiseReason,
						Source:          publicEndpoint.Source,
					},
				},
				AllowedOrigins: []string{"https://tv.example.net"},
				TrustedProxies: []string{"127.0.0.1/32"},
			},
			wantSeverity:     FindingSeverityDegraded,
			wantReadyBlocked: false,
			wantPairingBlock: true,
			wantWebBlocked:   false,
		},
		{
			name: "vps without tls is fatal",
			input: ContractInput{
				Profile:            DeploymentProfileVPS,
				PublishedEndpoints: []PublishedEndpoint{publicEndpoint},
				AllowedOrigins:     []string{"https://tv.example.net"},
			},
			wantSeverity:     FindingSeverityFatal,
			wantStartupFatal: true,
			wantReadyBlocked: true,
			wantPairingBlock: true,
			wantWebBlocked:   true,
		},
		{
			name: "public allowLocalHTTP stays warn when public endpoint exists",
			input: ContractInput{
				Profile:        DeploymentProfileTunnel,
				AllowLocalHTTP: true,
				PublishedEndpoints: []PublishedEndpoint{
					publicEndpoint,
					{
						URL:             "http://192.168.1.20:8088",
						Kind:            EndpointKindLocalHTTP,
						Priority:        20,
						TLSMode:         TLSModeProhibited,
						AllowPairing:    true,
						AllowStreaming:  true,
						AllowNative:     true,
						AdvertiseReason: "lan fallback",
						Source:          EndpointSourceConfig,
					},
				},
				AllowedOrigins: []string{"https://tv.example.net"},
				TrustedProxies: []string{"127.0.0.1/32"},
			},
			wantSeverity: FindingSeverityWarn,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			report := EvaluateContract(tt.input)

			if report.Severity != tt.wantSeverity {
				t.Fatalf("severity = %q, want %q", report.Severity, tt.wantSeverity)
			}
			if report.StartupFatal() != tt.wantStartupFatal {
				t.Fatalf("startup fatal = %t, want %t", report.StartupFatal(), tt.wantStartupFatal)
			}
			if report.ReadinessBlocked() != tt.wantReadyBlocked {
				t.Fatalf("readiness blocked = %t, want %t", report.ReadinessBlocked(), tt.wantReadyBlocked)
			}
			if report.PairingBlocked() != tt.wantPairingBlock {
				t.Fatalf("pairing blocked = %t, want %t", report.PairingBlocked(), tt.wantPairingBlock)
			}
			if report.WebBlocked() != tt.wantWebBlocked {
				t.Fatalf("web blocked = %t, want %t", report.WebBlocked(), tt.wantWebBlocked)
			}
		})
	}
}

func TestEvaluateContractPublicBrowserTrust(t *testing.T) {
	t.Parallel()

	endpoint := PublishedEndpoint{
		URL:             "https://tv.example.net",
		Kind:            EndpointKindPublicHTTPS,
		Priority:        10,
		TLSMode:         TLSModeRequired,
		AllowPairing:    true,
		AllowStreaming:  true,
		AllowWeb:        true,
		AllowNative:     true,
		AdvertiseReason: "public origin",
		Source:          EndpointSourceConfig,
	}

	t.Run("wildcard origins are fatal in public profiles", func(t *testing.T) {
		t.Parallel()

		report := EvaluateContract(ContractInput{
			Profile:            DeploymentProfileReverseProxy,
			PublishedEndpoints: []PublishedEndpoint{endpoint},
			AllowedOrigins:     []string{"*"},
			TrustedProxies:     []string{"127.0.0.1/32"},
		})

		if !report.StartupFatal() {
			t.Fatalf("expected wildcard allowed origins to be startup fatal, got %#v", report.Findings)
		}
	})

	t.Run("missing explicit web origin blocks web only", func(t *testing.T) {
		t.Parallel()

		report := EvaluateContract(ContractInput{
			Profile:            DeploymentProfileReverseProxy,
			PublishedEndpoints: []PublishedEndpoint{endpoint},
			AllowedOrigins:     []string{"https://other.example.net"},
			TrustedProxies:     []string{"127.0.0.1/32"},
		})

		if !report.WebBlocked() {
			t.Fatalf("expected missing allowed origin to block web, got %#v", report.Findings)
		}
		if report.PairingBlocked() {
			t.Fatalf("missing web allowed origin must not block pairing, got %#v", report.Findings)
		}
		if report.ReadinessBlocked() {
			t.Fatalf("missing web allowed origin must not block readiness, got %#v", report.Findings)
		}
	})
}

func TestEvaluateContractRejectsExtremelyBroadPrivateProxyTrustInPublicProfiles(t *testing.T) {
	t.Parallel()

	endpoint := PublishedEndpoint{
		URL:             "https://tv.example.net",
		Kind:            EndpointKindPublicHTTPS,
		Priority:        10,
		TLSMode:         TLSModeRequired,
		AllowPairing:    true,
		AllowStreaming:  true,
		AllowWeb:        true,
		AllowNative:     true,
		AdvertiseReason: "public origin",
		Source:          EndpointSourceConfig,
	}

	for _, trustedProxy := range []string{"10.0.0.0/8", "172.16.0.0/12", "fd00::/8"} {
		trustedProxy := trustedProxy
		t.Run(trustedProxy, func(t *testing.T) {
			t.Parallel()
			report := EvaluateContract(ContractInput{
				Profile:            DeploymentProfileReverseProxy,
				PublishedEndpoints: []PublishedEndpoint{endpoint},
				AllowedOrigins:     []string{"https://tv.example.net"},
				TrustedProxies:     []string{trustedProxy},
			})
			if !report.StartupFatal() {
				t.Fatalf("expected %s to be startup fatal, got %#v", trustedProxy, report.Findings)
			}
			if finding := report.BlockingFinding(FindingScopeStartup); finding == nil ||
				finding.Code != "connectivity.trusted_proxies.too_broad" {
				t.Fatalf("expected broad proxy finding for %s, got %#v", trustedProxy, report.Findings)
			}
		})
	}
}

func TestEvaluateContractAllowsNarrowPrivateProxyNetwork(t *testing.T) {
	t.Parallel()

	report := EvaluateContract(ContractInput{
		Profile: DeploymentProfileReverseProxy,
		PublishedEndpoints: []PublishedEndpoint{{
			URL:             "https://tv.example.net",
			Kind:            EndpointKindPublicHTTPS,
			Priority:        10,
			TLSMode:         TLSModeRequired,
			AllowPairing:    true,
			AllowStreaming:  true,
			AllowWeb:        true,
			AllowNative:     true,
			AdvertiseReason: "public origin",
			Source:          EndpointSourceConfig,
		}},
		AllowedOrigins: []string{"https://tv.example.net"},
		TrustedProxies: []string{"172.31.0.0/24"},
	})
	if report.StartupFatal() {
		t.Fatalf("narrow private proxy network unexpectedly fatal: %#v", report.Findings)
	}
}
