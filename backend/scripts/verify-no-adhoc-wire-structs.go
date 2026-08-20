//go:build ignore

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

// verify-no-adhoc-wire-structs holds hand-written structs to the contract that
// oapi-codegen generates from api/openapi.yaml.
//
// Two rules, because the v3 package and its sub-packages are in different
// positions:
//
//  1. In package v3 the generated type is right there, so a hand-written
//     struct with the same JSON field set and a related name is a duplicate
//     and has to go.
//
//  2. In a sub-package it is not: v3 imports them, so importing v3 back would
//     cycle. A mirror is allowed there, but it must not contradict the schema
//     it mirrors. Two things contradict it: missing a field the schema marks
//     required, and carrying a field a closed schema forbids. Both are read
//     from api/openapi.yaml rather than guessed.
//
//     A mirror that simply never populates an optional field is not a
//     violation. Failing on that would be failing on a design choice, and a
//     gate that does so gets an exception list instead of a fix.
//
// This is the check that would have caught the pairing incident. The handler
// wrote its own exchangePairingResponse while the generated
// ExchangePairingResponse sat unused in the same package, so the published
// contract and the served bytes were free to disagree — and did, for as long as
// it took a client to break.
//
// A duplicate is reported only when the JSON field set matches *and* the names
// are related. The field set alone is not proof: ContinueWatchingResponse and
// ErrorCatalogResponse both carry exactly {items} and have nothing to do with
// each other. Requiring both keeps the gate from accusing unrelated types,
// which matters more than catching every last duplicate — a check that cries
// wolf gets an exception added instead of a fix.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const generatedSuffix = "_gen.go"

// allowed lists hand-written types that knowingly mirror a generated one, with
// the reason and the condition for removing the entry. An entry here is a debt
// record, not an opinion that the duplication is fine.
var allowed = map[string]bool{
	// SystemInfoData declares its seven sections as inline anonymous objects in
	// api/openapi.yaml, so the generated Go type nests anonymous structs that
	// are painful to construct and impossible to name. Promote those sections
	// to named component schemas, then delete this entry and use the generated
	// type. Until then SystemInfo is the readable shape of the same contract.
	"SystemInfo": true,
}

type structDecl struct {
	name string
	file string
	line int
	tags []string
}

func main() {
	dir := "./internal/control/http/v3"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	specPath := "./api/openapi.yaml"
	if len(os.Args) > 2 {
		specPath = os.Args[2]
	}

	generated, handWritten, err := collect(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analysis failed: %v\n", err)
		os.Exit(1)
	}

	mirrors, err := collectSubPackages(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analysis failed: %v\n", err)
		os.Exit(1)
	}

	index := map[string][]string{}
	for _, decl := range generated {
		if len(decl.tags) == 0 {
			continue
		}
		key := strings.Join(decl.tags, ",")
		index[key] = append(index[key], decl.name)
	}

	var violations []string
	for _, decl := range handWritten {
		if len(decl.tags) == 0 || allowed[decl.name] {
			continue
		}
		for _, twin := range index[strings.Join(decl.tags, ",")] {
			if !namesRelated(decl.name, twin) {
				continue
			}
			violations = append(violations, fmt.Sprintf(
				"  %s:%d: %s duplicates the generated %s (same JSON fields, related name)",
				decl.file, decl.line, decl.name, twin))
			break
		}
	}
	sort.Strings(violations)

	if len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "❌ hand-written structs duplicate generated contract types:")
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Use the generated type. A hand-written copy can drift from api/openapi.yaml;")
		fmt.Fprintln(os.Stderr, "the generated one cannot, because regeneration turns a contract change into a")
		fmt.Fprintln(os.Stderr, "compile error rather than a client bug.")
		os.Exit(1)
	}

	schemas, err := loadSchemaFacts(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analysis failed: %v\n", err)
		os.Exit(1)
	}

	var drifted []string
	checked := 0
	for _, decl := range mirrors {
		facts, mirrored := schemas[decl.name]
		if !mirrored {
			continue
		}
		checked++

		if absent := missing(facts.required, decl.tags); len(absent) > 0 {
			drifted = append(drifted, fmt.Sprintf(
				"  %s:%d: %s omits fields %s declares required: %v",
				decl.file, decl.line, decl.name, decl.name, absent))
		}
		if facts.closed {
			if extra := missing(decl.tags, facts.properties); len(extra) > 0 {
				drifted = append(drifted, fmt.Sprintf(
					"  %s:%d: %s carries fields %s forbids (additionalProperties: false): %v",
					decl.file, decl.line, decl.name, decl.name, extra))
			}
		}
	}
	sort.Strings(drifted)

	if len(drifted) > 0 {
		fmt.Fprintln(os.Stderr, "❌ contract mirrors contradict api/openapi.yaml:")
		for _, violation := range drifted {
			fmt.Fprintln(os.Stderr, violation)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "These sub-packages cannot import the generated types — v3 imports them, so the")
		fmt.Fprintln(os.Stderr, "reverse would cycle — and therefore carry a copy. Either fix the copy or, if")
		fmt.Fprintln(os.Stderr, "the server really does accept or send the field, declare it in the schema.")
		os.Exit(1)
	}

	fmt.Printf("✅ no hand-written duplicates; %d contract mirrors agree with the spec\n", checked)
}

