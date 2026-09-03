// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdmissionGovernance_ASTCheck verifies that allocation methods (AcquireWithBoundTicket, ValidateBoundTicket)
// are referenced across allocation call paths and no rogue direct allocator bypasses exist in production code.
func TestAdmissionGovernance_ASTCheck(t *testing.T) {
	rootPath := filepath.Join("..", "..")
	fset := token.NewFileSet()

	boundAcquireFound := false
	validateBoundTicketFound := false
	directBypassDetected := false

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		node, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return parseErr
		}

		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			// Invariant: internal/stream/smoother must never act as a lease authority, reserve tuner leases, or dial directly.
			if strings.Contains(path, "internal/stream/smoother/") && !strings.HasSuffix(path, "_test.go") {
				if sel.Sel.Name == "ReserveStreamLeaseAtomic" || sel.Sel.Name == "AcquireTunerSlot" || sel.Sel.Name == "Dial" || sel.Sel.Name == "DialContext" {
					directBypassDetected = true
					t.Errorf("governance failure: smoother must never act as a tuner/lease authority or dial directly '%s' at %s:%d", sel.Sel.Name, path, fset.Position(sel.Pos()).Line)
				}
			}

			// Authorized consumer: smoother.Handler is allowed to consume shared ingest via exact h.manager.Acquire only.
			// No other file, receiver, or field name in internal/stream/smoother is permitted to make any Acquire calls.
			isPermittedSmootherAcquire := false
			if strings.HasSuffix(filepath.ToSlash(path), "internal/stream/smoother/handler.go") && sel.Sel.Name == "Acquire" {
				if subSel, ok := sel.X.(*ast.SelectorExpr); ok && subSel.Sel.Name == "manager" {
					if recv, ok2 := subSel.X.(*ast.Ident); ok2 && recv.Name == "h" {
						isPermittedSmootherAcquire = true
					}
				}
			}

			// Reject un-gated direct .Acquire() or .AcquireTunerSlot() calls outside authorized lease internal methods
			if (sel.Sel.Name == "AcquireTunerSlot" || sel.Sel.Name == "Acquire") &&
				!strings.Contains(path, "internal/pipeline/lease/") &&
				!strings.Contains(path, "internal/domain/receiverusage/") &&
				!strings.Contains(path, "internal/domain/session/manager/orchestrator_leases.go") &&
				!strings.Contains(path, "internal/stream/ingest/") &&
				!isPermittedSmootherAcquire &&
				!strings.Contains(path, "_test.go") {
				directBypassDetected = true
				t.Errorf("governance failure: un-gated direct lease call '%s' at %s:%d", sel.Sel.Name, path, fset.Position(sel.Pos()).Line)
			}

			if sel.Sel.Name == "AcquireWithBoundTicket" || sel.Sel.Name == "AcquireTunerSlotWithTicket" {
				boundAcquireFound = true
			}
			if sel.Sel.Name == "ValidateBoundTicket" {
				validateBoundTicketFound = true
			}
			return true
		})

		return nil
	})

	if err != nil {
		t.Fatalf("failed walking repository AST: %v", err)
	}

	if !boundAcquireFound {
		t.Errorf("governance check failed: AcquireWithBoundTicket must be defined and referenced")
	}

	if !validateBoundTicketFound {
		t.Errorf("governance check failed: ValidateBoundTicket must be defined and referenced")
	}

	if directBypassDetected {
		t.Errorf("governance check failed: un-gated direct allocator bypass detected")
	}
}

// TestSmoother_OutboundNetworkOwnershipForbidden verifies via AST that internal/stream/smoother
// production files cannot own, instantiate or use any outbound HTTP client, transport, or network dialer.
// The allowed data path is strictly: smoother -> session.Manager.Acquire -> shared ingest.
func TestSmoother_OutboundNetworkOwnershipForbidden(t *testing.T) {
	smootherDir := "../../internal/stream/smoother"
	fset := token.NewFileSet()

	entries, err := os.ReadDir(smootherDir)
	if err != nil {
		t.Fatalf("smoother directory must exist: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		filePath := filepath.Join(smootherDir, entry.Name())
		node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("must parse %s: %v", filePath, err)
		}

		// 1. Verify forbidden imports
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "net" || strings.HasPrefix(importPath, "net/http/httputil") {
				t.Errorf("%s imports forbidden network package %q: smoother must not own network connections", entry.Name(), importPath)
			}
		}

		// 2. Inspect AST for forbidden outbound types and calls
		ast.Inspect(node, func(n ast.Node) bool {
			// Check forbidden selector expressions
			if sel, ok := n.(*ast.SelectorExpr); ok {
				// Forbid http.Client, http.Transport, http.DefaultClient
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "http" {
					switch sel.Sel.Name {
					case "Client", "Transport", "DefaultClient", "RoundTripper",
						"Get", "Post", "PostForm", "Head", "NewRequest", "NewRequestWithContext":
						t.Errorf("%s references forbidden outbound http API '%s.%s' at %s:%d",
							entry.Name(), id.Name, sel.Sel.Name, filePath, fset.Position(sel.Pos()).Line)
					}
				}

				// Forbid .Do(...) and .RoundTrip(...) calls
				if sel.Sel.Name == "Do" || sel.Sel.Name == "RoundTrip" {
					t.Errorf("%s references forbidden client call '%s' at %s:%d: smoother must not execute outbound requests",
						entry.Name(), sel.Sel.Name, filePath, fset.Position(sel.Pos()).Line)
				}
			}

			return true
		})
	}
}
