// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package connectivity

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

type DeploymentProfile string

const (
	DeploymentProfileLAN          DeploymentProfile = "lan"
	DeploymentProfileReverseProxy DeploymentProfile = "reverse_proxy"
	DeploymentProfileTunnel       DeploymentProfile = "tunnel"
	DeploymentProfileVPS          DeploymentProfile = "vps"
)

type FindingSeverity string

const (
	FindingSeverityOK       FindingSeverity = "ok"
	FindingSeverityWarn     FindingSeverity = "warn"
	FindingSeverityDegraded FindingSeverity = "degraded"
	FindingSeverityFatal    FindingSeverity = "fatal"
)

type FindingScope string

const (
	FindingScopeGeneral   FindingScope = "general"
	FindingScopeStartup   FindingScope = "startup"
	FindingScopeReadiness FindingScope = "readiness"
	FindingScopePairing   FindingScope = "pairing"
	FindingScopeWeb       FindingScope = "web"
)

type ContractInput struct {
	Profile            DeploymentProfile
	AllowLocalHTTP     bool
	PublishedEndpoints []PublishedEndpoint
	AllowedOrigins     []string
	TrustedProxies     []string
	TLSEnabled         bool
	ForceHTTPS         bool
}

type ContractFinding struct {
	Code        string
	Severity    FindingSeverity
	Scopes      []FindingScope
	Field       string
	Summary     string
	Detail      string
	EndpointURL string
}

type EndpointSelection struct {
	Endpoint *PublishedEndpoint
	Reason   string
}

type ContractSelections struct {
	Web           EndpointSelection
	WebPublic     EndpointSelection
	Native        EndpointSelection
	NativePublic  EndpointSelection
	Pairing       EndpointSelection
	PairingPublic EndpointSelection
	Streaming     EndpointSelection
}

type ContractReport struct {
	Profile            DeploymentProfile
	Public             bool
	Severity           FindingSeverity
	AllowLocalHTTP     bool
	TLSEnabled         bool
	ForceHTTPS         bool
	AllowedOrigins     []string
	TrustedProxies     []string
	PublishedEndpoints []PublishedEndpoint
	Selections         ContractSelections
	Findings           []ContractFinding
}

