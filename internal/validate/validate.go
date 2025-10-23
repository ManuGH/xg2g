// SPDX-License-Identifier: MIT

// Package validate provides configuration validation utilities for the xg2g application.
package validate

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Error represents a validation error
type Error struct {
	Field   string      // Field name that failed validation
	Value   interface{} // The invalid value
	Message string      // Human-readable error message
}

// Error implements the error interface
func (e Error) Error() string {
	return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Message)
}

// Validator accumulates validation errors and can produce a ValidationError when invalid.
type Validator struct {
	errors []Error
}

// ValidationError bundles multiple validation errors into a single error value.
type ValidationError struct {
	errors []Error
}

// New creates a new validator
func New() *Validator {
	return &Validator{
		errors: make([]Error, 0),
	}
}

// AddError adds a validation error
func (v *Validator) AddError(field, message string, value interface{}) {
	v.errors = append(v.errors, Error{
		Field:   field,
		Value:   value,
		Message: message,
	})
}

// IsValid returns true if no errors have been accumulated
func (v *Validator) IsValid() bool {
	return len(v.errors) == 0
}

// Errors returns all accumulated validation errors
func (v *Validator) Errors() []Error {
	return v.errors
}

// Err converts the accumulated validation errors into an error value.
func (v *Validator) Err() error {
	if len(v.errors) == 0 {
		return nil
	}

	copied := make([]Error, len(v.errors))
	copy(copied, v.errors)

	return ValidationError{errors: copied}
}

// Errors returns the individual validation errors making up the validation failure.
func (e ValidationError) Errors() []Error {
	return e.errors
}

// Error implements the error interface for ValidationError.
func (e ValidationError) Error() string {
	if len(e.errors) == 0 {
		return ""
	}

	if len(e.errors) == 1 {
		return e.errors[0].Error()
	}

	// Multiple errors - format as list
	msgs := make([]string, len(e.errors))
	for i, err := range e.errors {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "; ")
}

// URL validates a URL string
func (v *Validator) URL(field, value string, allowedSchemes []string) {
	if value == "" {
		v.AddError(field, "URL cannot be empty", value)
		return
	}

	u, err := url.Parse(value)
	if err != nil {
		v.AddError(field, fmt.Sprintf("invalid URL: %v", err), value)
		return
	}

	if u.Host == "" {
		v.AddError(field, "URL must have a host", value)
		return
	}

	// Check allowed schemes
	if len(allowedSchemes) > 0 {
		schemeValid := false
		for _, scheme := range allowedSchemes {
			if u.Scheme == scheme {
				schemeValid = true
				break
			}
		}
		if !schemeValid {
			v.AddError(field,
				fmt.Sprintf("unsupported URL scheme %q (allowed: %v)", u.Scheme, allowedSchemes),
				value)
		}
	}
}

// Port validates a port number (1-65535)
func (v *Validator) Port(field string, port int) {
	if port <= 0 || port > 65535 {
		v.AddError(field,
			fmt.Sprintf("port must be between 1 and 65535, got %d", port),
			port)
	}
}

// Range validates that an integer is within a specified range (inclusive)
func (v *Validator) Range(field string, value, minVal, maxVal int) {
	if value < minVal || value > maxVal {
		v.AddError(field,
			fmt.Sprintf("value must be between %d and %d, got %d", minVal, maxVal, value),
			value)
	}
}

// Directory validates a directory path
// If mustExist is true, the directory must already exist
// If mustExist is false, the directory will be created if it doesn't exist
func (v *Validator) Directory(field, path string, mustExist bool) {
	if path == "" {
		v.AddError(field, "directory path cannot be empty", path)
		return
	}

	// Security: Check for path traversal
	if strings.Contains(path, "..") {
		v.AddError(field, "path contains traversal sequences (..)", path)
		return
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		v.AddError(field, fmt.Sprintf("invalid path: %v", err), path)
		return
	}

	// Check if exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if mustExist {
				v.AddError(field, "directory does not exist", path)
				return
			}
			// Try to create it
			if err := os.MkdirAll(absPath, 0750); err != nil {
				v.AddError(field, fmt.Sprintf("cannot create directory: %v", err), path)
				return
			}
			// Successfully created
			return
		}
		v.AddError(field, fmt.Sprintf("cannot access directory: %v", err), path)
		return
	}

	// Exists - check if it's actually a directory
	if !info.IsDir() {
		v.AddError(field, "path is not a directory", path)
	}
}

// NotEmpty validates that a string is not empty or whitespace-only
func (v *Validator) NotEmpty(field, value string) {
	if strings.TrimSpace(value) == "" {
		v.AddError(field, "value cannot be empty", value)
	}
}

// OneOf validates that a value is one of the allowed values
func (v *Validator) OneOf(field, value string, allowed []string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	v.AddError(field,
		fmt.Sprintf("value must be one of %v, got %q", allowed, value),
		value)
}

