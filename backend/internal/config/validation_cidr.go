// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"net"
	"strings"
)

// validateCIDRList validates a list of CIDR/IP entries and blocks forbidden networks.
// This is used for both TrustedProxies and RateLimitWhitelist to prevent security bypasses.
func validateCIDRList(key string, entries []string) error {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue // Empty entries are allowed (ignored)
		}

		// Try parsing as CIDR first
		ip, ipnet, err := net.ParseCIDR(entry)
		if err == nil {
			// Successfully parsed as CIDR
			if err := checkForbiddenNetwork(key, entry, ip, ipnet); err != nil {
				return err
			}
			continue
		}

		// Try parsing as single IP
		ip = net.ParseIP(entry)
		if ip == nil {
			return fmt.Errorf("invalid %s: invalid entry %q (must be CIDR or IP)", key, entry)
		}

		// Treat single IP as /32 or /128 CIDR
		if err := checkForbiddenIP(key, entry, ip); err != nil {
			return err
		}
	}
	return nil
}

// checkForbiddenNetwork checks if a CIDR network is forbidden (e.g., 0.0.0.0/0, ::/0).
func checkForbiddenNetwork(key, entry string, ip net.IP, ipnet *net.IPNet) error {
	ones, bits := ipnet.Mask.Size()

	// Block "any" networks (0.0.0.0/0 or ::/0)
	if ones == 0 {
		if bits == 32 {
			return fmt.Errorf("%s contains forbidden CIDR %q (trust-all IPv4 is not allowed)", key, entry)
		}
		if bits == 128 {
			return fmt.Errorf("%s contains forbidden CIDR %q (trust-all IPv6 is not allowed)", key, entry)
		}
	}

	// Block unspecified addresses (0.0.0.0/32 or ::/128)
	if ip.IsUnspecified() {
		if (bits == 32 && ones == 32) || (bits == 128 && ones == 128) {
			return fmt.Errorf("%s contains unspecified address %q (not allowed)", key, entry)
		}
	}

	return nil
}

// checkForbiddenIP checks if a single IP is forbidden (e.g., 0.0.0.0, ::).
func checkForbiddenIP(key, entry string, ip net.IP) error {
	if ip.IsUnspecified() {
		return fmt.Errorf("%s contains unspecified address %q (not allowed)", key, entry)
	}
	return nil
}
