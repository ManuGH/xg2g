// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

// Command gencert generates self-signed TLS certificates for xg2g.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ManuGH/xg2g/internal/tls"
)

func main() {
	certPath := flag.String("cert", "certs/xg2g.crt", "Path to certificate file")
	keyPath := flag.String("key", "certs/xg2g.key", "Path to key file")
	years := flag.Int("years", 10, "Certificate validity in years")
	flag.Parse()

	if err := tls.GenerateSelfSigned(*certPath, *keyPath, *years); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Self-signed TLS certificates generated:\n")
	fmt.Printf("   📄 Certificate: %s\n", *certPath)
	fmt.Printf("   🔑 Private Key: %s\n", *keyPath)
	fmt.Printf("   ⏱️  Valid for: %d years\n", *years)
}
