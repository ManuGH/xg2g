// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

import (
	"fmt"
	"sort"
	"strings"
)

type ExposureClass string

const (
	ExposureClassRead    ExposureClass = "read"
	ExposureClassWrite   ExposureClass = "write"
	ExposureClassAdmin   ExposureClass = "admin"
	ExposureClassDevice  ExposureClass = "device"
	ExposureClassPairing ExposureClass = "pairing"
	ExposureClassSession ExposureClass = "session"
	ExposureClassHealth  ExposureClass = "health"
	ExposureClassSystem  ExposureClass = "system"
)

type ExposureAuthKind string

const (
	ExposureAuthBearerScope    ExposureAuthKind = "bearer_scope"
	ExposureAuthNone           ExposureAuthKind = "none"
	ExposureAuthPairingSecret  ExposureAuthKind = "pairing_secret"
	ExposureAuthDeviceGrant    ExposureAuthKind = "device_grant"
	ExposureAuthBootstrapToken ExposureAuthKind = "bootstrap_token"
)

type ExposureRateLimitClass string

const (
	ExposureRateLimitNone          ExposureRateLimitClass = "none"
	ExposureRateLimitGlobal        ExposureRateLimitClass = "global"
	ExposureRateLimitAuth          ExposureRateLimitClass = "auth"
	ExposureRateLimitPairingStart  ExposureRateLimitClass = "pairing_start"
	ExposureRateLimitPairingPoll   ExposureRateLimitClass = "pairing_poll"
	ExposureRateLimitPairingSecret ExposureRateLimitClass = "pairing_secret"
	ExposureRateLimitDeviceGrant   ExposureRateLimitClass = "device_grant"
	ExposureRateLimitBootstrap     ExposureRateLimitClass = "web_bootstrap"
)

type ExposureBrowserTrust string

const (
	ExposureBrowserTrustSameOrigin ExposureBrowserTrust = "same_origin_or_allowed_origin"
	ExposureBrowserTrustNotBrowser ExposureBrowserTrust = "not_browser"
	ExposureBrowserTrustNone       ExposureBrowserTrust = "none"
)

type ExposurePolicy struct {
	Class          ExposureClass
	AuthKind       ExposureAuthKind
	PublicAllowed  bool
	BrowserTrust   ExposureBrowserTrust
	RateLimitClass ExposureRateLimitClass
	AuditRequired  bool
	RedactErrors   bool
}

func policy(class ExposureClass, auth ExposureAuthKind, rate ExposureRateLimitClass, browser ExposureBrowserTrust, audit bool) ExposurePolicy {
	return ExposurePolicy{
		Class:          class,
		AuthKind:       auth,
		PublicAllowed:  true,
		BrowserTrust:   browser,
		RateLimitClass: rate,
		AuditRequired:  audit,
		RedactErrors:   class == ExposureClassPairing || class == ExposureClassDevice || auth != ExposureAuthBearerScope,
	}
}

