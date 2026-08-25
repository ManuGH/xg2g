// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Command gen-contract-manifest emits the index over the whole generated wire
// surface: every path, every operation, every component schema, and every
// artifact that any generator derives from backend/api/openapi.yaml, each with
// a content hash.
//
// Two things depend on it.
//
// Freshness: the manifest records the spec hash next to the hash of every
// artifact generated from it. A generated file edited by hand, or a spec change
// that never reached a generator, shows up as a manifest diff, so the drift
// lock does not depend on any single generator remembering to be rerun.
//
// Zone checking: the guards that keep wire schemas inside the generated zones
// need to know which type names *are* wire schemas. Matching on file names
// guesses; reading `schemas` out of this manifest does not.
//
// It runs last in the generation pipeline, after every other generator has
// written its output, because it hashes what they produced.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	manifestVersion   = 1
	contractExtension = "x-xg2g-native-contract"
	doNotEditMarker   = "DO NOT EDIT"
)

// generatedArtifact is one file some generator derives from the spec.
//
// requireMarker is false only for outputs whose generator does not emit a
// marker and whose format has no comment syntax to carry one; every such
// exemption is spelled out at the declaration rather than inferred from the
// extension.
type generatedArtifact struct {
	path          string
	language      string
	generator     string
	requireMarker bool
}

// generatedArtifacts is the closed list of files generated from the OpenAPI
// document. Adding a generator without adding its output here is what the
// manifest exists to make impossible: the index would claim completeness it
// does not have.
var generatedArtifacts = []generatedArtifact{
	{"backend/internal/api/server_gen.go", "go", "oapi-codegen", true},
	{"backend/internal/control/http/v3/server_gen.go", "go", "oapi-codegen", true},
	{"backend/internal/control/http/v3/operation_routes_gen.go", "go", "gen-operation-catalog", true},
	{"backend/internal/control/authz/operation_catalog_gen.go", "go", "gen-operation-catalog", true},
	{"openapi/v3.normative.snapshot.yaml", "yaml", "generate-normative-snapshot", true},
	{"apps/webui/src/client-ts/types.gen.ts", "typescript", "@hey-api/openapi-ts", true},
	{"apps/webui/src/client-ts/sdk.gen.ts", "typescript", "@hey-api/openapi-ts", true},
	{"apps/webui/src/client-ts/client.gen.ts", "typescript", "@hey-api/openapi-ts", true},
	{"apps/webui/src/client-ts/index.ts", "typescript", "@hey-api/openapi-ts", true},
	{"apps/webui/src/types/api/consumption.d.ts", "typescript", "generate-consumption-types", true},
	{"ios/Xg2g/Generated/Xg2gContract.swift", "swift", "gen-native-contract", true},
	{"android/app/src/main/java/io/github/manugh/xg2g/android/contract/Xg2gContract.kt", "kotlin", "gen-native-contract", true},
}

type manifest struct {
	Comment    string              `json:"$comment"`
	Version    int                 `json:"manifestVersion"`
	Spec       specEntry           `json:"spec"`
	Paths      []string            `json:"paths"`
	Operations []operationEntry    `json:"operations"`
	Schemas    []string            `json:"schemas"`
	Native     nativeContractEntry `json:"nativeContract"`
	Artifacts  []artifactEntry     `json:"artifacts"`
}

type specEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type operationEntry struct {
	OperationID string          `json:"operationId"`
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Parameters  []parameterInfo `json:"parameters"`
	RequestBody []string        `json:"requestBodyContentTypes,omitempty"`
	Responses   []responseInfo  `json:"responses"`
}

type parameterInfo struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
}

type responseInfo struct {
	Status       string   `json:"status"`
	ContentTypes []string `json:"contentTypes,omitempty"`
}

type nativeContractEntry struct {
	Roots   []string `json:"roots"`
	Closure []string `json:"closure"`
}

type artifactEntry struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	Generator string `json:"generator"`
	SHA256    string `json:"sha256"`
}

