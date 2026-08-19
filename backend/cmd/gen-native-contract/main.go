// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Command gen-native-contract generates the iOS and Android model layer for the
// subset of the OpenAPI contract the native clients decode directly.
//
// The Go server and the TypeScript WebUI have been generated from
// api/openapi.yaml and drift-locked in CI for a while; the native clients were
// not, and hand-maintained their own idea of the same wire format. They drifted
// apart from the spec and from each other — see the pairing exchange, where the
// Android client decoded a response shape the server had already retired.
//
// This closes that gap with the mechanism already used everywhere else in the
// repository: generate from the single source, commit the result, and fail CI
// when the committed output no longer matches the spec.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

const contractExtension = "x-xg2g-native-contract"

type contractDecl struct {
	Roots []string `json:"roots"`
}

func main() {
	specPath := flag.String("spec", "api/openapi.yaml", "canonical OpenAPI document")
	swiftOut := flag.String("swift-out", "../ios/Xg2g/Generated/Xg2gContract.swift", "generated Swift contract")
	kotlinOut := flag.String("kotlin-out", "../android/app/src/main/java/io/github/manugh/xg2g/android/contract/Xg2gContract.kt", "generated Kotlin contract")
	flag.Parse()

	doc, err := loadSpec(*specPath)
	if err != nil {
		fatal(err)
	}

	roots, err := decodeRoots(doc.Extensions[contractExtension])
	if err != nil {
		fatal(err)
	}

	model, err := extract(doc, roots)
	if err != nil {
		fatal(err)
	}

	if err := write(*swiftOut, renderSwift(model)); err != nil {
		fatal(err)
	}
	if err := write(*kotlinOut, renderKotlin(model)); err != nil {
		fatal(err)
	}

	fmt.Printf("gen-native-contract: %d objects, %d enums\n", len(model.objects), len(model.enums))
	fmt.Printf("  - %s\n", *swiftOut)
	fmt.Printf("  - %s\n", *kotlinOut)
}

func loadSpec(path string) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate OpenAPI: %w", err)
	}
	return doc, nil
}

func decodeRoots(raw any) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("OpenAPI root extension %s is required", contractExtension)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", contractExtension, err)
	}
	var decl contractDecl
	if err := json.Unmarshal(data, &decl); err != nil {
		return nil, fmt.Errorf("decode %s: %w", contractExtension, err)
	}
	if len(decl.Roots) == 0 {
		return nil, fmt.Errorf("OpenAPI root extension %s must declare at least one root schema", contractExtension)
	}

	seen := map[string]bool{}
	for _, root := range decl.Roots {
		if seen[root] {
			return nil, fmt.Errorf("%s: duplicate root schema %q", contractExtension, root)
		}
		seen[root] = true
	}

	// Sorted so the generated output depends on the set of roots, never on the
	// order somebody happened to append them in.
	roots := append([]string(nil), decl.Roots...)
	sort.Strings(roots)
	return roots, nil
}

func write(path string, source []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create output directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, source, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen-native-contract:", err)
	os.Exit(1)
}
