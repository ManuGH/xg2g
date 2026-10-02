package jobs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Every production refresh must keep the IPTV source registry up to date.
// Registry population is an option of RefreshWithOptions, so a call site that
// forgets jobs.WithIPTVSources silently leaves the registry stale (a scheduled
// refresh rewrites the playlist but opaque IPTV ids from new bouquet entries
// would stay unresolvable). This guard scans the production tree so a new or
// existing call site cannot drop the option unnoticed.
func TestProductionRefreshCallSitesPassIPTVSources(t *testing.T) {
	root, err := filepath.Abs("..") // backend/internal
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	var checked int
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "testdata" || path == filepath.Join(root, "jobs") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "jobs" {
				return true
			}
			pos := fset.Position(call.Pos())
			switch sel.Sel.Name {
			case "Refresh":
				t.Errorf("%s: jobs.Refresh takes no options and cannot keep the IPTV registry current; use jobs.RefreshWithOptions with jobs.WithIPTVSources", pos)
			case "RefreshWithOptions":
				checked++
				if !passesIPTVSources(call) {
					t.Errorf("%s: jobs.RefreshWithOptions call does not pass jobs.WithIPTVSources", pos)
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	// Anti-vacuity: bootstrap (periodic/API refresh), bootstrap (initial refresh)
	// and the daemon scheduler exist today.
	if checked < 3 {
		t.Fatalf("expected at least 3 production RefreshWithOptions call sites, found %d (scan broken or call sites moved)", checked)
	}
}

func passesIPTVSources(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		inner, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := inner.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "jobs" && sel.Sel.Name == "WithIPTVSources" {
			return true
		}
	}
	return false
}