func main() {
	repoRoot := flag.String("repo-root", "..", "repository root")
	specPath := flag.String("spec", "api/openapi.yaml", "canonical OpenAPI document")
	out := flag.String("out", "api/contract.manifest.json", "generated manifest")
	flag.Parse()

	if err := run(*repoRoot, *specPath, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-contract-manifest:", err)
		os.Exit(1)
	}
}

func run(repoRoot, specPath, out string) error {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return fmt.Errorf("load OpenAPI: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return fmt.Errorf("validate OpenAPI: %w", err)
	}

	specHash, err := hashFile(specPath)
	if err != nil {
		return err
	}

	m := manifest{
		Comment: "GENERATED FILE - DO NOT EDIT. Source: backend/api/openapi.yaml. " +
			"Regenerate with: make generate-contract-manifest",
		Version: manifestVersion,
		Spec:    specEntry{Path: "backend/api/openapi.yaml", SHA256: specHash},
	}

	m.Paths, m.Operations = indexOperations(doc)
	m.Schemas = indexSchemas(doc)

	native, err := indexNativeContract(doc)
	if err != nil {
		return err
	}
	m.Native = native

	artifacts, err := indexArtifacts(repoRoot)
	if err != nil {
		return err
	}
	m.Artifacts = artifacts

	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	encoded = append(encoded, '\n')

	if err := os.WriteFile(out, encoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	fmt.Printf("gen-contract-manifest: %d paths, %d operations, %d schemas, %d artifacts\n",
		len(m.Paths), len(m.Operations), len(m.Schemas), len(m.Artifacts))
	fmt.Printf("  - %s\n", out)
	return nil
}

func indexOperations(doc *openapi3.T) ([]string, []operationEntry) {
	paths := make([]string, 0, doc.Paths.Len())
	entries := make([]operationEntry, 0, doc.Paths.Len()*2)

	for path, item := range doc.Paths.Map() {
		paths = append(paths, path)
		for method, op := range item.Operations() {
			entry := operationEntry{
				OperationID: op.OperationID,
				Method:      method,
				Path:        path,
				Parameters:  []parameterInfo{},
				Responses:   []responseInfo{},
			}

			// Path-level parameters apply to every operation on the item, so
			// they belong in each operation's inventory too.
			for _, ref := range append(append(openapi3.Parameters{}, item.Parameters...), op.Parameters...) {
				if ref == nil || ref.Value == nil {
					continue
				}
				entry.Parameters = append(entry.Parameters, parameterInfo{
					Name:     ref.Value.Name,
					In:       ref.Value.In,
					Required: ref.Value.Required,
				})
			}
			sort.Slice(entry.Parameters, func(i, j int) bool {
				if entry.Parameters[i].In != entry.Parameters[j].In {
					return entry.Parameters[i].In < entry.Parameters[j].In
				}
				return entry.Parameters[i].Name < entry.Parameters[j].Name
			})

			if op.RequestBody != nil && op.RequestBody.Value != nil {
				entry.RequestBody = contentTypes(op.RequestBody.Value.Content)
			}

			if op.Responses != nil {
				for status, ref := range op.Responses.Map() {
					info := responseInfo{Status: status}
					if ref != nil && ref.Value != nil {
						info.ContentTypes = contentTypes(ref.Value.Content)
					}
					entry.Responses = append(entry.Responses, info)
				}
				sort.Slice(entry.Responses, func(i, j int) bool {
					return entry.Responses[i].Status < entry.Responses[j].Status
				})
			}

			entries = append(entries, entry)
		}
	}

	sort.Strings(paths)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Method < entries[j].Method
	})
	return paths, entries
}

func contentTypes(content openapi3.Content) []string {
	if len(content) == 0 {
		return nil
	}
	out := make([]string, 0, len(content))
	for mediaType := range content {
		out = append(out, mediaType)
	}
	sort.Strings(out)
	return out
}

