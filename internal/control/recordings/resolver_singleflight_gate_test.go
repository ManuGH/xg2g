package recordings

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSingleflightKeyGate(t *testing.T) {
	fset := token.NewFileSet()
	filePath := filepath.Join(".", "manager.go")
	file, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		t.Fatalf("parse manager.go: %v", err)
	}

	doCalls := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Do" {
			return true
		}

		doCalls++
		return true
	})

	if doCalls != 1 {
		t.Fatalf("expected exactly one singleflight.Do call in manager.go, got %d", doCalls)
	}
}

func TestEnsureProbedBoundaryGate(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	fset := token.NewFileSet()
	var violations []string
	callCount := 0

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "ensureProbed" {
				return true
			}
			callCount++
			if name != "resolver.go" {
				violations = append(violations, name)
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf("ensureProbed calls must remain in resolver.go only; found in %v", violations)
	}
	if callCount == 0 {
		t.Fatalf("expected at least one ensureProbed call in resolver.go")
	}
}
