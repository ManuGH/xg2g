// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package authz

import (
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
)

func TestPolicyCoverage(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	specOps := map[string]struct{}{}
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			if op.OperationID == "" {
				t.Fatalf("operation id missing in spec")
			}
			normalized := codegen.ToCamelCase(op.OperationID)
			specOps[normalized] = struct{}{}
		}
	}

	for opID := range specOps {
		if _, ok := operationScopes[opID]; !ok {
			t.Errorf("missing policy for operation %s", opID)
		}
	}
	for opID := range operationScopes {
		if _, ok := specOps[opID]; !ok {
			t.Errorf("policy defined for unknown operation %s", opID)
		}
	}
}

func TestPolicyUnscopedAllowlist(t *testing.T) {
	for opID, scopes := range operationScopes {
		if len(scopes) == 0 && !IsUnscopedAllowed(opID) {
			t.Errorf("operation %s has empty scopes but is not allowlisted", opID)
		}
	}
	for opID := range unscopedOperations {
		scopes, ok := operationScopes[opID]
		if !ok {
			t.Errorf("allowlisted operation %s missing policy entry", opID)
			continue
		}
		if len(scopes) != 0 {
			t.Errorf("allowlisted operation %s must have empty scopes", opID)
		}
	}
}

func loadOpenAPIDoc(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	path := filepath.Join("..", "..", "..", "api", "openapi.yaml")
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("openapi load failed: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("openapi validate failed: %v", err)
	}
	return doc
}