var operationExposurePolicies = map[string]ExposurePolicy{
	"CreateSession":   policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"DeleteSession":   policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"ListSessions":    policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetSessionState": policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PostSessionHeartbeat": policy(
		ExposureClassSession,
		ExposureAuthBearerScope,
		ExposureRateLimitGlobal,
		ExposureBrowserTrustSameOrigin,
		false,
	),
	"ServeHLS":               policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"ServeHLSHead":           policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"ReportPlaybackFeedback": policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"CreateIntent":           policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"DeleteStreamsId":        policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"GetStreams":             policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"CreateDeviceSession":    policy(ExposureClassDevice, ExposureAuthDeviceGrant, ExposureRateLimitDeviceGrant, ExposureBrowserTrustNotBrowser, true),
	"CreateWebBootstrap":     policy(ExposureClassDevice, ExposureAuthBearerScope, ExposureRateLimitBootstrap, ExposureBrowserTrustSameOrigin, true),
	"CompleteWebBootstrap":   policy(ExposureClassDevice, ExposureAuthBootstrapToken, ExposureRateLimitBootstrap, ExposureBrowserTrustSameOrigin, true),
	"StartPairing":           policy(ExposureClassPairing, ExposureAuthNone, ExposureRateLimitPairingStart, ExposureBrowserTrustNotBrowser, true),
	"GetPairingStatus":       policy(ExposureClassPairing, ExposureAuthPairingSecret, ExposureRateLimitPairingPoll, ExposureBrowserTrustNotBrowser, true),
	"ApprovePairing":         policy(ExposureClassPairing, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"ExchangePairing":        policy(ExposureClassPairing, ExposureAuthPairingSecret, ExposureRateLimitPairingSecret, ExposureBrowserTrustNotBrowser, true),
	"GetErrors":              policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetDvrCapabilities":     policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetDvrStatus":           policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetEpg":                 policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetHouseholdUnlock":     policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetHouseholdProfiles":   policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetReceiverCurrent":     policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetRecordings":          policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetRecordingPlaybackInfo": policy(
		ExposureClassRead,
		ExposureAuthBearerScope,
		ExposureRateLimitGlobal,
		ExposureBrowserTrustSameOrigin,
		false,
	),
	"PostRecordingPlaybackInfo":        policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PostLivePlaybackInfo":             policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"StreamRecordingDirect":            policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"ProbeRecordingMp4":                policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetRecordingHLSPlaylist":          policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"GetRecordingHLSPlaylistHead":      policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"GetRecordingHLSTimeshift":         policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"GetRecordingHLSTimeshiftHead":     policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"GetRecordingHLSCustomSegment":     policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"GetRecordingHLSCustomSegmentHead": policy(ExposureClassSession, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustNotBrowser, false),
	"GetRecordingsRecordingIdStatus":   policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"DeleteRecording":                  policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"GetSeriesRules":                   policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"CreateSeriesRule":                 policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"RunAllSeriesRules":                policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"DeleteSeriesRule":                 policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"UpdateSeriesRule":                 policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"RunSeriesRule":                    policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"GetServices":                      policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetServicesBouquets":              policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PostServicesNowNext":              policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PostServicesIdToggle":             policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"PostHouseholdProfiles":            policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"PostHouseholdUnlock":              policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"PutHouseholdProfile":              policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"DeleteHouseholdProfile":           policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"DeleteHouseholdUnlock":            policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetSystemConfig":                  policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PutSystemConfig":                  policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetSystemConnectivity":            policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetSystemEntitlements":            policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PostSystemEntitlementReceipt":     policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"PostSystemEntitlementOverride":    policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"DeleteSystemEntitlementOverride":  policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetSystemHealth":                  policy(ExposureClassHealth, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetSystemHealthz":                 policy(ExposureClassHealth, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"GetSystemInfo":                    policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"PostSystemRefresh":                policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetSystemScanStatus":              policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"TriggerSystemScan":                policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetLogs":                          policy(ExposureClassAdmin, ExposureAuthBearerScope, ExposureRateLimitAuth, ExposureBrowserTrustSameOrigin, true),
	"GetTimers":                        policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"AddTimer":                         policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"PreviewConflicts":                 policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"DeleteTimer":                      policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
	"GetTimer":                         policy(ExposureClassRead, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, false),
	"UpdateTimer":                      policy(ExposureClassWrite, ExposureAuthBearerScope, ExposureRateLimitGlobal, ExposureBrowserTrustSameOrigin, true),
}

func ExposurePolicyForOperation(operationID string) (ExposurePolicy, bool) {
	policy, ok := operationExposurePolicies[operationID]
	return policy, ok
}

func OperationIDs() []string {
	out := make([]string, 0, len(operationScopes))
	for operationID := range operationScopes {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func MissingExposurePolicies() []string {
	missing := make([]string, 0)
	for operationID := range operationScopes {
		if _, ok := operationExposurePolicies[operationID]; !ok {
			missing = append(missing, operationID)
		}
	}
	sort.Strings(missing)
	return missing
}

func ExposurePolicies() map[string]ExposurePolicy {
	out := make(map[string]ExposurePolicy, len(operationExposurePolicies))
	for operationID, policy := range operationExposurePolicies {
		out[operationID] = policy
	}
	return out
}

func ValidateExposurePolicy(operationID, method string, scopes []string, p ExposurePolicy) error {
	operationID = strings.TrimSpace(operationID)
	method = strings.ToUpper(strings.TrimSpace(method))
	if operationID == "" {
		return fmt.Errorf("operation id is required")
	}
	if p.Class == "" {
		return fmt.Errorf("%s: exposure class is required", operationID)
	}
	if p.AuthKind == "" {
		return fmt.Errorf("%s: exposure auth kind is required", operationID)
	}
	if p.RateLimitClass == "" {
		return fmt.Errorf("%s: exposure rate limit class is required", operationID)
	}
	if p.BrowserTrust == "" {
		return fmt.Errorf("%s: exposure browser trust is required", operationID)
	}
	if p.AuthKind == ExposureAuthBearerScope && len(scopes) == 0 {
		return fmt.Errorf("%s: bearer-scope exposure requires scoped auth", operationID)
	}
	if p.AuthKind != ExposureAuthBearerScope && len(scopes) > 0 {
		return fmt.Errorf("%s: non-bearer exposure must not also require bearer scopes", operationID)
	}
	if p.AuthKind != ExposureAuthBearerScope && !IsUnscopedAllowed(operationID) {
		return fmt.Errorf("%s: non-bearer exposure must be explicitly allowlisted as unscoped", operationID)
	}
	if p.RequiresAbuseControls() {
		if !p.HasDedicatedRateLimit() {
			return fmt.Errorf("%s: sensitive exposure requires dedicated rate limit", operationID)
		}
		if !p.AuditRequired {
			return fmt.Errorf("%s: sensitive exposure requires audit", operationID)
		}
		if !p.RedactErrors {
			return fmt.Errorf("%s: sensitive exposure requires redacted errors", operationID)
		}
	}
	if p.Class == ExposureClassAdmin {
		if !hasScope(scopes, "v3:admin") {
			return fmt.Errorf("%s: admin exposure requires v3:admin", operationID)
		}
		if p.RateLimitClass != ExposureRateLimitAuth {
			return fmt.Errorf("%s: admin exposure requires auth rate-limit class", operationID)
		}
		if !p.AuditRequired {
			return fmt.Errorf("%s: admin exposure requires audit", operationID)
		}
	}
	if p.Class == ExposureClassWrite {
		if !hasScope(scopes, "v3:write") {
			return fmt.Errorf("%s: write exposure requires v3:write", operationID)
		}
		if !p.AuditRequired {
			return fmt.Errorf("%s: write exposure requires audit", operationID)
		}
	}
	if p.Class == ExposureClassPairing && p.RateLimitClass == ExposureRateLimitGlobal {
		return fmt.Errorf("%s: pairing exposure must not use global rate limit", operationID)
	}
	if p.Class == ExposureClassDevice && p.RateLimitClass == ExposureRateLimitGlobal {
		return fmt.Errorf("%s: device exposure must not use global rate limit", operationID)
	}
	if method == "GET" && p.AuthKind == ExposureAuthNone {
		return fmt.Errorf("%s: unauthenticated exposure must not be GET-cacheable", operationID)
	}
	if p.BrowserTrust == ExposureBrowserTrustSameOrigin && p.AuthKind == ExposureAuthNone {
		return fmt.Errorf("%s: browser-reachable unauthenticated exposure is not allowed", operationID)
	}
	return nil
}

func (p ExposurePolicy) RequiresAbuseControls() bool {
	return p.AuthKind != ExposureAuthBearerScope || p.Class == ExposureClassPairing || p.Class == ExposureClassDevice
}

func (p ExposurePolicy) HasDedicatedRateLimit() bool {
	return p.RateLimitClass != ExposureRateLimitNone && p.RateLimitClass != ExposureRateLimitGlobal
}

func (p ExposurePolicy) IsBrowserReachable() bool {
	return strings.TrimSpace(string(p.BrowserTrust)) == string(ExposureBrowserTrustSameOrigin)
}

func (p ExposurePolicy) RequiresNoStore() bool {
	return p.AuditRequired || p.AuthKind != ExposureAuthBearerScope || p.Class == ExposureClassPairing || p.Class == ExposureClassDevice
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if strings.EqualFold(strings.TrimSpace(scope), required) {
			return true
		}
	}
	return false
}
