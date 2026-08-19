//go:build ignore

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

// verify-no-adhoc-wire-structs fails when a hand-written struct in the v3
// package duplicates a type that oapi-codegen already generates from
// api/openapi.yaml.
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
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
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

	generated, handWritten, err := collect(dir)
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

	fmt.Println("✅ no hand-written duplicates of generated contract types")
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
