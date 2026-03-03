//go:build ignore

// Copyright (c) 2025 ManuGH

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// This script enforces that only approved fields are marked as HotReloadable in registry.go.
// It prevents unauthorized promotion of configuration fields to runtime-tunable status.

const targetFile = "internal/config/registry.go"

var approvedFields = map[string]bool{
	"LogLevel": true,
}

func main() {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, targetFile, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse %s: %v\n", targetFile, err)
		os.Exit(1)
	}

	violations := 0

	ast.Inspect(node, func(n ast.Node) bool {
		// Look for composite literals of type ConfigEntry
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}

		// We check if it looks like a ConfigEntry based on its fields
		// (The parser doesn't resolve types without more context, but we can look for "HotReloadable")

		isConfigEntry := false
		fieldPath := ""
		hotReloadable := false

		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}

			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}

			switch key.Name {
			case "FieldPath":
				if lit, ok := kv.Value.(*ast.BasicLit); ok {
					fieldPath = strings.Trim(lit.Value, "\"")
					isConfigEntry = true
				}
			case "HotReloadable":
				if ident, ok := kv.Value.(*ast.Ident); ok {
					if ident.Name == "true" {
						hotReloadable = true
					}
				}
			}
		}

		if isConfigEntry && hotReloadable {
			if !approvedFields[fieldPath] {
				fmt.Printf("VIOLATION: Field %q is marked HotReloadable but is NOT in the approved allowlist\n", fieldPath)
				violations++
			}
		}

		return true
	})

	if violations > 0 {
		fmt.Printf("\nFAILED: %d hot-reload governance violations found in %s\n", violations, targetFile)
		fmt.Println("New hot-reloadable fields require security review and must be added to approvedFields in scripts/verify-hot-reload-governance.go")
		os.Exit(1)
	}

	fmt.Println("PASS: Hot-reload governance check successful")
}