// Positive validates that a number is positive (> 0)
func (v *Validator) Positive(field string, value int) {
	if value <= 0 {
		v.AddError(field, fmt.Sprintf("value must be positive, got %d", value), value)
	}
}

// NonNegative validates that a number is non-negative (>= 0)
func (v *Validator) NonNegative(field string, value int) {
	if value < 0 {
		v.AddError(field, fmt.Sprintf("value cannot be negative, got %d", value), value)
	}
}

// Custom allows custom validation logic
// The validator function should return an error if validation fails
func (v *Validator) Custom(field string, value interface{}, validator func(interface{}) error) {
	if err := validator(value); err != nil {
		v.AddError(field, err.Error(), value)
	}
}

// Path validates a file path for security issues
// This function protects against path traversal attacks
func (v *Validator) Path(field, path string) {
	if path == "" {
		// Empty paths are allowed (optional fields)
		return
	}

	// Check 1: Must not be absolute
	if filepath.IsAbs(path) {
		v.AddError(field, fmt.Sprintf("must be relative path, got absolute: %s", path), path)
		return
	}

	// Check 2: Must not contain traversal sequences
	if strings.Contains(path, "..") {
		v.AddError(field, fmt.Sprintf("contains path traversal: %s", path), path)
		return
	}

	// Check 3: Clean and verify it's local (Go 1.20+)
	cleaned := filepath.Clean(path)
	if !filepath.IsLocal(cleaned) {
		v.AddError(field, fmt.Sprintf("is not a local path: %s", path), path)
		return
	}

	// Check 4: Resolve symlinks if file exists
	if _, err := os.Stat(cleaned); err == nil {
		resolved, err := filepath.EvalSymlinks(cleaned)
		if err != nil {
			v.AddError(field, fmt.Sprintf("symlink resolution failed: %v", err), path)
			return
		}
		// Ensure resolved path doesn't escape expected directory
		if !filepath.IsLocal(resolved) {
			v.AddError(field, fmt.Sprintf("resolves to non-local path: %s", resolved), path)
		}
	}
}

// PathWithinRoot validates that a path stays within a specified root directory
// This provides stronger guarantees against directory escape attacks
func (v *Validator) PathWithinRoot(field, path, rootDir string) {
	if path == "" {
		// Empty paths are allowed (optional fields)
		return
	}

	// Check 1: Must not be absolute
	if filepath.IsAbs(path) {
		v.AddError(field, fmt.Sprintf("must be relative path, got absolute: %s", path), path)
		return
	}

	// Check 2: Must not contain traversal sequences
	if strings.Contains(path, "..") {
		v.AddError(field, fmt.Sprintf("contains path traversal: %s", path), path)
		return
	}

	// Check 3: Clean the path
	cleaned := filepath.Clean(path)
	if !filepath.IsLocal(cleaned) {
		v.AddError(field, fmt.Sprintf("is not a local path: %s", path), path)
		return
	}

	// Check 4: Ensure root directory is absolute
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		v.AddError(field, fmt.Sprintf("cannot resolve root directory: %v", err), path)
		return
	}

	// Check 5: Join with root and resolve symlinks
	fullPath := filepath.Join(absRoot, cleaned)

	// If file exists, resolve symlinks
	if info, err := os.Stat(fullPath); err == nil {
		// File exists
		if info.IsDir() {
			v.AddError(field, fmt.Sprintf("path points to directory, expected file: %s", path), path)
			return
		}

		resolved, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			v.AddError(field, fmt.Sprintf("symlink resolution failed: %v", err), path)
			return
		}

		// Also resolve symlinks in the root to handle /var vs /private/var on macOS
		resolvedRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			// If root can't be resolved, use original
			resolvedRoot = absRoot
		}

		// Check 6: Ensure resolved path is within root
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			v.AddError(field, fmt.Sprintf("path escapes root directory: %s", path), path)
		}
	}
	// If file doesn't exist yet, that's okay - just validate the path structure
}

// StreamURL validates a stream URL for IPTV/streaming purposes
// Checks: valid URL syntax, http/https scheme, host present, path not empty
func (v *Validator) StreamURL(field, streamURL string) {
	if streamURL == "" {
		v.AddError(field, "stream URL cannot be empty", streamURL)
		return
	}

	// Parse URL
	u, err := url.Parse(streamURL)
	if err != nil {
		v.AddError(field, fmt.Sprintf("invalid URL syntax: %v", err), streamURL)
		return
	}

	// Validate scheme
	if u.Scheme != "http" && u.Scheme != "https" {
		v.AddError(field, fmt.Sprintf("unsupported scheme %q (must be http or https)", u.Scheme), streamURL)
		return
	}

	// Validate host
	if u.Host == "" {
		v.AddError(field, "stream URL must have a host", streamURL)
		return
	}

	// Validate path (stream URLs should have a path component)
	if u.Path == "" || u.Path == "/" {
		v.AddError(field, "stream URL must have a path component", streamURL)
	}
}
