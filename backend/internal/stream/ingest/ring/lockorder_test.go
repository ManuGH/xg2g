// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Two locks with an order between them is a deadlock waiting for a third caller.
// The order is ingestMu before mu, always, and it is written in comments where it
// matters - but a comment does not fail a build. This reads the package instead.
//
// Deliberately a source check rather than a lock abstraction. There are two
// mutexes and a handful of functions that take them; wrapping that in a type with
// an enforced ordering would be a larger thing than the problem, and would itself
// need to be trusted.
func TestLockOrder_IngestMuIsAlwaysTakenBeforeMu(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	checked := 0
	for _, p := range pkg {
		for name, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					return true
				}

				// The order the two locks are taken in, in source order.
				var order []string
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Lock" {
						return true
					}
					inner, ok := sel.X.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch inner.Sel.Name {
					case "mu", "ingestMu":
						order = append(order, inner.Sel.Name)
					}
					return true
				})

				if len(order) < 2 {
					return true
				}
				checked++

				seenMu := false
				for _, l := range order {
					if l == "mu" {
						seenMu = true
						continue
					}
					if l == "ingestMu" && seenMu {
						t.Errorf("%s: %s takes mu before ingestMu (%v); the order is ingestMu first, always",
							filepath.Base(fset.Position(fn.Pos()).Filename), fn.Name.Name, order)
					}
				}
				_ = name
				return true
			})
		}
	}

	if checked == 0 {
		t.Fatal("no function was found taking both locks; this check has stopped checking anything")
	}
	t.Logf("%d functions take both locks, all in the order ingestMu -> mu", checked)
}
