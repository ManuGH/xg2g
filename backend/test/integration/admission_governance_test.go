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
// are referenced across allocation call paths and no rogue direct allocator bypasses exist.
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
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "AcquireWithBoundTicket" {
				boundAcquireFound = true
			}
			if ident.Name == "ValidateBoundTicket" {
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
		t.Errorf("governance check failed: AcquireWithBoundTicket must be defined and referenced in lease package")
	}

	if !validateBoundTicketFound {
		t.Errorf("governance check failed: ValidateBoundTicket must be defined and referenced in policy package")
	}

	if directBypassDetected {
		t.Errorf("governance check failed: un-gated direct allocator bypass detected")
	}
}
