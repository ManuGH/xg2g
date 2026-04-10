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

type connectivitySelectionResponse struct {
	Endpoint *publishedEndpointResponse `json:"endpoint,omitempty"`
	Reason   string                     `json:"reason,omitempty"`
}

type connectivityFindingResponse struct {
	Code        string   `json:"code"`
	Severity    string   `json:"severity"`
	Scopes      []string `json:"scopes"`
	Field       string   `json:"field,omitempty"`
	Summary     string   `json:"summary"`
	Detail      string   `json:"detail,omitempty"`
	EndpointUrl string   `json:"endpointUrl,omitempty"`
}

type connectivityRequestResponse struct {
	RemoteAddr           string   `json:"remoteAddr,omitempty"`
	RemoteIP             string   `json:"remoteIp,omitempty"`
	RemoteIsLoopback     bool     `json:"remoteIsLoopback"`
	TlsDirect            bool     `json:"tlsDirect"`
	TrustedProxyMatch    bool     `json:"trustedProxyMatch"`
	EffectiveHTTPS       bool     `json:"effectiveHttps"`
	SchemeSource         string   `json:"schemeSource"`
	AcceptedProxyHeaders []string `json:"acceptedProxyHeaders"`
	XForwardedProto      string   `json:"xForwardedProto,omitempty"`
	XForwardedHost       string   `json:"xForwardedHost,omitempty"`
	XForwardedFor        string   `json:"xForwardedFor,omitempty"`
	Origin               string   `json:"origin,omitempty"`
	OriginAllowed        *bool    `json:"originAllowed,omitempty"`
	OriginAllowAll       bool     `json:"originAllowAll"`
}

type connectivityContractResponse struct {
	Profile            string                         `json:"profile"`
	Public             bool                           `json:"public"`
	Status             string                         `json:"status"`
	StartupFatal       bool                           `json:"startupFatal"`
	ReadinessBlocked   bool                           `json:"readinessBlocked"`
	PairingBlocked     bool                           `json:"pairingBlocked"`
	WebBlocked         bool                           `json:"webBlocked"`
	AllowLocalHTTP     bool                           `json:"allowLocalHTTP"`
	TLSEnabled         bool                           `json:"tlsEnabled"`
	ForceHTTPS         bool                           `json:"forceHTTPS"`
	AllowedOrigins     []string                       `json:"allowedOrigins"`
	TrustedProxies     []string                       `json:"trustedProxies"`
	PublishedEndpoints []publishedEndpointResponse    `json:"publishedEndpoints"`
	Selections         connectivitySelectionsResponse `json:"selections"`
	Findings           []connectivityFindingResponse  `json:"findings"`
	Request            connectivityRequestResponse    `json:"request"`
}

type connectivitySelectionsResponse struct {
	Web           connectivitySelectionResponse `json:"web"`
	WebPublic     connectivitySelectionResponse `json:"webPublic"`
	Native        connectivitySelectionResponse `json:"native"`
	NativePublic  connectivitySelectionResponse `json:"nativePublic"`
	Pairing       connectivitySelectionResponse `json:"pairing"`
	PairingPublic connectivitySelectionResponse `json:"pairingPublic"`
	Streaming     connectivitySelectionResponse `json:"streaming"`
}

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

	writeJSON(w, http.StatusOK, connectivityContractResponse{
		Profile:            string(report.Profile),
		Public:             report.Public,
		Status:             string(report.Severity),
		StartupFatal:       report.StartupFatal(),
		ReadinessBlocked:   report.ReadinessBlocked(),
		PairingBlocked:     report.PairingBlocked(),
		WebBlocked:         report.WebBlocked(),
		AllowLocalHTTP:     report.AllowLocalHTTP,
		TLSEnabled:         report.TLSEnabled,
		ForceHTTPS:         report.ForceHTTPS,
		AllowedOrigins:     append([]string(nil), report.AllowedOrigins...),
		TrustedProxies:     append([]string(nil), report.TrustedProxies...),
		PublishedEndpoints: mapPublishedEndpointResponses(report.PublishedEndpoints),
		Selections:         mapConnectivitySelections(report.Selections),
		Findings:           mapConnectivityFindings(report.Findings),
		Request:            s.connectivityRequestResponse(r),
	})
}

func (s *Server) connectivityRequestResponse(r *http.Request) connectivityRequestResponse {
	if r == nil {
		return connectivityRequestResponse{AcceptedProxyHeaders: []string{"X-Forwarded-Proto"}}
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

	response := connectivityRequestResponse{
		RemoteAddr:           strings.TrimSpace(r.RemoteAddr),
		RemoteIsLoopback:     requestRemoteIsLoopback(r),
		TlsDirect:            r.TLS != nil,
		TrustedProxyMatch:    trustedProxyMatch,
		EffectiveHTTPS:       s.requestIsHTTPS(r),
		SchemeSource:         schemeSource,
		AcceptedProxyHeaders: []string{"X-Forwarded-Proto"},
		XForwardedProto:      strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")),
		XForwardedHost:       strings.TrimSpace(r.Header.Get("X-Forwarded-Host")),
		XForwardedFor:        strings.TrimSpace(r.Header.Get("X-Forwarded-For")),
		Origin:               requestOrigin,
		OriginAllowed:        originAllowedPtr,
		OriginAllowAll:       allowAll,
	}
	if remoteIP != nil {
		response.RemoteIP = remoteIP.String()
	}
	return response
}

func mapConnectivitySelections(selections connectivitydomain.ContractSelections) connectivitySelectionsResponse {
	return connectivitySelectionsResponse{
		Web:           mapConnectivitySelection(selections.Web),
		WebPublic:     mapConnectivitySelection(selections.WebPublic),
		Native:        mapConnectivitySelection(selections.Native),
		NativePublic:  mapConnectivitySelection(selections.NativePublic),
		Pairing:       mapConnectivitySelection(selections.Pairing),
		PairingPublic: mapConnectivitySelection(selections.PairingPublic),
		Streaming:     mapConnectivitySelection(selections.Streaming),
	}
}

func mapConnectivitySelection(selection connectivitydomain.EndpointSelection) connectivitySelectionResponse {
	resp := connectivitySelectionResponse{
		Reason: selection.Reason,
	}
	if selection.Endpoint != nil {
		endpoint := *selection.Endpoint
		resp.Endpoint = &publishedEndpointResponse{
			URL:             endpoint.URL,
			Kind:            string(endpoint.Kind),
			Priority:        endpoint.Priority,
			TLSMode:         string(endpoint.TLSMode),
			AllowPairing:    endpoint.AllowPairing,
			AllowStreaming:  endpoint.AllowStreaming,
			AllowWeb:        endpoint.AllowWeb,
			AllowNative:     endpoint.AllowNative,
			AdvertiseReason: endpoint.AdvertiseReason,
			Source:          string(endpoint.Source),
		}
	}
	return resp
}

func mapConnectivityFindings(findings []connectivitydomain.ContractFinding) []connectivityFindingResponse {
	if len(findings) == 0 {
		return []connectivityFindingResponse{}
	}

	resp := make([]connectivityFindingResponse, 0, len(findings))
	for _, finding := range findings {
		scopes := make([]string, 0, len(finding.Scopes))
		for _, scope := range finding.Scopes {
			scopes = append(scopes, string(scope))
		}
		resp = append(resp, connectivityFindingResponse{
			Code:        finding.Code,
			Severity:    string(finding.Severity),
			Scopes:      scopes,
			Field:       finding.Field,
			Summary:     finding.Summary,
			Detail:      finding.Detail,
			EndpointUrl: finding.EndpointURL,
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
