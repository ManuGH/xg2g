// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

// validate is a CLI tool to validate xg2g YAML configuration files.
//
// Usage:
//
//	validate -f config.yaml
//	validate --file config.yaml
//
// Exit codes:
//   - 0: Configuration is valid
//   - 1: Configuration is invalid (parse or validation error)
//   - 2: Usage error (missing required flag)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ManuGH/xg2g/internal/config"
)

var Version = "dev"

func main() {
	var file string
	var showVersion bool

	flag.StringVar(&file, "file", "", "path to YAML configuration file")
	flag.StringVar(&file, "f", "", "path to YAML configuration file (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if file == "" {
		fmt.Fprintln(os.Stderr, "Error: --file is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  validate -f config.yaml")
		fmt.Fprintln(os.Stderr, "  validate --file config.yaml")
		os.Exit(2)
	}

	// Load configuration (uses strict YAML parsing)
	loader := config.NewLoader(file, Version)
	cfg, err := loader.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error in %s:\n", file)
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		os.Exit(1)
	}

	// Validate configuration (business logic validation)
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Validation error in %s:\n", file)
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ %s is valid\n", file)
}
