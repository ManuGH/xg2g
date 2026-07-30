// Command perfgate runs a versioned set of Go benchmarks and fails when a
// latency or allocation budget is exceeded.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const supportedSchemaVersion = 1

var (
	packagePattern   = regexp.MustCompile(`^\./[A-Za-z0-9_./-]+$`)
	benchmarkPattern = regexp.MustCompile(`^Benchmark[A-Za-z0-9_]+$`)
)

type budgetConfig struct {
	SchemaVersion int      `json:"schema_version"`
	Count         int      `json:"count"`
	Benchtime     string   `json:"benchtime"`
	Benchmarks    []budget `json:"benchmarks"`
}

type budget struct {
	Package        string  `json:"package"`
	Benchmark      string  `json:"benchmark"`
	MaxNSPerOp     float64 `json:"max_ns_per_op"`
	MaxBytesPerOp  uint64  `json:"max_bytes_per_op"`
	MaxAllocsPerOp uint64  `json:"max_allocs_per_op"`
}

type sample struct {
	NSPerOp     float64 `json:"ns_per_op"`
	BytesPerOp  uint64  `json:"bytes_per_op"`
	AllocsPerOp uint64  `json:"allocs_per_op"`
}

type benchmarkResult struct {
	Budget      budget   `json:"budget"`
	Samples     []sample `json:"samples"`
	MaxObserved sample   `json:"max_observed"`
	Pass        bool     `json:"pass"`
	Violations  []string `json:"violations"`
}

