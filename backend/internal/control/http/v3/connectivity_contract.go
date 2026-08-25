// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func (s *Server) connectivityContractReport() (connectivitydomain.ContractReport, error) {
	return config.BuildConnectivityContract(s.GetConfig())
}

func (s *Server) enforceConnectivityScope(w http.ResponseWriter, r *http.Request, scope connectivitydomain.FindingScope) bool {
	report, err := s.connectivityContractReport()
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", problemcode.CodeUnavailable, "Connectivity contract evaluation failed", nil)
		return false
	}
	if !report.Public {
		return true
	}
	if !scopeBlockedByContract(report, scope) {
		return true
	}

	detail := "Public deployment contract blocks this operation."
	extra := map[string]any{
		"profile":  string(report.Profile),
		"severity": string(report.Severity),
		"scope":    string(scope),
	}
	if finding := report.BlockingFinding(scope); finding != nil {
		extra["findingCode"] = finding.Code
		if strings.TrimSpace(finding.Detail) != "" {
			detail = finding.Detail
		} else if strings.TrimSpace(finding.Summary) != "" {
			detail = finding.Summary
		}
	}

	writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "connectivity/contract_blocked", "Public Connectivity Contract Blocked", problemcode.CodeUnavailable, detail, extra)
	return false
}

func scopeBlockedByContract(report connectivitydomain.ContractReport, scope connectivitydomain.FindingScope) bool {
	switch scope {
	case connectivitydomain.FindingScopePairing:
		return report.PairingBlocked()
	case connectivitydomain.FindingScopeWeb:
		return report.WebBlocked()
	case connectivitydomain.FindingScopeReadiness:
		return report.ReadinessBlocked()
	default:
		if finding := report.BlockingFinding(scope); finding != nil {
			return true
		}
		return false
	}
}

func (s *Server) GetSystemConnectivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHouseholdSettingsAccess(w, r); !ok {
		return
	}

	report, err := s.connectivityContractReport()
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", problemcode.CodeUnavailable, "Connectivity contract evaluation failed", nil)
		return
	}

	writeJSON(w, http.StatusOK, ConnectivityContract{
		Profile:            ConnectivityDeploymentProfile(report.Profile),
		Public:             report.Public,
		Status:             ConnectivityContractStatus(report.Severity),
		StartupFatal:       report.StartupFatal(),
		ReadinessBlocked:   report.ReadinessBlocked(),
		PairingBlocked:     report.PairingBlocked(),
		WebBlocked:         report.WebBlocked(),
		AllowLocalHTTP:     report.AllowLocalHTTP,
		TlsEnabled:         report.TLSEnabled,
		ForceHTTPS:         report.ForceHTTPS,
		AllowedOrigins:     append([]string(nil), report.AllowedOrigins...),
		TrustedProxies:     append([]string(nil), report.TrustedProxies...),
		PublishedEndpoints: mapPublishedEndpointContracts(report.PublishedEndpoints),
		Selections:         mapConnectivitySelections(report.Selections),
		Findings:           mapConnectivityFindings(report.Findings),
		Request:            s.connectivityRequestResponse(r),
	})
}

func (s *Server) connectivityRequestResponse(r *http.Request) ConnectivityRequest {
	if r == nil {
		return ConnectivityRequest{AcceptedProxyHeaders: []string{"X-Forwarded-Proto"}}
	}

	remoteIP := requestRemoteIP(r)
	trustedProxyMatch := false
	trustedProxies, err := middleware.ParseCIDRs(splitCSVNonEmpty(strings.TrimSpace(s.GetConfig().TrustedProxies)))
	if err == nil && remoteIP != nil {
		trustedProxyMatch = middleware.IsIPAllowed(remoteIP, trustedProxies)
	}

	requestOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	originAllowed, allowAll := originAllowedByConfig(s.GetConfig().AllowedOrigins, requestOrigin)
	var originAllowedPtr *bool
	if requestOrigin != "" {
		originAllowedPtr = &originAllowed
	}

	schemeSource := "direct_http"
	switch {
	case r.TLS != nil:
		schemeSource = "tls"
	case trustedProxyMatch && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"):
		schemeSource = "trusted_x_forwarded_proto"
	}

	response := ConnectivityRequest{
		RemoteAddr:           optionalStringPtr(strings.TrimSpace(r.RemoteAddr)),
		RemoteIsLoopback:     requestRemoteIsLoopback(r),
		TlsDirect:            r.TLS != nil,
		TrustedProxyMatch:    trustedProxyMatch,
		EffectiveHttps:       s.requestIsHTTPS(r),
		SchemeSource:         schemeSource,
		AcceptedProxyHeaders: []string{"X-Forwarded-Proto"},
		XForwardedProto:      optionalStringPtr(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))),
		XForwardedHost:       optionalStringPtr(strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))),
		XForwardedFor:        optionalStringPtr(strings.TrimSpace(r.Header.Get("X-Forwarded-For"))),
		Origin:               optionalStringPtr(requestOrigin),
		OriginAllowed:        originAllowedPtr,
		OriginAllowAll:       allowAll,
	}
	if remoteIP != nil {
		response.RemoteIp = optionalStringPtr(remoteIP.String())
	}
	return response
}

func mapConnectivitySelections(selections connectivitydomain.ContractSelections) ConnectivitySelections {
	return ConnectivitySelections{
		Web:           mapConnectivitySelection(selections.Web),
		WebPublic:     mapConnectivitySelection(selections.WebPublic),
		Native:        mapConnectivitySelection(selections.Native),
		NativePublic:  mapConnectivitySelection(selections.NativePublic),
		Pairing:       mapConnectivitySelection(selections.Pairing),
		PairingPublic: mapConnectivitySelection(selections.PairingPublic),
		Streaming:     mapConnectivitySelection(selections.Streaming),
	}
}

func mapConnectivitySelection(selection connectivitydomain.EndpointSelection) ConnectivitySelection {
	resp := ConnectivitySelection{
		Reason: optionalStringPtr(selection.Reason),
	}
	if selection.Endpoint != nil {
		// Reuse the one endpoint mapper rather than a second inline copy: the
		// duplicate that used to live here is what let priority skip the int32
		// clamp the contract declares.
		mapped := mapPublishedEndpointContracts([]connectivitydomain.PublishedEndpoint{*selection.Endpoint})
		resp.Endpoint = &mapped[0]
	}
	return resp
}

func mapConnectivityFindings(findings []connectivitydomain.ContractFinding) []ConnectivityFinding {
	if len(findings) == 0 {
		return []ConnectivityFinding{}
	}

	resp := make([]ConnectivityFinding, 0, len(findings))
	for _, finding := range findings {
		scopes := make([]string, 0, len(finding.Scopes))
		for _, scope := range finding.Scopes {
			scopes = append(scopes, string(scope))
		}
		resp = append(resp, ConnectivityFinding{
			Code:        finding.Code,
			Severity:    ConnectivityFindingSeverity(finding.Severity),
			Scopes:      scopes,
			Field:       optionalStringPtr(finding.Field),
			Summary:     finding.Summary,
			Detail:      optionalStringPtr(finding.Detail),
			EndpointUrl: optionalStringPtr(finding.EndpointURL),
		})
	}
	return resp
}

func originAllowedByConfig(allowedOrigins []string, origin string) (bool, bool) {
	if strings.TrimSpace(origin) == "" {
		return false, false
	}
	allowAll := false
	for _, allowed := range allowedOrigins {
		switch strings.TrimSpace(allowed) {
		case "*":
			allowAll = true
		case origin:
			return true, allowAll
		}
	}
	return allowAll, allowAll
}
