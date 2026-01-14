package recordings

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestSingleflightKeyGate(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join(".", "truth.go")
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse resolver.go: %v", err)
	}

	var violations []token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Do" {
			return true
		}

		if len(call.Args) == 0 {
			violations = append(violations, call.Pos())
			return true
		}

		firstCall, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			violations = append(violations, call.Pos())
			return true
		}

		ident, ok := firstCall.Fun.(*ast.Ident)
		if !ok || ident.Name != "hashSingleflightKey" {
			violations = append(violations, call.Pos())
		}
		return true
	})

	if len(violations) > 0 {
		t.Fatalf("singleflight.Do must use hashSingleflightKey as the first argument")
	}
}
