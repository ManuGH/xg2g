//go:build ignore

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzer(t *testing.T) {
	// Path to the testdata violation file
	// We use absolute path or relative to content root
	wd, _ := filepath.Abs(".")
	testDataPath := filepath.Join(wd, "testdata", "violation.go")

	// Run analyzer on the file (packages.Load accepts file paths)
	violations, err := Analyze("file=" + testDataPath)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	expectedViolations := []string{
		"context canceled",
		"deadline exceeded",
		"forbidden usage of domain detail constant",
	}

	for _, expected := range expectedViolations {
		found := false
		for _, v := range violations {
			if strings.Contains(v, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected violation containing %q, but not found. Got: %v", expected, violations)
		}
	}

	if len(violations) < 3 {
		t.Errorf("Expected at least 3 violations, got %d", len(violations))
	}
}
