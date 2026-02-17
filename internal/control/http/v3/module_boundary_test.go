package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleBoundaries_NoDirectServerStateFieldAccess ensures handlers use module deps
// snapshots rather than reaching directly into Server runtime fields.
func TestModuleBoundaries_NoDirectServerStateFieldAccess(t *testing.T) {
	forbiddenFields := map[string]struct{}{
		"v3Store":        {},
		"v3Bus":          {},
		"resumeStore":    {},
		"v3Scan":         {},
		"scanSource":     {},
		"admissionState": {},
	}

	allowedFiles := map[string]struct{}{
		"server.go":         {},
		"server_modules.go": {},
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read v3 dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			continue
		}
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._") {
			continue
		}
		if _, ok := allowedFiles[name]; ok {
			continue
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || recv.Name != "s" {
				return true
			}
			if _, bad := forbiddenFields[sel.Sel.Name]; !bad {
				return true
			}

			pos := fset.Position(sel.Pos())
			t.Errorf("%s:%d:%d direct access to s.%s is forbidden; use module deps snapshot", filepath.Base(pos.Filename), pos.Line, pos.Column, sel.Sel.Name)
			return true
		})
	}
}
