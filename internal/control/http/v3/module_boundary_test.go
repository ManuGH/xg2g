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
		"v3Store":           {},
		"v3Bus":             {},
		"resumeStore":       {},
		"v3Scan":            {},
		"scanSource":        {},
		"admission":         {},
		"admissionState":    {},
		"preflightProvider": {},
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
		receiverNames := serverReceiverNames(f)

		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if _, isServerReceiver := receiverNames[recv.Name]; !isServerReceiver {
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

func serverReceiverNames(f *ast.File) map[string]struct{} {
	names := make(map[string]struct{})
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		recv := fn.Recv.List[0]
		if !isServerType(recv.Type) {
			continue
		}
		for _, n := range recv.Names {
			if n != nil && n.Name != "" {
				names[n.Name] = struct{}{}
			}
		}
	}
	return names
}

func isServerType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "Server"
	case *ast.StarExpr:
		return isServerType(t.X)
	default:
		return false
	}
}
