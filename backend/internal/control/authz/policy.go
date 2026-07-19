// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

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
