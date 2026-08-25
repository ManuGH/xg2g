// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func loadTestSpec(t *testing.T, schemas string) *openapi3.T {
	t.Helper()

	doc := `openapi: 3.0.3
info:
  title: test
  version: 1.0.0
paths: {}
components:
  schemas:
` + schemas

	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromData([]byte(doc))
	require.NoError(t, err, "load test spec")
	require.NoError(t, spec.Validate(context.Background()), "validate test spec")
	return spec
}

func TestExtract_PullsReferencedSchemasTransitively(t *testing.T) {
	spec := loadTestSpec(t, `
    Leaf:
      type: string
      enum: [a, b]
    Middle:
      type: object
      required: [leaf]
      properties:
        leaf:
          $ref: "#/components/schemas/Leaf"
    Root:
      type: object
      required: [middles]
      properties:
        middles:
          type: array
          items:
            $ref: "#/components/schemas/Middle"
    Unrelated:
      type: object
      properties:
        ignored:
          type: string
`)

	model, err := extract(spec, []string{"Root"})
	require.NoError(t, err)

	names := make([]string, 0, len(model.objects))
	for _, object := range model.objects {
		names = append(names, object.name)
	}
	require.Equal(t, []string{"Middle", "Root"}, names, "a schema nobody references is not part of the contract")
	require.Len(t, model.enums, 1)
	require.Equal(t, "Leaf", model.enums[0].name)
}

func TestExtract_NamesInlineEnumsTheWayOapiCodegenDoes(t *testing.T) {
	spec := loadTestSpec(t, `
    Key:
      type: object
      required: [crv]
      properties:
        crv:
          type: string
          enum: ["P-256"]
`)

	model, err := extract(spec, []string{"Key"})
	require.NoError(t, err)

	require.Len(t, model.enums, 1)
	require.Equal(t, "KeyCrv", model.enums[0].name, "matches the ECPublicKeyJWKCrv name in server_gen.go")
	require.Equal(t, []string{"P-256"}, model.enums[0].values)
}

func TestExtract_IsDeterministicRegardlessOfRootOrder(t *testing.T) {
	schemas := `
    Alpha:
      type: object
      properties:
        value:
          type: string
    Beta:
      type: object
      properties:
        value:
          type: string
`

	first, err := extract(loadTestSpec(t, schemas), []string{"Alpha", "Beta"})
	require.NoError(t, err)
	second, err := extract(loadTestSpec(t, schemas), []string{"Beta", "Alpha"})
	require.NoError(t, err)

	require.Equal(t, renderSwift(first), renderSwift(second))
	require.Equal(t, renderKotlin(first), renderKotlin(second))
}

