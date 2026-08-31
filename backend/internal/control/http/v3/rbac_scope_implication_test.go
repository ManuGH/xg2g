// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "testing"

// TestScopeImplications pins the direction of the admin/status relation.
// An administrator sees the system's own status; a monitoring token that sees
// the status stays blind to everything else.
func TestScopeImplications(t *testing.T) {
	tests := []struct {
		name    string
		granted []string
		allowed []Scope
		denied  []Scope
	}{
		{
			name:    "v3:admin implies status, write and read",
			granted: []string{string(ScopeV3Admin)},
			allowed: []Scope{ScopeV3Admin, ScopeV3Status, ScopeV3Write, ScopeV3Read},
		},
		{
			name:    "v3:* implies status",
			granted: []string{string(ScopeV3All)},
			allowed: []Scope{ScopeV3Status, ScopeV3Admin},
		},
		{
			name:    "* implies status",
			granted: []string{string(ScopeAll)},
			allowed: []Scope{ScopeV3Status, ScopeV3Admin},
		},
		{
			name:    "legacy admin alias implies status",
			granted: []string{"admin"},
			allowed: []Scope{ScopeV3Status, ScopeV3Admin},
		},
		{
			name:    "v3:status alone grants nothing else",
			granted: []string{string(ScopeV3Status)},
			allowed: []Scope{ScopeV3Status},
			denied:  []Scope{ScopeV3Read, ScopeV3Write, ScopeV3Admin},
		},
		{
			name:    "v3:write does not reach status",
			granted: []string{string(ScopeV3Write)},
			allowed: []Scope{ScopeV3Write, ScopeV3Read},
			denied:  []Scope{ScopeV3Status, ScopeV3Admin},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := newScopeSet(tc.granted)
			for _, scope := range tc.allowed {
				if !set.has(scope) {
					t.Errorf("scopes %v: has(%q) = false, want true", tc.granted, scope)
				}
			}
			for _, scope := range tc.denied {
				if set.has(scope) {
					t.Errorf("scopes %v: has(%q) = true, want false", tc.granted, scope)
				}
			}
		})
	}
}

// TestAdminScopeSetContainsStatus guards the logged set, not just the answer.
// has() short-circuits the wildcards, so a set that disagrees with it would
// still authorize correctly while reporting the wrong token_scopes on a 403.
func TestAdminScopeSetContainsStatus(t *testing.T) {
	for _, granted := range []string{string(ScopeAll), string(ScopeV3All), string(ScopeV3Admin), "admin"} {
		set := newScopeSet([]string{granted})
		if _, ok := set[ScopeV3Status]; !ok {
			t.Errorf("scope %q: set is missing %q, so a 403 would log an incomplete token_scopes", granted, ScopeV3Status)
		}
	}
}