func EvaluateContract(input ContractInput) ContractReport {
	profile := normalizeDeploymentProfile(input.Profile)
	report := ContractReport{
		Profile:            profile,
		Public:             profile != DeploymentProfileLAN,
		Severity:           FindingSeverityOK,
		AllowLocalHTTP:     input.AllowLocalHTTP,
		TLSEnabled:         input.TLSEnabled,
		ForceHTTPS:         input.ForceHTTPS,
		AllowedOrigins:     cloneStrings(input.AllowedOrigins),
		TrustedProxies:     normalizeTrustedProxies(input.TrustedProxies),
		PublishedEndpoints: ClonePublishedEndpoints(input.PublishedEndpoints),
	}
	report.Selections = buildSelections(report.PublishedEndpoints)

	if !isKnownDeploymentProfile(profile) {
		report.addFinding(ContractFinding{
			Code:     "connectivity.profile.invalid",
			Severity: FindingSeverityFatal,
			Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
			Field:    "Connectivity.Profile",
			Summary:  "connectivity profile is invalid",
			Detail:   "Supported profiles are lan, reverse_proxy, tunnel, and vps.",
		})
		return report.finalize()
	}

	if isTrustedProxyBroad(report.TrustedProxies) {
		report.addFinding(ContractFinding{
			Code:     "connectivity.trusted_proxies.too_broad",
			Severity: FindingSeverityFatal,
			Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
			Field:    "TrustedProxies",
			Summary:  "trustedProxies is too broad for a public deployment contract",
			Detail:   "Do not trust every remote address for X-Forwarded-* headers. Use explicit proxy CIDRs only.",
		})
	}

	if report.Public {
		evaluatePublicContract(&report)
		evaluatePublicBrowserTrust(&report)
	}

	switch profile {
	case DeploymentProfileReverseProxy:
		if len(report.TrustedProxies) == 0 {
			report.addFinding(ContractFinding{
				Code:     "connectivity.reverse_proxy.trusted_proxies_required",
				Severity: FindingSeverityFatal,
				Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
				Field:    "TrustedProxies",
				Summary:  "reverse_proxy profile requires trustedProxies",
				Detail:   "Explicit proxy CIDRs are required so X-Forwarded-Proto is trusted only from the reverse proxy.",
			})
		}
	case DeploymentProfileTunnel:
		if len(report.TrustedProxies) == 0 {
			report.addFinding(ContractFinding{
				Code:     "connectivity.tunnel.trusted_proxies_required",
				Severity: FindingSeverityFatal,
				Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
				Field:    "TrustedProxies",
				Summary:  "tunnel profile requires trustedProxies",
				Detail:   "Explicit proxy CIDRs are required so HTTPS offload is trusted only from the tunnel ingress.",
			})
		}
	case DeploymentProfileVPS:
		if !report.TLSEnabled {
			report.addFinding(ContractFinding{
				Code:     "connectivity.vps.tls_required",
				Severity: FindingSeverityFatal,
				Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
				Field:    "TLSEnabled",
				Summary:  "vps profile requires direct TLS",
				Detail:   "Use reverse_proxy instead when TLS is terminated upstream.",
			})
		}
		if len(report.TrustedProxies) > 0 {
			report.addFinding(ContractFinding{
				Code:     "connectivity.vps.trusted_proxies_present",
				Severity: FindingSeverityWarn,
				Scopes:   []FindingScope{FindingScopeGeneral},
				Field:    "TrustedProxies",
				Summary:  "vps profile still declares trusted proxies",
				Detail:   "If public HTTPS is terminated upstream, prefer the reverse_proxy profile so the contract stays explicit.",
			})
		}
	}

	if report.Public && report.AllowLocalHTTP {
		detail := "local_http remains disabled for browsers and must stay an explicit operator choice."
		if report.Selections.Native.Endpoint != nil && report.Selections.Native.Endpoint.Kind == EndpointKindLocalHTTP {
			detail = "A local_http endpoint is published alongside public truth. This is allowed only as an explicit supplemental native-only path."
		}
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.allow_local_http_enabled",
			Severity: FindingSeverityWarn,
			Scopes:   []FindingScope{FindingScopeGeneral},
			Field:    "Connectivity.AllowLocalHTTP",
			Summary:  "allowLocalHTTP is enabled in a public profile",
			Detail:   detail,
		})
	}

	return report.finalize()
}

func evaluatePublicBrowserTrust(report *ContractReport) {
	if originListAllowsAll(report.AllowedOrigins) {
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.allowed_origins_wildcard",
			Severity: FindingSeverityFatal,
			Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
			Field:    "AllowedOrigins",
			Summary:  "public profile must not allow wildcard browser origins",
			Detail:   "Set allowedOrigins to explicit published HTTPS origins. Wildcard CORS is not allowed in public profiles.",
		})
	}

	if report.Selections.WebPublic.Endpoint == nil {
		return
	}

	webOrigin := endpointOrigin(report.Selections.WebPublic.Endpoint.URL)
	if webOrigin == "" {
		return
	}
	if !originListContains(report.AllowedOrigins, webOrigin) {
		report.addFinding(ContractFinding{
			Code:        "connectivity.public.web_origin_not_allowed",
			Severity:    FindingSeverityDegraded,
			Scopes:      []FindingScope{FindingScopeWeb},
			Field:       "AllowedOrigins",
			Summary:     "public web endpoint is not listed in allowedOrigins",
			Detail:      "Browser state-changing requests in public profiles must use an explicit allowed origin matching the published web endpoint.",
			EndpointURL: report.Selections.WebPublic.Endpoint.URL,
		})
	}
}

func (r ContractReport) StartupFatal() bool {
	return r.scopeHasSeverity(FindingScopeStartup, FindingSeverityFatal)
}

func (r ContractReport) ReadinessBlocked() bool {
	return r.scopeBlocked(FindingScopeReadiness)
}