func indexSchemas(doc *openapi3.T) []string {
	if doc.Components == nil {
		return []string{}
	}
	out := make([]string, 0, len(doc.Components.Schemas))
	for name := range doc.Components.Schemas {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// indexNativeContract records both the declared roots and the transitive
// closure they pull in. The closure is what the zone guards check against:
// a hand-written Swift or Kotlin type named after any of these is a duplicate
// of a generated one, whether or not it was named as a root.
func indexNativeContract(doc *openapi3.T) (nativeContractEntry, error) {
	entry := nativeContractEntry{Roots: []string{}, Closure: []string{}}

	raw, ok := doc.Extensions[contractExtension]
	if !ok || raw == nil {
		return entry, fmt.Errorf("OpenAPI root extension %s is required", contractExtension)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return entry, fmt.Errorf("encode %s: %w", contractExtension, err)
	}
	var decl struct {
		Roots []string `json:"roots"`
	}
	if err := json.Unmarshal(data, &decl); err != nil {
		return entry, fmt.Errorf("decode %s: %w", contractExtension, err)
	}

	entry.Roots = append(entry.Roots, decl.Roots...)
	sort.Strings(entry.Roots)

	if doc.Components == nil {
		return entry, fmt.Errorf("OpenAPI document declares no component schemas")
	}

	seen := map[string]bool{}
	var walk func(name string) error
	walk = func(name string) error {
		if seen[name] {
			return nil
		}
		ref, ok := doc.Components.Schemas[name]
		if !ok || ref.Value == nil {
			return fmt.Errorf("%s: schema %q is not defined in components.schemas", contractExtension, name)
		}
		seen[name] = true
		for _, property := range ref.Value.Properties {
			for _, referenced := range referencedComponents(property) {
				if err := walk(referenced); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, root := range entry.Roots {
		if err := walk(root); err != nil {
			return entry, err
		}
	}

	for name := range seen {
		entry.Closure = append(entry.Closure, name)
	}
	sort.Strings(entry.Closure)
	return entry, nil
}

func referencedComponents(ref *openapi3.SchemaRef) []string {
	if ref == nil {
		return nil
	}
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(ref.Ref, prefix) {
		return []string{strings.TrimPrefix(ref.Ref, prefix)}
	}
	if ref.Value == nil {
		return nil
	}
	var out []string
	out = append(out, referencedComponents(ref.Value.Items)...)
	for _, property := range ref.Value.Properties {
		out = append(out, referencedComponents(property)...)
	}
	return out
}

func indexArtifacts(repoRoot string) ([]artifactEntry, error) {
	out := make([]artifactEntry, 0, len(generatedArtifacts))
	for _, artifact := range generatedArtifacts {
		abs := filepath.Join(repoRoot, artifact.path)
		content, err := os.ReadFile(abs) // #nosec G304 -- closed list of repository-relative generated paths
		if err != nil {
			return nil, fmt.Errorf("generated artifact %s: %w", artifact.path, err)
		}
		if artifact.requireMarker && !hasMarker(content) {
			return nil, fmt.Errorf(
				"generated artifact %s does not carry a %q marker; every generated file must announce itself",
				artifact.path, doNotEditMarker)
		}
		sum := sha256.Sum256(content)
		out = append(out, artifactEntry{
			Path:      artifact.path,
			Language:  artifact.language,
			Generator: artifact.generator,
			SHA256:    hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// hasMarker looks only at the head of the file: a marker further down is a
// mention, not a declaration.
func hasMarker(content []byte) bool {
	head := content
	if len(head) > 4096 {
		head = head[:4096]
	}
	upper := strings.ToUpper(string(head))
	return strings.Contains(upper, doNotEditMarker) ||
		strings.Contains(upper, "AUTO-GENERATED") ||
		strings.Contains(upper, "GENERATED BY")
}

func hashFile(path string) (string, error) {
	content, err := os.ReadFile(path) // #nosec G304 -- generator input path from the build system
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}