type gateReport struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	GoVersion     string            `json:"go_version"`
	GOOS          string            `json:"goos"`
	GOARCH        string            `json:"goarch"`
	Count         int               `json:"count"`
	Benchtime     string            `json:"benchtime"`
	Pass          bool              `json:"pass"`
	Results       []benchmarkResult `json:"results"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("perfgate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	budgetsPath := flags.String("budgets", "test/benchmark/budgets.json", "versioned performance budget file")
	reportPath := flags.String("report", "../artifacts/performance/report.json", "JSON report path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		writef(stderr, "perfgate: unexpected positional arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}

	cfg, err := loadBudgetConfig(*budgetsPath)
	if err != nil {
		writef(stderr, "perfgate: %v\n", err)
		return 2
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		writef(stderr, "perfgate: locate go binary: %v\n", err)
		return 2
	}

	report := gateReport{
		SchemaVersion: supportedSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Count:         cfg.Count,
		Benchtime:     cfg.Benchtime,
		Pass:          true,
		Results:       make([]benchmarkResult, 0, len(cfg.Benchmarks)),
	}

	ctx := context.Background()
	for _, item := range cfg.Benchmarks {
		result, output, runErr := runBenchmark(ctx, goBin, cfg, item)
		writef(stdout, "## %s %s\n%s", item.Package, item.Benchmark, output)
		if runErr != nil {
			writef(stderr, "perfgate: %s %s: %v\n", item.Package, item.Benchmark, runErr)
			return 1
		}
		report.Results = append(report.Results, result)
		if !result.Pass {
			report.Pass = false
			for _, violation := range result.Violations {
				writef(stderr, "perfgate: %s: %s\n", item.Benchmark, violation)
			}
		}
	}

	if err := writeReport(*reportPath, report); err != nil {
		writef(stderr, "perfgate: write report: %v\n", err)
		return 1
	}
	writef(stdout, "performance_report=%s verdict=%s\n", *reportPath, verdict(report.Pass))
	if !report.Pass {
		return 1
	}
	return 0
}

func loadBudgetConfig(path string) (budgetConfig, error) {
	// #nosec G304 -- the operator supplies a repository-local budget path.
	data, err := os.ReadFile(path)
	if err != nil {
		return budgetConfig{}, fmt.Errorf("read budgets: %w", err)
	}
	var cfg budgetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return budgetConfig{}, fmt.Errorf("decode budgets: %w", err)
	}
	if err := validateBudgetConfig(cfg); err != nil {
		return budgetConfig{}, err
	}
	return cfg, nil
}

func validateBudgetConfig(cfg budgetConfig) error {
	if cfg.SchemaVersion != supportedSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", cfg.SchemaVersion)
	}
	if cfg.Count < 2 || cfg.Count > 10 {
		return errors.New("count must be between 2 and 10")
	}
	duration, err := time.ParseDuration(cfg.Benchtime)
	if err != nil || duration < 50*time.Millisecond || duration > 5*time.Second {
		return errors.New("benchtime must be a duration between 50ms and 5s")
	}
	if len(cfg.Benchmarks) == 0 {
		return errors.New("at least one benchmark budget is required")
	}
	seen := make(map[string]bool, len(cfg.Benchmarks))
	for _, item := range cfg.Benchmarks {
		key := item.Package + ":" + item.Benchmark
		if seen[key] {
			return fmt.Errorf("duplicate benchmark budget %q", key)
		}
		seen[key] = true
		if !packagePattern.MatchString(item.Package) {
			return fmt.Errorf("invalid benchmark package %q", item.Package)
		}
		if !benchmarkPattern.MatchString(item.Benchmark) {
			return fmt.Errorf("invalid benchmark name %q", item.Benchmark)
		}
		if item.MaxNSPerOp <= 0 || item.MaxBytesPerOp == 0 || item.MaxAllocsPerOp == 0 {
			return fmt.Errorf("benchmark %q budgets must be positive", key)
		}
	}
	return nil
}

func runBenchmark(
	ctx context.Context,
	goBin string,
	cfg budgetConfig,
	item budget,
) (benchmarkResult, string, error) {
	args := []string{
		"test",
		"-run", "^$",
		"-bench", "^" + item.Benchmark + "$",
		"-benchmem",
		"-count", fmt.Sprintf("%d", cfg.Count),
		"-benchtime", cfg.Benchtime,
		item.Package,
	}
	// #nosec G204 -- package and benchmark values are constrained by strict allowlist regexes.
	cmd := exec.CommandContext(ctx, goBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return benchmarkResult{}, string(output), fmt.Errorf("go test benchmark failed: %w", err)
	}
	samples, err := parseSamples(string(output), item.Benchmark)
	if err != nil {
		return benchmarkResult{}, string(output), err
	}
	if len(samples) != cfg.Count {
		return benchmarkResult{}, string(output), fmt.Errorf(
			"benchmark returned %d samples, expected %d",
			len(samples),
			cfg.Count,
		)
	}
	return evaluate(item, samples), string(output), nil
}

func parseSamples(output, benchmark string) ([]sample, error) {
	linePattern := regexp.MustCompile(
		`^` + regexp.QuoteMeta(benchmark) +
			`(?:-\d+)?\s+\d+\s+([0-9.]+)\s+ns/op\s+(\d+)\s+B/op\s+(\d+)\s+allocs/op$`,
	)
	var samples []sample
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		matches := linePattern.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if len(matches) == 0 {
			continue
		}
		var value sample
		if _, err := fmt.Sscanf(
			strings.Join(matches[1:], " "),
			"%f %d %d",
			&value.NSPerOp,
			&value.BytesPerOp,
			&value.AllocsPerOp,
		); err != nil {
			return nil, fmt.Errorf("parse benchmark metrics: %w", err)
		}
		samples = append(samples, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan benchmark output: %w", err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("benchmark %s emitted no parseable metrics", benchmark)
	}
	return samples, nil
}

func evaluate(item budget, samples []sample) benchmarkResult {
	result := benchmarkResult{Budget: item, Samples: samples, Pass: true}
	for _, value := range samples {
		if value.NSPerOp > result.MaxObserved.NSPerOp {
			result.MaxObserved.NSPerOp = value.NSPerOp
		}
		if value.BytesPerOp > result.MaxObserved.BytesPerOp {
			result.MaxObserved.BytesPerOp = value.BytesPerOp
		}
		if value.AllocsPerOp > result.MaxObserved.AllocsPerOp {
			result.MaxObserved.AllocsPerOp = value.AllocsPerOp
		}
	}
	if result.MaxObserved.NSPerOp > item.MaxNSPerOp {
		result.Violations = append(result.Violations, fmt.Sprintf(
			"%.2f ns/op exceeds %.2f ns/op",
			result.MaxObserved.NSPerOp,
			item.MaxNSPerOp,
		))
	}
	if result.MaxObserved.BytesPerOp > item.MaxBytesPerOp {
		result.Violations = append(result.Violations, fmt.Sprintf(
			"%d B/op exceeds %d B/op",
			result.MaxObserved.BytesPerOp,
			item.MaxBytesPerOp,
		))
	}
	if result.MaxObserved.AllocsPerOp > item.MaxAllocsPerOp {
		result.Violations = append(result.Violations, fmt.Sprintf(
			"%d allocs/op exceeds %d allocs/op",
			result.MaxObserved.AllocsPerOp,
			item.MaxAllocsPerOp,
		))
	}
	result.Pass = len(result.Violations) == 0
	return result
}

func writeReport(path string, report gateReport) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(dir, ".performance-report-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func verdict(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

func writef(writer io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(writer, format, args...)
}
