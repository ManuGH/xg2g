package main

import (
	"strings"
	"testing"
)

func TestParseSamplesRequiresExactBenchmarkLines(t *testing.T) {
	t.Parallel()

	output := `goos: linux
BenchmarkHotPath-4    1000    12.5 ns/op    16 B/op    1 allocs/op
BenchmarkOther-4      1000    99.0 ns/op    32 B/op    2 allocs/op
BenchmarkHotPath-4    1000    14.0 ns/op    16 B/op    1 allocs/op
PASS
`
	samples, err := parseSamples(output, "BenchmarkHotPath")
	if err != nil {
		t.Fatalf("parse samples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples=%d, want 2", len(samples))
	}
	if samples[1].NSPerOp != 14 || samples[1].BytesPerOp != 16 || samples[1].AllocsPerOp != 1 {
		t.Fatalf("second sample=%#v", samples[1])
	}
}

func TestEvaluateFailsEveryExceededBudget(t *testing.T) {
	t.Parallel()

	result := evaluate(
		budget{
			Package:        "./internal/example",
			Benchmark:      "BenchmarkHotPath",
			MaxNSPerOp:     10,
			MaxBytesPerOp:  20,
			MaxAllocsPerOp: 2,
		},
		[]sample{{NSPerOp: 11, BytesPerOp: 21, AllocsPerOp: 3}},
	)
	if result.Pass {
		t.Fatal("expected result to fail")
	}
	if len(result.Violations) != 3 {
		t.Fatalf("violations=%v", result.Violations)
	}
}

func TestValidateBudgetConfigRejectsUnsafeAndDuplicateInputs(t *testing.T) {
	t.Parallel()

	valid := budget{
		Package:        "./internal/example",
		Benchmark:      "BenchmarkHotPath",
		MaxNSPerOp:     10,
		MaxBytesPerOp:  20,
		MaxAllocsPerOp: 2,
	}
	tests := []struct {
		name       string
		benchmarks []budget
		want       string
	}{
		{
			name: "unsafe package",
			benchmarks: []budget{{
				Package:        "./internal/example;echo",
				Benchmark:      valid.Benchmark,
				MaxNSPerOp:     valid.MaxNSPerOp,
				MaxBytesPerOp:  valid.MaxBytesPerOp,
				MaxAllocsPerOp: valid.MaxAllocsPerOp,
			}},
			want: "invalid benchmark package",
		},
		{
			name:       "duplicate",
			benchmarks: []budget{valid, valid},
			want:       "duplicate benchmark budget",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateBudgetConfig(budgetConfig{
				SchemaVersion: supportedSchemaVersion,
				Count:         3,
				Benchtime:     "200ms",
				Benchmarks:    tt.benchmarks,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}
