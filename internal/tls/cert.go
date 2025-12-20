// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

const (
	// DefaultCertPath is the default path for the TLS certificate
	DefaultCertPath = "certs/xg2g.crt"
	// DefaultKeyPath is the default path for the TLS key
	DefaultKeyPath = "certs/xg2g.key"
	// DefaultValidityYears is the default validity period for self-signed certificates
	DefaultValidityYears = 10
)

// Config holds configuration for certificate generation
type Config struct {
	CertPath string
	KeyPath  string
	Logger   zerolog.Logger
}

// EnsureCertificates checks if TLS certificates exist and generates self-signed ones if missing.
// Returns the paths to the certificate and key files.
func EnsureCertificates(cfg Config) (certPath, keyPath string, err error) {
	certPath = cfg.CertPath
	keyPath = cfg.KeyPath

	// Use defaults if not specified
	if certPath == "" {
		certPath = DefaultCertPath
	}
	if keyPath == "" {
		keyPath = DefaultKeyPath
	}

	// Check if both files exist
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)

	if certExists && keyExists {
		cfg.Logger.Debug().
			Str("cert", certPath).
			Str("key", keyPath).
			Msg("TLS certificates found")
		return certPath, keyPath, nil
	}

	// If only one exists, log warning and regenerate both
	if certExists || keyExists {
		cfg.Logger.Warn().
			Bool("cert_exists", certExists).
			Bool("key_exists", keyExists).
			Msg("Incomplete TLS certificate pair found, regenerating both")
	}

	// Generate new self-signed certificates
	cfg.Logger.Info().
		Str("cert", certPath).
		Str("key", keyPath).
		Msg("Generating self-signed TLS certificates")

	// Detect network IPs for certificate SANs
	networkIPs, err := GetNetworkIPs()
	if err != nil {
		cfg.Logger.Warn().
			Err(err).
			Msg("Failed to detect network IPs, certificate will only work for localhost")
		networkIPs = nil
	} else if len(networkIPs) > 0 {
		ipStrings := make([]string, len(networkIPs))
		for i, ip := range networkIPs {
			ipStrings[i] = ip.String()
		}
		cfg.Logger.Info().
			Strs("network_ips", ipStrings).
			Msg("Detected network IPs for certificate")
	}

	if err := GenerateSelfSignedWithIPs(certPath, keyPath, DefaultValidityYears, networkIPs, nil); err != nil {
		return "", "", fmt.Errorf("generate self-signed certificates: %w", err)
	}

	cfg.Logger.Info().
		Str("cert", certPath).
		Str("key", keyPath).
		Int("validity_years", DefaultValidityYears).
		Int("network_ips", len(networkIPs)).
		Msg("Self-signed TLS certificates generated successfully")

	return certPath, keyPath, nil
}

// GenerateSelfSigned generates a self-signed TLS certificate and private key.
// The certificate is valid for the specified number of years and includes localhost
// and common LAN hostnames/IPs.
func GenerateSelfSigned(certPath, keyPath string, validityYears int) error {
	return GenerateSelfSignedWithIPs(certPath, keyPath, validityYears, nil, nil)
}

// GenerateSelfSignedWithIPs generates a self-signed TLS certificate with custom IPs and DNS names.
// Additional IPs and DNS names are merged with the default localhost entries.
func GenerateSelfSignedWithIPs(certPath, keyPath string, validityYears int, additionalIPs []net.IP, additionalDNS []string) error {
	// Ensure directory exists
	certDir := filepath.Dir(certPath)
	if err := os.MkdirAll(certDir, 0750); err != nil {
		return fmt.Errorf("create cert directory: %w", err)
	}

	// Generate private key (ECDSA P-256 for modern, efficient crypto)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate private key: %w", err)
	}

	// Generate a random serial number
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("generate serial number: %w", err)
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.AddDate(validityYears, 0, 0)

	// Default IPs (localhost)
	defaultIPs := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
		net.ParseIP("0.0.0.0"),
		net.ParseIP("::"),
	}

	// Default DNS names
	defaultDNS := []string{
		"localhost",
		"localhost.localdomain",
		"xg2g",
	}

	// Merge with additional IPs and DNS names
	allIPs := append(defaultIPs, additionalIPs...)
	allDNS := append(defaultDNS, additionalDNS...)

	// Deduplicate IPs
	ipMap := make(map[string]net.IP)
	for _, ip := range allIPs {
		if ip != nil {
			ipMap[ip.String()] = ip
		}
	}
	uniqueIPs := make([]net.IP, 0, len(ipMap))
	for _, ip := range ipMap {
		uniqueIPs = append(uniqueIPs, ip)
	}

	// Deduplicate DNS names
	dnsMap := make(map[string]bool)
	for _, dns := range allDNS {
		if dns != "" {
			dnsMap[dns] = true
		}
	}
	uniqueDNS := make([]string, 0, len(dnsMap))
	for dns := range dnsMap {
		uniqueDNS = append(uniqueDNS, dns)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"xg2g Self-Signed"},
			CommonName:   "xg2g",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           uniqueIPs,
		DNSNames:              uniqueDNS,
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	// Write certificate to file
	// #nosec G304
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		_ = certOut.Close()
		return fmt.Errorf("encode certificate: %w", err)
	}
	if err := certOut.Close(); err != nil {
		return fmt.Errorf("close cert file: %w", err)
	}

	// Write private key to file
	// #nosec G304
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	privBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		_ = keyOut.Close()
		return fmt.Errorf("marshal private key: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		_ = keyOut.Close()
		return fmt.Errorf("encode private key: %w", err)
	}
	if err := keyOut.Close(); err != nil {
		return fmt.Errorf("close key file: %w", err)
	}

	return nil
}

// fileExists checks if a file exists and is not a directory
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// GetNetworkIPs returns all non-loopback IPv4 and IPv6 addresses from network interfaces.
// This is used to automatically include all server IPs in the self-signed certificate.
func GetNetworkIPs() ([]net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("get network interfaces: %w", err)
	}

	var ips []net.IP
	for _, iface := range interfaces {
		// Skip down interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip loopback addresses (already in defaults)
			if ip == nil || ip.IsLoopback() {
				continue
			}

			// Skip link-local addresses (169.254.x.x, fe80::/10)
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}

			// Add both IPv4 and IPv6
			ips = append(ips, ip)
		}
	}

	return ips, nil
}
