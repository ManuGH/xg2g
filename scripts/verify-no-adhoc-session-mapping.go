// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

func main() {
	path := "./internal/control/http/v3"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	violations, err := Analyze(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analysis failed: %v\n", err)
		os.Exit(1)
	}

	if len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "❌ ad-hoc session mapping violations found:")
		for _, v := range violations {
			fmt.Fprintln(os.Stderr, v)
		}
		os.Exit(1)
	}
}

// Analyze performs the AST checks on the given package pattern/path
func Analyze(pattern string) ([]string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedName,
		// We use . as Dir to resolve relative paths like ./internal/... correctly
		Dir: ".",
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
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
			// Skip tests and the mapping SSOT
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			if strings.HasSuffix(filename, filepath.Join("internal", "control", "http", "v3", "session_mapping.go")) {
				continue
			}

			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				// Check for string literals "context canceled" or "deadline exceeded"
				case *ast.BasicLit:
					if node.Kind == token.STRING {
						val, _ := strconv.Unquote(node.Value)
						if strings.Contains(strings.ToLower(val), "context canceled") ||
							strings.Contains(strings.ToLower(val), "deadline exceeded") {
							violations = append(violations, formatViolation(filename, node.Pos(), fmt.Sprintf("forbidden string literal %q (use session_mapping.go)", val)))
						}
					}

				// Check for usage of model.D* constants (ReasonDetailCode)
				case *ast.SelectorExpr:
					if isModelDetailConstant(node, pkg.TypesInfo) {
						violations = append(violations, formatViolation(filename, node.Pos(), "forbidden usage of domain detail constant (use session_mapping.go)"))
					}
				}
				return true
			})
		}
	}
	return violations, nil
}

func formatViolation(filename string, pos token.Pos, msg string) string {
	// Attempt to get relative path for cleaner output, fall back to abs
	rel, err := filepath.Rel(".", filename)
	if err == nil {
		filename = rel
	}
	return fmt.Sprintf("%s:%d: %s", filename, pos, msg)
}

func isModelDetailConstant(sel *ast.SelectorExpr, info *types.Info) bool {
	// We want to detect usage of D* constants from internal/domain/session/model
	obj := info.ObjectOf(sel.Sel)
	if obj == nil {
		return false
	}
	pkg := obj.Pkg()
	if pkg == nil {
		return false
	}
	if !strings.HasSuffix(pkg.Path(), "internal/domain/session/model") {
		return false
	}

	// Check if the object is a constant and name starts with D
	_, isConst := obj.(*types.Const)
	if !isConst {
		return false
	}
	return strings.HasPrefix(obj.Name(), "D")
}
