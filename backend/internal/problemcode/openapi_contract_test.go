// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package problemcode

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type openAPISchema struct {
	Ref        string                   `yaml:"$ref"`
	Enum       []string                 `yaml:"enum"`
	Properties map[string]openAPISchema `yaml:"properties"`
}

type openAPIDoc struct {
	Components struct {
		Schemas map[string]openAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

func TestProblemCodeRegistry_OpenAPIProblemCodeEnum(t *testing.T) {
	doc := loadOpenAPIProblemDoc(t)
	problemCodeSchema, ok := doc.Components.Schemas["ProblemCode"]
	require.True(t, ok, "OpenAPI schema ProblemCode missing")

	expected := make([]string, 0, len(PublicEntries()))
	for _, entry := range PublicEntries() {
		expected = append(expected, entry.Code)
	}

	actual := append([]string(nil), problemCodeSchema.Enum...)
	slices.Sort(expected)
	slices.Sort(actual)

	require.Equal(t, expected, actual)
}

func TestProblemCodeRegistry_OpenAPICodeFieldsReferenceCatalog(t *testing.T) {
	doc := loadOpenAPIProblemDoc(t)

	require.Equal(t, "#/components/schemas/ProblemCode", doc.Components.Schemas["ProblemDetails"].Properties["code"].Ref)
	require.Equal(t, "#/components/schemas/ProblemCode", doc.Components.Schemas["SessionTerminalProblem"].Properties["code"].Ref)
}

func TestProblemCodeRegistry_OpenAPIErrorSeverityEnum(t *testing.T) {
	doc := loadOpenAPIProblemDoc(t)
	severitySchema, ok := doc.Components.Schemas["ErrorSeverity"]
	require.True(t, ok, "OpenAPI schema ErrorSeverity missing")

	expected := []string{
		string(SeverityInfo),
		string(SeverityWarning),
		string(SeverityError),
		string(SeverityCritical),
	}
	actual := append([]string(nil), severitySchema.Enum...)
	slices.Sort(expected)
	slices.Sort(actual)

	require.Equal(t, expected, actual)
}

func loadOpenAPIProblemDoc(t *testing.T) openAPIDoc {
	t.Helper()

	path := locateOpenAPIProblemDoc(t)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var doc openAPIDoc
	require.NoError(t, yaml.Unmarshal(data, &doc))
	return doc
}

func locateOpenAPIProblemDoc(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		candidate := filepath.Join(dir, "api", "openapi.yaml")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	require.FailNowf(t, "openapi.yaml not found", "searched upward from %s", dir)
	return ""
}
