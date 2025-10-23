package validate

import (
	"testing"
)

// BenchmarkValidatorRequired benchmarks Required validation
func BenchmarkValidatorRequired(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Required("field", "value")
		v.Clear()
	}
}

// BenchmarkValidatorRange benchmarks Range validation
func BenchmarkValidatorRange(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Range("port", 8080, 1, 65535)
		v.Clear()
	}
}

// BenchmarkValidatorURL benchmarks URL validation
func BenchmarkValidatorURL(b *testing.B) {
	v := New()
	url := "http://example.com:8080/path?query=value"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.URL("url", url)
		v.Clear()
	}
}

// BenchmarkValidatorDirectory benchmarks Directory validation
func BenchmarkValidatorDirectory(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Directory("dir", "/tmp")
		v.Clear()
	}
}

// BenchmarkValidatorMultipleChecks benchmarks realistic validation scenario
func BenchmarkValidatorMultipleChecks(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Required("url", "http://example.com")
		v.URL("url", "http://example.com")
		v.Required("port", "8080")
		v.Range("port", 8080, 1, 65535)
		v.Required("dir", "/tmp")
		v.Directory("dir", "/tmp")
		v.Clear()
	}
}

// BenchmarkValidatorWithErrors benchmarks validator with errors
func BenchmarkValidatorWithErrors(b *testing.B) {
	v := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		v.Required("field", "") // Will fail
		v.Range("port", 99999, 1, 65535) // Will fail
		v.URL("url", "invalid://") // Will fail
		_ = v.HasErrors()
		_ = v.Errors()
		v.Clear()
	}
}
