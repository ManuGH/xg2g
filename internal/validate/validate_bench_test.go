// SPDX-License-Identifier: MIT
package validate

import (
	"testing"
)

func resetValidator(v *Validator) {
	v.errors = v.errors[:0]
}

// BenchmarkValidatorNotEmpty benchmarks NotEmpty validation
func BenchmarkValidatorNotEmpty(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.NotEmpty("field", "value")
		resetValidator(v)
	}
}

// BenchmarkValidatorRange benchmarks Range validation
func BenchmarkValidatorRange(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Range("port", 8080, 1, 65535)
		resetValidator(v)
	}
}

// BenchmarkValidatorURL benchmarks URL validation
func BenchmarkValidatorURL(b *testing.B) {
	v := New()
	url := "http://example.com:8080/path?query=value"
	allowedSchemes := []string{"http", "https"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.URL("url", url, allowedSchemes)
		resetValidator(v)
	}
}

// BenchmarkValidatorDirectory benchmarks Directory validation
func BenchmarkValidatorDirectory(b *testing.B) {
	v := New()
	path := b.TempDir()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Directory("dir", path, true)
		resetValidator(v)
	}
}

// BenchmarkValidatorMultipleChecks benchmarks realistic validation scenario
func BenchmarkValidatorMultipleChecks(b *testing.B) {
	v := New()
	path := b.TempDir()
	url := "http://example.com"
	allowedSchemes := []string{"http", "https"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.NotEmpty("url", url)
		v.URL("url", url, allowedSchemes)
		v.NotEmpty("port", "8080")
		v.Range("port", 8080, 1, 65535)
		v.NotEmpty("dir", path)
		v.Directory("dir", path, true)
		resetValidator(v)
	}
}

// BenchmarkValidatorWithErrors benchmarks validator with errors
func BenchmarkValidatorWithErrors(b *testing.B) {
	v := New()
	allowedSchemes := []string{"http", "https"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.NotEmpty("field", "")                    // Will fail
		v.Range("port", 99999, 1, 65535)           // Will fail
		v.URL("url", "invalid://", allowedSchemes) // Will fail
		_ = v.IsValid()
		_ = v.Errors()
		resetValidator(v)
	}
}