func (r ContractReport) PairingBlocked() bool {
	return r.scopeBlocked(FindingScopePairing)
}

func (r ContractReport) WebBlocked() bool {
	return r.scopeBlocked(FindingScopeWeb)
}

func (r ContractReport) BlockingFinding(scope FindingScope) *ContractFinding {
	for _, finding := range r.Findings {
		if !hasScope(finding.Scopes, scope) {
			continue
		}
		if finding.Severity == FindingSeverityFatal || finding.Severity == FindingSeverityDegraded {
			finding := finding
			return &finding
		}
	}
	return nil
}

func (r ContractReport) scopeBlocked(scope FindingScope) bool {
	return r.scopeHasSeverity(scope, FindingSeverityFatal) || r.scopeHasSeverity(scope, FindingSeverityDegraded)
}

func (r ContractReport) scopeHasSeverity(scope FindingScope, severity FindingSeverity) bool {
	for _, finding := range r.Findings {
		if finding.Severity != severity {
			continue
		}
		if hasScope(finding.Scopes, scope) {
			return true
		}
	}
	return false
}

func (r *ContractReport) addFinding(finding ContractFinding) {
	if len(finding.Scopes) == 0 {
		finding.Scopes = []FindingScope{FindingScopeGeneral}
	}
	if finding.Summary == "" {
		finding.Summary = finding.Code
	}
	r.Findings = append(r.Findings, finding)
}

func (r ContractReport) finalize() ContractReport {
	slices.SortFunc(r.Findings, func(a, b ContractFinding) int {
		if severityRank(a.Severity) != severityRank(b.Severity) {
			if severityRank(a.Severity) > severityRank(b.Severity) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Code, b.Code)
	})

	maxSeverity := FindingSeverityOK
	for _, finding := range r.Findings {
		if severityRank(finding.Severity) > severityRank(maxSeverity) {
			maxSeverity = finding.Severity
		}
	}
	r.Severity = maxSeverity
	return r
}

func evaluatePublicContract(report *ContractReport) {
	if len(report.PublishedEndpoints) == 0 {
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.endpoints_required",
			Severity: FindingSeverityFatal,
			Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
			Field:    "Connectivity.PublishedEndpoints",
			Summary:  "public profile requires publishedEndpoints",
			Detail:   "Declare at least one externally reachable HTTPS origin so clients never infer topology.",
		})
		return
	}

	if report.Selections.WebPublic.Endpoint == nil &&
		report.Selections.NativePublic.Endpoint == nil &&
		report.Selections.PairingPublic.Endpoint == nil {
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.https_endpoint_required",
			Severity: FindingSeverityFatal,
			Scopes:   []FindingScope{FindingScopeStartup, FindingScopeReadiness, FindingScopePairing, FindingScopeWeb},
			Field:    "Connectivity.PublishedEndpoints",
			Summary:  "public profile requires at least one public_https endpoint",
			Detail:   "Public deployment truth must include an externally reachable HTTPS origin.",
		})
	}

	if report.Selections.PairingPublic.Endpoint == nil {
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.pairing_endpoint_missing",
			Severity: FindingSeverityDegraded,
			Scopes:   []FindingScope{FindingScopePairing},
			Field:    "Connectivity.PublishedEndpoints",
			Summary:  "no public pairing endpoint is published",
			Detail:   "Pairing is blocked because no public_https endpoint allows pairing.",
		})
	}

	if report.Selections.NativePublic.Endpoint == nil {
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.native_endpoint_missing",
			Severity: FindingSeverityDegraded,
			Scopes:   []FindingScope{FindingScopePairing},
			Field:    "Connectivity.PublishedEndpoints",
			Summary:  "no public native endpoint is published",
			Detail:   "Native clients cannot be handed an externally valid origin because no public_https endpoint allows native access.",
		})
	}

	if report.Selections.WebPublic.Endpoint == nil {
		report.addFinding(ContractFinding{
			Code:     "connectivity.public.web_endpoint_missing",
			Severity: FindingSeverityDegraded,
			Scopes:   []FindingScope{FindingScopeWeb},
			Field:    "Connectivity.PublishedEndpoints",
			Summary:  "no public web endpoint is published",
			Detail:   "Web bootstrap is blocked because no public_https endpoint allows web access.",
		})
	}

	for _, endpoint := range report.PublishedEndpoints {
		if endpoint.Kind != EndpointKindPublicHTTPS && endpoint.AllowWeb {
			report.addFinding(ContractFinding{
				Code:        "connectivity.public.local_endpoint_allows_web",
				Severity:    FindingSeverityDegraded,
				Scopes:      []FindingScope{FindingScopeWeb},
				Field:       "Connectivity.PublishedEndpoints",
				Summary:     "a local endpoint is marked as web-capable in a public profile",
				Detail:      "Only public_https endpoints should be advertised to web clients in public mode.",
				EndpointURL: endpoint.URL,
			})
		}
	}
}

