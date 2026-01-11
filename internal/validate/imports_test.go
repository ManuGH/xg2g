// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package validate

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLayeringRules enforces architectural layering rules.
// See docs/arch/PACKAGE_LAYOUT.md for policy details.
func TestLayeringRules(t *testing.T) {
	projectRoot := findProjectRoot(t)

	// Known tech debt: temporary exemptions (to be removed incrementally)
	exemptions := map[string]bool{
		"internal/infra/ffmpeg/builder.go -> github.com/ManuGH/xg2g/internal/control/vod": true,
		"internal/infra/ffmpeg/probe.go -> github.com/ManuGH/xg2g/internal/control/vod":   true,
		"internal/infra/ffmpeg/runner.go -> github.com/ManuGH/xg2g/internal/control/vod":  true,
	}

	violations := []string{}

	// Rule 1: control/http/v3 MUST NOT directly import infra/* (bypasses domain layer)
	violations = append(violations, checkForbiddenImport(
		t, projectRoot,
		"internal/control/http/v3",
		"github.com/ManuGH/xg2g/internal/infra",
		"HTTP layer must not directly import infra layer (use domain layer instead)",
	)...)

	// Rule 2: domain/* MUST NOT import control/* (dependency inversion)
	violations = append(violations, checkForbiddenImport(
		t, projectRoot,
		"internal/domain",
		"github.com/ManuGH/xg2g/internal/control",
		"Domain layer must not import control layer",
	)...)

	// Rule 3: infra/* MAY import domain/*/ports (to implement interfaces) and domain/vod (pure types)
	// BUT MUST NOT import domain logic (e.g., domain/session/manager)
	// Allowed: domain/session/ports (hexagonal architecture), domain/vod (pure data types)
	violations = append(violations, checkForbiddenImportExcept(
		t, projectRoot,
		"internal/infra",
		"github.com/ManuGH/xg2g/internal/domain",
		[]string{
			"github.com/ManuGH/xg2g/internal/domain/session/ports",
			"github.com/ManuGH/xg2g/internal/domain/vod",
		},
		"Infra layer must not import domain logic (except domain/*/ports and domain/vod types)",
	)...)

	// Rule 4: infra/* MUST NOT import control/* (infra is lowest layer)
	violations = append(violations, checkForbiddenImport(
		t, projectRoot,
		"internal/infra",
		"github.com/ManuGH/xg2g/internal/control",
		"Infra layer must not import control layer",
	)...)

	// Rule 5: platform/* MUST NOT import config/* (platform is lower than config)
	violations = append(violations, checkForbiddenImport(
		t, projectRoot,
		"internal/platform",
		"github.com/ManuGH/xg2g/internal/config",
		"Platform layer must not import config layer",
	)...)

	// Rule 6: platform/* MUST NOT import domain/* (platform is lowest layer)
	violations = append(violations, checkForbiddenImport(
		t, projectRoot,
		"internal/platform",
		"github.com/ManuGH/xg2g/internal/domain",
		"Platform layer must not import domain layer",
	)...)

	// Rule 7: Enforce no imports of deprecated infrastructure/* (should be infra/*)
	violations = append(violations, checkForbiddenImport(
		t, projectRoot,
		"internal",
		"github.com/ManuGH/xg2g/internal/infrastructure",
		"Do not import internal/infrastructure (use internal/infra instead)",
	)...)

	// Filter out known exemptions
	filteredViolations := []string{}
	for _, v := range violations {
		if !isExempted(exemptions, v) {
			filteredViolations = append(filteredViolations, v)
		}
	}

	if len(filteredViolations) > 0 {
		t.Errorf("Layering violations detected:\n\n%s\n\nSee docs/arch/PACKAGE_LAYOUT.md for policy details.",
			strings.Join(filteredViolations, "\n"))
	}

	// Warn about exemptions (not a failure, just visibility)
	if len(violations) > len(filteredViolations) {
		t.Logf("⚠️  %d known layering violations are exempted (tech debt):", len(violations)-len(filteredViolations))
		for _, v := range violations {
			if isExempted(exemptions, v) {
				t.Logf("  %s", strings.Split(v, "\n")[0])
			}
		}
	}
}

