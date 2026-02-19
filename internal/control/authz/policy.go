// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

// Policy registry for OpenAPI operation IDs.
// This is the single source of truth for required scopes.
var operationScopes = map[string][]string{
	"CreateSession":                    {"v3:read"},
	"GetDvrCapabilities":               {"v3:read"},
	"GetDvrStatus":                     {"v3:read"},
	"GetEpg":                           {"v3:read"},
	"CreateIntent":                     {"v3:write"},
	"GetLogs":                          {"v3:admin"},
	"GetReceiverCurrent":               {"v3:read"},
	"GetRecordings":                    {"v3:read"},
	"DeleteRecording":                  {"v3:write"},
	"GetRecordingHLSPlaylist":          {"v3:read"},
	"GetRecordingHLSPlaylistHead":      {"v3:read"},
	"GetRecordingsRecordingIdStatus":   {"v3:read"},
	"GetRecordingPlaybackInfo":         {"v3:read"},
	"PostRecordingPlaybackInfo":        {"v3:read"},
	"StreamRecordingDirect":            {"v3:read"},
	"ProbeRecordingMp4":                {"v3:read"},
	"GetRecordingHLSTimeshift":         {"v3:read"},
	"GetRecordingHLSTimeshiftHead":     {"v3:read"},
	"GetRecordingHLSCustomSegment":     {"v3:read"},
	"GetRecordingHLSCustomSegmentHead": {"v3:read"},
	"GetSeriesRules":                   {"v3:read"},
	"CreateSeriesRule":                 {"v3:write"},
	"RunAllSeriesRules":                {"v3:write"},
	"DeleteSeriesRule":                 {"v3:write"},
	"UpdateSeriesRule":                 {"v3:write"},
	"RunSeriesRule":                    {"v3:write"},
	"GetServices":                      {"v3:read"},
	"GetServicesBouquets":              {"v3:read"},
	"PostServicesNowNext":              {"v3:read"},
	"PostServicesIdToggle":             {"v3:write"},
	"ListSessions":                     {"v3:admin"},
	"GetSessionState":                  {"v3:read"},
	"ServeHLS":                         {"v3:read"},
	"ServeHLSHead":                     {"v3:read"},
	"ReportPlaybackFeedback":           {"v3:read"},
	"GetStreams":                       {"v3:admin"},
	"DeleteStreamsId":                  {"v3:write"},
	"GetSystemConfig":                  {"v3:admin"},
	"PutSystemConfig":                  {"v3:admin"},
	"GetSystemHealth":                  {"v3:read"},
	"GetSystemHealthz":                 {"v3:read"},
	"GetSystemInfo":                    {"v3:read"},
	"PostSystemRefresh":                {"v3:write"},
	"GetSystemScanStatus":              {},
	"TriggerSystemScan":                {},
	"GetTimers":                        {"v3:read"},
	"AddTimer":                         {"v3:write"},
	"PreviewConflicts":                 {"v3:read"},
	"DeleteTimer":                      {"v3:write"},
	"GetTimer":                         {"v3:read"},
	"UpdateTimer":                      {"v3:write"},
}

// Operations allowed to be unscoped (e.g., health/scan endpoints).
var unscopedOperations = map[string]struct{}{
	"GetSystemScanStatus": {},
	"TriggerSystemScan":   {},
}

// RequiredScopes returns the required scopes for an operation ID.
func RequiredScopes(operationID string) ([]string, bool) {
	scopes, ok := operationScopes[operationID]
	if !ok {
		return nil, false
	}
	return cloneScopes(scopes), true
}

// MustScopes returns required scopes for an operation.
// Unknown operations resolve to an empty scope list.
// Deprecated: prefer RequiredScopes + explicit error handling.
func MustScopes(operationID string) []string {
	scopes, ok := RequiredScopes(operationID)
	if !ok {
		return []string{}
	}
	return scopes
}

// IsUnscopedAllowed reports whether an operation is allowed to have empty scopes.
func IsUnscopedAllowed(operationID string) bool {
	_, ok := unscopedOperations[operationID]
	return ok
}

func cloneScopes(scopes []string) []string {
	if scopes == nil {
		return []string{}
	}
	return append([]string{}, scopes...)
}