// namesRelated reports whether two type names describe the same contract:
// equal once case and a trailing Request/Response/Data wrapper word are removed.
func namesRelated(a, b string) bool {
	return normalizeTypeName(a) == normalizeTypeName(b)
}

func normalizeTypeName(name string) string {
	lowered := strings.ToLower(name)
	for _, suffix := range []string{"response", "request", "data"} {
		lowered = strings.TrimSuffix(lowered, suffix)
	}
	return lowered
}

func collect(dir string) (generated, handWritten []structDecl, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(dir, name)
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}

		decls := structsIn(fset, file, path)
		if strings.HasSuffix(name, generatedSuffix) {
			generated = append(generated, decls...)
			continue
		}
		handWritten = append(handWritten, decls...)
	}
	return generated, handWritten, nil
}

// collectSubPackages gathers structs below the v3 package directory, where the
// generated types are out of reach and a mirror is the only option.
func collectSubPackages(root string) ([]structDecl, error) {
	var out []structDecl
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if filepath.Dir(path) == root {
			return nil // rule 1 territory
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, generatedSuffix) {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		out = append(out, structsIn(fset, file, path)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// schemaFacts is what the contract says about one schema, as opposed to what
// the generated Go happens to look like: Go cannot express "required" or
// "additionalProperties: false", so both are read from the document itself.
type schemaFacts struct {
	properties []string
	required   []string
	closed     bool
}

func loadSchemaFacts(path string) (map[string]schemaFacts, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI: %w", err)
	}
	if doc.Components == nil {
		return nil, fmt.Errorf("OpenAPI document declares no components")
	}

	out := make(map[string]schemaFacts, len(doc.Components.Schemas))
	for name, ref := range doc.Components.Schemas {
		if ref == nil || ref.Value == nil {
			continue
		}
		schema := ref.Value

		properties := make([]string, 0, len(schema.Properties))
		for property := range schema.Properties {
			properties = append(properties, property)
		}
		sort.Strings(properties)

		required := append([]string(nil), schema.Required...)
		sort.Strings(required)

		out[name] = schemaFacts{
			properties: properties,
			required:   required,
			closed:     schema.AdditionalProperties.Has != nil && !*schema.AdditionalProperties.Has,
		}
	}
	return out, nil
}

// missing returns the entries of a that b does not have. Both are sorted.
func missing(a, b []string) []string {
	have := map[string]bool{}
	for _, v := range b {
		have[v] = true
	}
	var out []string
	for _, v := range a {
		if !have[v] {
			out = append(out, v)
		}
	}
	return out
}

func structsIn(fset *token.FileSet, file *ast.File, path string) []structDecl {
	var out []structDecl

	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok || structType.Fields == nil {
			return true
		}

		var tags []string
		for _, field := range structType.Fields.List {
			if field.Tag == nil {
				continue
			}
			raw, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				continue
			}
			name := strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			tags = append(tags, name)
		}
		if len(tags) == 0 {
			return true
		}
		sort.Strings(tags)

		out = append(out, structDecl{
			name: spec.Name.Name,
			file: path,
			line: fset.Position(spec.Pos()).Line,
			tags: tags,
		})
		return true
	})

	return out
}
