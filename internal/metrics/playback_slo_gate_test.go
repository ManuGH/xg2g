package metrics

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMetrics_NoForbiddenHighCardinalityLabels(t *testing.T) {
	forbidden := map[string]struct{}{
		"request_id":   {},
		"session_id":   {},
		"recording_id": {},
		"service_ref":  {},
	}

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, filepath.Clean(path), nil, 0)
		if parseErr != nil {
			return parseErr
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			funcName := sel.Sel.Name
			if !strings.HasSuffix(funcName, "Vec") {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "promauto" {
				return true
			}

			labels, ok := call.Args[1].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range labels.Elts {
				lit, ok := elt.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				label, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if _, bad := forbidden[label]; bad {
					pos := fset.Position(lit.Pos())
					t.Errorf("%s:%d forbidden high-cardinality metric label: %s", pos.Filename, pos.Line, label)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan metrics dir: %v", err)
	}
}