// TestDeprecatedPackages ensures deprecated packages are not growing.
func TestDeprecatedPackages(t *testing.T) {
	projectRoot := findProjectRoot(t)

	violations := []string{}

	// Rule: internal/core is deprecated - fail if new files are added
	coreFiles, err := findGoFiles(filepath.Join(projectRoot, "internal/core"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to scan internal/core: %v", err)
	}

	// Baseline: 5 files as of 2026-01-11 (openwebif, pathutil, profile, urlutil, useragent)
	const coreBaseline = 5
	if len(coreFiles) > coreBaseline {
		violations = append(violations, fmt.Sprintf(
			"internal/core is deprecated but has grown: %d files (baseline: %d). DO NOT add new code here.",
			len(coreFiles), coreBaseline,
		))
	}

	if len(violations) > 0 {
		t.Errorf("Deprecated package violations:\n\n%s\n\nSee internal/core/README.md for migration plan.",
			strings.Join(violations, "\n"))
	}
}

// TestNoUtilsPackages prevents creation of "utils hell" packages.
func TestNoUtilsPackages(t *testing.T) {
	projectRoot := findProjectRoot(t)

	forbiddenDirs := []string{
		"internal/utils",
		"internal/util",
		"internal/common",
		"internal/helpers",
		"internal/shared",
	}

	violations := []string{}
	for _, dir := range forbiddenDirs {
		fullPath := filepath.Join(projectRoot, dir)
		if _, err := os.Stat(fullPath); err == nil {
			violations = append(violations, fmt.Sprintf(
				"Forbidden package detected: %s (see docs/arch/PACKAGE_LAYOUT.md)",
				dir,
			))
		}
	}

	if len(violations) > 0 {
		t.Errorf("Utils package violations:\n\n%s\n\nInstead of generic utils packages, use semantically named packages:\n- internal/platform/fs/security/\n- internal/domain/[feature]/\n- internal/control/http/client/",
			strings.Join(violations, "\n"))
	}
}

// --- Helper Functions ---

func checkForbiddenImport(t *testing.T, projectRoot, sourceDir, forbiddenImportPrefix, reason string) []string {
	return checkForbiddenImportExcept(t, projectRoot, sourceDir, forbiddenImportPrefix, nil, reason)
}

func checkForbiddenImportExcept(t *testing.T, projectRoot, sourceDir, forbiddenImportPrefix string, allowedImports []string, reason string) []string {
	t.Helper()

	sourcePath := filepath.Join(projectRoot, sourceDir)
	files, err := findGoFiles(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist - no violation
		}
		t.Fatalf("Failed to scan %s: %v", sourceDir, err)
	}

	// Build set of allowed imports for fast lookup
	allowedSet := make(map[string]bool)
	for _, allowed := range allowedImports {
		allowedSet[allowed] = true
	}

	violations := []string{}
	for _, file := range files {
		imports, err := extractImports(file)
		if err != nil {
			t.Logf("Warning: failed to parse %s: %v", file, err)
			continue
		}

		for _, imp := range imports {
			if strings.HasPrefix(imp, forbiddenImportPrefix) {
				// Check if this import is explicitly allowed
				if allowedSet[imp] {
					continue
				}
				relPath, _ := filepath.Rel(projectRoot, file)
				violations = append(violations, fmt.Sprintf(
					"  ❌ %s imports %s\n     Reason: %s",
					relPath, imp, reason,
				))
			}
		}
	}

	return violations
}

// isExempted checks if a violation is in the known exemption list.
func isExempted(exemptions map[string]bool, violation string) bool {
	// Extract file path and import from violation string
	// Format: "  ❌ <file> imports <import>\n     Reason: <reason>"
	lines := strings.Split(violation, "\n")
	if len(lines) == 0 {
		return false
	}
	firstLine := strings.TrimSpace(lines[0])
	firstLine = strings.TrimPrefix(firstLine, "❌ ")
	parts := strings.Split(firstLine, " imports ")
	if len(parts) != 2 {
		return false
	}
	file := strings.TrimSpace(parts[0])
	imp := strings.TrimSpace(parts[1])
	key := file + " -> " + imp
	return exemptions[key]
}

func findGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func extractImports(filePath string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	imports := []string{}
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		imports = append(imports, importPath)
	}
	return imports, nil
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Walk up until we find go.mod
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("Could not find project root (no go.mod found)")
		}
		dir = parent
	}
}
