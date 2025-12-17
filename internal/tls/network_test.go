// SPDX-License-Identifier: MIT

package tls

import (
	"net"
	"testing"
)

func TestGetNetworkIPs(t *testing.T) {
	ips, err := GetNetworkIPs()
	if err != nil {
		t.Fatalf("GetNetworkIPs failed: %v", err)
	}

	// We should have at least one network IP (unless running in isolated environment)
	// Don't fail if no IPs found, just log
	if len(ips) == 0 {
		t.Log("No network IPs detected (may be expected in isolated environment)")
		return
	}

	// Verify all IPs are valid
	for _, ip := range ips {
		if ip == nil {
			t.Error("Got nil IP")
			continue
		}

		// Should not be loopback
		if ip.IsLoopback() {
			t.Errorf("Got loopback IP %s (should be filtered)", ip)
		}

		// Should not be link-local
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			t.Errorf("Got link-local IP %s (should be filtered)", ip)
		}

		t.Logf("Found network IP: %s", ip)
	}
}

func TestGenerateSelfSignedWithIPs(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := tmpDir + "/test.crt"
	keyPath := tmpDir + "/test.key"

	// Test with additional IPs
	additionalIPs := []net.IP{
		net.ParseIP("10.10.55.14"),
		net.ParseIP("192.168.1.100"),
		net.ParseIP("2001:db8::1"),
	}

	additionalDNS := []string{
		"xg2g.local",
		"myserver.home",
	}

	err := GenerateSelfSignedWithIPs(certPath, keyPath, 1, additionalIPs, additionalDNS)
	if err != nil {
		t.Fatalf("GenerateSelfSignedWithIPs failed: %v", err)
	}

	// Load and verify certificate
	cert, err := loadCertificate(certPath)
	if err != nil {
		t.Fatalf("Failed to load certificate: %v", err)
	}

	// Check that additional IPs are present
	foundIPs := make(map[string]bool)
	for _, ip := range cert.IPAddresses {
		foundIPs[ip.String()] = true
	}

	for _, ip := range additionalIPs {
		if !foundIPs[ip.String()] {
			t.Errorf("Expected IP %s not found in certificate", ip)
		}
	}

	// Check default IPs are still present
	defaultIPs := []string{"127.0.0.1", "::1"}
	for _, ip := range defaultIPs {
		if !foundIPs[ip] {
			t.Errorf("Default IP %s not found in certificate", ip)
		}
	}

	// Check DNS names
	foundDNS := make(map[string]bool)
	for _, dns := range cert.DNSNames {
		foundDNS[dns] = true
	}

	for _, dns := range additionalDNS {
		if !foundDNS[dns] {
			t.Errorf("Expected DNS name %s not found in certificate", dns)
		}
	}

	// Check default DNS names
	defaultDNS := []string{"localhost", "xg2g"}
	for _, dns := range defaultDNS {
		if !foundDNS[dns] {
			t.Errorf("Default DNS name %s not found in certificate", dns)
		}
	}
}

func TestGenerateSelfSignedWithIPs_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := tmpDir + "/test.crt"
	keyPath := tmpDir + "/test.key"

	// Test with duplicate IPs (should be deduplicated)
	additionalIPs := []net.IP{
		net.ParseIP("10.10.55.14"),
		net.ParseIP("10.10.55.14"), // duplicate
		net.ParseIP("127.0.0.1"),   // duplicate of default
	}

	additionalDNS := []string{
		"test.local",
		"test.local", // duplicate
		"localhost",  // duplicate of default
	}

	err := GenerateSelfSignedWithIPs(certPath, keyPath, 1, additionalIPs, additionalDNS)
	if err != nil {
		t.Fatalf("GenerateSelfSignedWithIPs failed: %v", err)
	}

	cert, err := loadCertificate(certPath)
	if err != nil {
		t.Fatalf("Failed to load certificate: %v", err)
	}

	// Count occurrences of test IP
	count := 0
	for _, ip := range cert.IPAddresses {
		if ip.String() == "10.10.55.14" {
			count++
		}
	}

	if count != 1 {
		t.Errorf("Expected IP 10.10.55.14 to appear once, got %d times", count)
	}

	// Count occurrences of test DNS
	dnsCount := 0
	for _, dns := range cert.DNSNames {
		if dns == "test.local" {
			dnsCount++
		}
	}

	if dnsCount != 1 {
		t.Errorf("Expected DNS test.local to appear once, got %d times", dnsCount)
	}
}