func buildSelections(endpoints []PublishedEndpoint) ContractSelections {
	return ContractSelections{
		Web: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool { return endpoint.AllowWeb }, "highest-priority endpoint with allow_web=true"),
		WebPublic: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool {
			return endpoint.Kind == EndpointKindPublicHTTPS && endpoint.AllowWeb
		}, "highest-priority public_https endpoint with allow_web=true"),
		Native: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool { return endpoint.AllowNative }, "highest-priority endpoint with allow_native=true"),
		NativePublic: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool {
			return endpoint.Kind == EndpointKindPublicHTTPS && endpoint.AllowNative
		}, "highest-priority public_https endpoint with allow_native=true"),
		Pairing: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool { return endpoint.AllowPairing }, "highest-priority endpoint with allow_pairing=true"),
		PairingPublic: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool {
			return endpoint.Kind == EndpointKindPublicHTTPS && endpoint.AllowPairing
		}, "highest-priority public_https endpoint with allow_pairing=true"),
		Streaming: selectEndpoint(endpoints, func(endpoint PublishedEndpoint) bool { return endpoint.AllowStreaming }, "highest-priority endpoint with allow_streaming=true"),
	}
}

func selectEndpoint(endpoints []PublishedEndpoint, match func(PublishedEndpoint) bool, reason string) EndpointSelection {
	for _, endpoint := range endpoints {
		if match(endpoint) {
			cloned := endpoint
			return EndpointSelection{
				Endpoint: &cloned,
				Reason:   reason,
			}
		}
	}
	return EndpointSelection{}
}

func normalizeDeploymentProfile(value DeploymentProfile) DeploymentProfile {
	trimmed := strings.ToLower(strings.TrimSpace(string(value)))
	if trimmed == "" {
		return DeploymentProfileLAN
	}
	return DeploymentProfile(trimmed)
}

func isKnownDeploymentProfile(value DeploymentProfile) bool {
	switch value {
	case DeploymentProfileLAN, DeploymentProfileReverseProxy, DeploymentProfileTunnel, DeploymentProfileVPS:
		return true
	default:
		return false
	}
}

func normalizeTrustedProxies(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func isTrustedProxyBroad(values []string) bool {
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		if prefix.Bits() == 0 {
			return true
		}
	}
	return false
}

func severityRank(value FindingSeverity) int {
	switch value {
	case FindingSeverityWarn:
		return 1
	case FindingSeverityDegraded:
		return 2
	case FindingSeverityFatal:
		return 3
	default:
		return 0
	}
}

func hasScope(scopes []FindingScope, want FindingScope) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

func originListAllowsAll(origins []string) bool {
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}
	return false
}

func originListContains(origins []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, origin := range origins {
		if normalizeOrigin(origin) == want {
			return true
		}
	}
	return false
}

func endpointOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return normalizeOrigin(parsed.Scheme + "://" + parsed.Host)
}

func normalizeOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	if (scheme == "http" && strings.HasSuffix(host, ":80")) || (scheme == "https" && strings.HasSuffix(host, ":443")) {
		host = strings.TrimSuffix(strings.TrimSuffix(host, ":80"), ":443")
	}
	return scheme + "://" + host
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
