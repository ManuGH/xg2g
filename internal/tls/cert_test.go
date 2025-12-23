// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

// loadCertificate loads and parses a certificate file
func loadCertificate(certPath string) (*x509.Certificate, error) {
	// #nosec G304 - Testing file
	certPEM, err := os.ReadFile(certPath)

	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, os.ErrInvalid
	}

	return x509.ParseCertificate(block.Bytes)
}

func TestGenerateSelfSigned(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")
	keyPath := filepath.Join(tmpDir, "test.key")

	// Generate certificate
	err := GenerateSelfSigned(certPath, keyPath, 1)
	if err != nil {
		t.Fatalf("GenerateSelfSigned failed: %v", err)
	}

	// Verify files were created
	if !fileExists(certPath) {
		t.Error("Certificate file was not created")
	}
	if !fileExists(keyPath) {
		t.Error("Key file was not created")
	}

	// Verify certificate can be loaded
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("Failed to load generated certificate: %v", err)
	}

	if cert.Certificate == nil {
		t.Error("Certificate is nil")
	}
}

func TestEnsureCertificates_Generate(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "auto.crt")
	keyPath := filepath.Join(tmpDir, "auto.key")

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	cfg := Config{
		CertPath: certPath,
		KeyPath:  keyPath,
		Logger:   logger,
	}

	// First call should generate certificates
	gotCert, gotKey, err := EnsureCertificates(cfg)
	if err != nil {
		t.Fatalf("EnsureCertificates failed: %v", err)
	}

	if gotCert != certPath {
		t.Errorf("Expected cert path %s, got %s", certPath, gotCert)
	}
	if gotKey != keyPath {
		t.Errorf("Expected key path %s, got %s", keyPath, gotKey)
	}

	// Verify files exist
	if !fileExists(certPath) {
		t.Error("Certificate was not generated")
	}
	if !fileExists(keyPath) {
		t.Error("Key was not generated")
	}
}

func TestEnsureCertificates_Existing(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "existing.crt")
	keyPath := filepath.Join(tmpDir, "existing.key")

	// Generate initial certificates
	if err := GenerateSelfSigned(certPath, keyPath, 1); err != nil {
		t.Fatalf("Failed to generate initial certificates: %v", err)
	}

	// Get modification times
	certInfo, _ := os.Stat(certPath)
	keyInfo, _ := os.Stat(keyPath)
	originalCertModTime := certInfo.ModTime()
	originalKeyModTime := keyInfo.ModTime()

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	cfg := Config{
		CertPath: certPath,
		KeyPath:  keyPath,
		Logger:   logger,
	}

	// Second call should not regenerate
	gotCert, gotKey, err := EnsureCertificates(cfg)
	if err != nil {
		t.Fatalf("EnsureCertificates failed: %v", err)
	}

	if gotCert != certPath {
		t.Errorf("Expected cert path %s, got %s", certPath, gotCert)
	}
	if gotKey != keyPath {
		t.Errorf("Expected key path %s, got %s", keyPath, gotKey)
	}

	// Verify files were not modified
	certInfo, _ = os.Stat(certPath)
	keyInfo, _ = os.Stat(keyPath)

	if !certInfo.ModTime().Equal(originalCertModTime) {
		t.Error("Certificate was regenerated when it should not have been")
	}
	if !keyInfo.ModTime().Equal(originalKeyModTime) {
		t.Error("Key was regenerated when it should not have been")
	}
}

func TestEnsureCertificates_IncompleteRegenerate(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "incomplete.crt")
	keyPath := filepath.Join(tmpDir, "incomplete.key")

	// Create only the cert file (incomplete pair)
	if err := os.WriteFile(certPath, []byte("dummy cert"), 0600); err != nil {
		t.Fatalf("Failed to create dummy cert: %v", err)
	}

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	cfg := Config{
		CertPath: certPath,
		KeyPath:  keyPath,
		Logger:   logger,
	}

	// Should regenerate both
	gotCert, gotKey, err := EnsureCertificates(cfg)
	if err != nil {
		t.Fatalf("EnsureCertificates failed: %v", err)
	}

	if gotCert != certPath {
		t.Errorf("Expected cert path %s, got %s", certPath, gotCert)
	}
	if gotKey != keyPath {
		t.Errorf("Expected key path %s, got %s", keyPath, gotKey)
	}

	// Verify both files exist and are valid
	_, err = tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Errorf("Generated certificate pair is invalid: %v", err)
	}
}

func TestEnsureCertificates_DefaultPaths(t *testing.T) {
	// This test runs in temp directory to avoid polluting the repo
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	logger := zerolog.New(os.Stdout).Level(zerolog.Disabled)

	cfg := Config{
		Logger: logger,
	}

	// Should use default paths
	gotCert, gotKey, err := EnsureCertificates(cfg)
	if err != nil {
		t.Fatalf("EnsureCertificates failed: %v", err)
	}

	if gotCert != DefaultCertPath {
		t.Errorf("Expected default cert path %s, got %s", DefaultCertPath, gotCert)
	}
	if gotKey != DefaultKeyPath {
		t.Errorf("Expected default key path %s, got %s", DefaultKeyPath, gotKey)
	}

	if !fileExists(gotCert) {
		t.Error("Certificate was not generated at default path")
	}
	if !fileExists(gotKey) {
		t.Error("Key was not generated at default path")
	}
}