// The generator refuses what it cannot translate faithfully. Silently emitting
// an approximation is the failure mode this whole change exists to remove, so
// each unsupported construct is asserted to fail rather than to degrade.
func TestExtract_RejectsUnsupportedConstructs(t *testing.T) {
	tests := []struct {
		name    string
		schemas string
		wantErr string
	}{
		{
			name: "allOf",
			schemas: `
    Base:
      type: object
      properties:
        a:
          type: string
    Root:
      allOf:
        - $ref: "#/components/schemas/Base"
`,
			wantErr: "allOf is not supported",
		},
		{
			name: "oneOf",
			schemas: `
    Root:
      oneOf:
        - type: string
        - type: integer
`,
			wantErr: "oneOf is not supported",
		},
		{
			name: "nullable property",
			schemas: `
    Root:
      type: object
      properties:
        a:
          type: string
          nullable: true
`,
			wantErr: "nullable is not supported",
		},
		{
			name: "free-form object property",
			schemas: `
    Root:
      type: object
      properties:
        fields:
          type: object
          additionalProperties: true
`,
			wantErr: "free-form objects",
		},
		{
			name: "inline object property",
			schemas: `
    Root:
      type: object
      properties:
        nested:
          type: object
          properties:
            a:
              type: string
`,
			wantErr: "inline object",
		},
		{
			name: "required property without definition",
			schemas: `
    Root:
      type: object
      required: [missing]
      properties:
        a:
          type: string
`,
			wantErr: `required property "missing" has no definition`,
		},
		{
			name: "non-string enum",
			schemas: `
    Root:
      type: integer
      enum: [1, 2]
`,
			wantErr: "only string enums are supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := extract(loadTestSpec(t, test.schemas), []string{"Root"})
			require.Error(t, err)
			require.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestExtract_RejectsUndeclaredRoot(t *testing.T) {
	spec := loadTestSpec(t, `
    Known:
      type: object
      properties:
        a:
          type: string
`)

	_, err := extract(spec, []string{"Missing"})
	require.ErrorContains(t, err, `schema "Missing" is not defined`)
}

func TestRenderSwift_MapsTheContractOntoSwiftTypes(t *testing.T) {
	spec := loadTestSpec(t, `
    Mode:
      type: string
      enum: [fast_path, slow_path]
    Root:
      type: object
      required: [id, mode, count, at, flags]
      properties:
        id:
          type: string
        mode:
          $ref: "#/components/schemas/Mode"
        count:
          type: integer
          format: int32
        at:
          type: string
          format: date-time
        flags:
          type: array
          items:
            type: boolean
        note:
          type: string
`)

	model, err := extract(spec, []string{"Root"})
	require.NoError(t, err)
	out := string(renderSwift(model))

	require.Contains(t, out, "enum Xg2gContract {")
	require.Contains(t, out, "case fastPath = \"fast_path\"")
	require.Contains(t, out, "let id: String")
	require.Contains(t, out, "let mode: Mode")
	require.Contains(t, out, "let count: Int")
	require.Contains(t, out, "let at: Date")
	require.Contains(t, out, "let flags: [Bool]")
	require.Contains(t, out, "let note: String?", "an optional property must not become a required one")
}

func TestRenderKotlin_MapsTheContractOntoKotlinTypes(t *testing.T) {
	spec := loadTestSpec(t, `
    Mode:
      type: string
      enum: [fast_path, slow_path]
    Root:
      type: object
      required: [id, mode, count, at]
      properties:
        id:
          type: string
        mode:
          $ref: "#/components/schemas/Mode"
        count:
          type: integer
          format: int32
        at:
          type: string
          format: date-time
        note:
          type: string
`)

	model, err := extract(spec, []string{"Root"})
	require.NoError(t, err)
	out := string(renderKotlin(model))

	require.Contains(t, out, "FAST_PATH(\"fast_path\")")
	require.Contains(t, out, "val id: String,")
	require.Contains(t, out, "val mode: Mode,")
	require.Contains(t, out, "val count: Int,")
	require.Contains(t, out, "val at: Instant,")
	require.Contains(t, out, "val note: String?")

	// A required field must go through requireField, which throws on absence.
	// The org.json defaults it replaces (optString -> "") are precisely what let
	// the hand-written client decode a response that was no longer being sent.
	require.Contains(t, out, `id = requireString(json.requireField("id", owner), owner, "id")`)
	require.Contains(t, out, `note = json.optionalField("note")?.let {`)
	require.NotContains(t, out, "optString", "generated decoders never fall back to a lenient default")
}

func TestDecodeRoots(t *testing.T) {
	roots, err := decodeRoots(map[string]any{"roots": []any{"Beta", "Alpha"}})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha", "Beta"}, roots, "roots are sorted so output never depends on list order")

	_, err = decodeRoots(nil)
	require.ErrorContains(t, err, "is required")

	_, err = decodeRoots(map[string]any{"roots": []any{}})
	require.ErrorContains(t, err, "at least one root schema")

	_, err = decodeRoots(map[string]any{"roots": []any{"Alpha", "Alpha"}})
	require.ErrorContains(t, err, "duplicate root schema")
}

func TestSplitWireWords(t *testing.T) {
	require.Equal(t, []string{"local", "https"}, splitWireWords("local_https"))
	require.Equal(t, []string{"P", "256"}, splitWireWords("P-256"))
	require.Equal(t, []string{"EC"}, splitWireWords("EC"))
	require.Equal(t, []string{"android", "Tv"}, splitWireWords("androidTv"))
	require.Empty(t, splitWireWords("-"))
}

func TestSwiftAndKotlinIdentifiersAvoidKeywords(t *testing.T) {
	require.Equal(t, "`operator`", swiftIdentifier("operator"))
	require.Equal(t, "scope", swiftIdentifier("scope"))
	require.Equal(t, "`object`", kotlinIdentifier("object"))
	require.Equal(t, "scope", kotlinIdentifier("scope"))
}

func TestGeneratedHeadersMarkTheFilesAsGenerated(t *testing.T) {
	spec := loadTestSpec(t, `
    Root:
      type: object
      properties:
        a:
          type: string
`)
	model, err := extract(spec, []string{"Root"})
	require.NoError(t, err)

	// verify-generated-artifacts-contract.sh classifies files by this marker.
	const marker = "Code generated by gen-native-contract from backend/api/openapi.yaml; DO NOT EDIT."
	require.True(t, strings.HasPrefix(string(renderSwift(model)), "// "+marker))
	require.True(t, strings.HasPrefix(string(renderKotlin(model)), "// "+marker))
}
