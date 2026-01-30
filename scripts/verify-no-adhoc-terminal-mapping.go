// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"go/types"

	"golang.org/x/tools/go/packages"
)

var (
	guardedFields = map[string]struct{}{
		"State":             {},
		"Reason":            {},
		"ReasonDetailCode":  {},
		"ReasonDetailDebug": {},
	}
)

func main() {
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedName,
		Dir:  ".",
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load packages: %v\n", err)
		os.Exit(1)
	}

	var violations []string
	for _, pkg := range pkgs {
		for i, file := range pkg.Syntax {
			filename := ""
			if i < len(pkg.CompiledGoFiles) {
				filename = pkg.CompiledGoFiles[i]
			} else if i < len(pkg.GoFiles) {
				filename = pkg.GoFiles[i]
			}
			if filename == "" {
				continue
			}
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			if strings.Contains(filename, filepath.Join("internal", "domain", "session", "lifecycle")+string(os.PathSeparator)) {
				continue
			}
			if strings.Contains(filename, filepath.Join("internal", "domain", "session", "store")+string(os.PathSeparator)) {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.AssignStmt:
					for idx, lhs := range node.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok {
							continue
						}
						if !isSessionRecordField(sel, pkg.TypesInfo) {
							continue
						}
						if idx >= len(node.Rhs) {
							continue
						}
						field := sel.Sel.Name
						if _, ok := guardedFields[field]; ok {
							violations = append(violations, formatViolation(filename, sel.Pos(), "direct SessionRecord field write (use lifecycle.Dispatch/ApplyTransition)"))
						}
					}
				case *ast.CompositeLit:
					if !isSessionRecordType(node.Type, pkg.TypesInfo) {
						return true
					}
					for _, elt := range node.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if _, ok := guardedFields[key.Name]; ok {
							violations = append(violations, formatViolation(filename, kv.Pos(), "direct SessionRecord field literal (use lifecycle.Dispatch/ApplyTransition)"))
						}
					}
				}
				return true
			})
		}
	}

	if len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "❌ ad-hoc terminal mappings found (use lifecycle.TerminalOutcome/ApplyOutcome):")
		for _, v := range violations {
			fmt.Fprintln(os.Stderr, v)
		}
		os.Exit(1)
	}
}

func isSessionRecordField(sel *ast.SelectorExpr, info *types.Info) bool {
	return isSessionRecordType(sel.X, info)
}

func isSessionRecordType(expr ast.Expr, info *types.Info) bool {
	typ := info.TypeOf(expr)
	if typ == nil {
		return false
	}
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}
	if named.Obj().Name() != "SessionRecord" {
		return false
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	return strings.HasSuffix(named.Obj().Pkg().Path(), "/internal/domain/session/model")
}

// No per-value filters; any direct write outside lifecycle is forbidden.

func formatViolation(filename string, pos token.Pos, msg string) string {
	return fmt.Sprintf("%s:%d: %s", filename, pos, msg)
}
