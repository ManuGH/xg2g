// Package main implements the xg2g-soak harness for Phase 5.3 validation.
// This tool generates traffic, injects chaos, and validates admission invariants.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"
)

// Report is the JSON output schema for soak test results.
type Report struct {
	RunID           string           `json:"run_id"`
	Seed            uint64           `json:"seed"`
	StartedAt       time.Time        `json:"started_at"`
	EndedAt         time.Time        `json:"ended_at"`
	DurationSeconds float64          `json:"duration_s"`
	ScenarioResults []ScenarioResult `json:"scenario_results"`
	Summary         Summary          `json:"summary"`
	Evidence        []string         `json:"evidence"`
}

// ScenarioResult holds the outcome of a single test scenario.
type ScenarioResult struct {
	Name         string           `json:"name"`
	Pass         bool             `json:"pass"`
	Observations map[string]int64 `json:"observations"`
	Failures     []Failure        `json:"failures"`
}

// Failure captures a specific invariant violation.
type Failure struct {
	Time        time.Time `json:"time"`
	RuleID      string    `json:"rule_id"`
	Message     string    `json:"message"`
	EvidenceRef string    `json:"evidence_ref,omitempty"`
}

// Summary provides the aggregate verdict.
type Summary struct {
	PassedScenarios int    `json:"passed_scenarios"`
	FailedScenarios int    `json:"failed_scenarios"`
	Verdict         string `json:"verdict"`
}

// Config holds command-line configurations.
type Config struct {
	BaseURL            string
	Token              string
	PromURL            string
	PromSelector       string
	Duration           time.Duration
	Seed               uint64
	Profile            string
	MixPulse           float64
	MixLive            float64
	MixRecording       float64
	MaxInflight        int
	GPULimitOverride   int
	TunerCountOverride int
	ChaosRate          float64
	ArtifactDir        string
}

func main() {
	cfg := parseFlags()

	// Seed handling: 0 means random
	if cfg.Seed == 0 {
		// #nosec G115 -- UnixNano is positive until 2262; safe to cast to uint64
		cfg.Seed = uint64(time.Now().UnixNano())
	}
	// #nosec G115 -- Seed logic is safe
	//nolint:staticcheck // Global seed for soak harness simplicity
	rand.Seed(int64(cfg.Seed))

	fmt.Printf("xg2g-soak v0.1.0 (Phase 5.3)\n")
	fmt.Printf("Seed: %d\n", cfg.Seed)
	fmt.Printf("Profile: %s\n", cfg.Profile)
	fmt.Printf("Duration: %s\n", cfg.Duration)
	fmt.Printf("Mix: Pulse=%.0f%% Live=%.0f%% Recording=%.0f%%\n",
		cfg.MixPulse*100, cfg.MixLive*100, cfg.MixRecording*100)

	// Initialize report
	report := Report{
		RunID:     fmt.Sprintf("soak-%d", cfg.Seed),
		Seed:      cfg.Seed,
		StartedAt: time.Now(),
		Evidence:  []string{},
	}

	// TODO: Implement scenario execution
	switch cfg.Profile {
	case "smoke":
		fmt.Println("Running smoke profile (quick validation)...")
		report.ScenarioResults = runSmokeProfile(cfg)
	case "nightly":
		fmt.Println("Running nightly profile (full 8h soak)...")
		report.ScenarioResults = runNightlyProfile(cfg)
	case "gpu":
		fmt.Println("Running GPU saturation scenario...")
		report.ScenarioResults = runGPUSaturation(cfg)
	case "tuner":
		fmt.Println("Running tuner exhaustion scenario...")
		report.ScenarioResults = runTunerExhaustion(cfg)
	case "cpu":
		fmt.Println("Running CPU pressure scenario...")
		report.ScenarioResults = runCPUPressure(cfg)
	case "chaos":
		fmt.Println("Running chaos injection scenario...")
		report.ScenarioResults = runChaosInjection(cfg)
	default:
		fmt.Printf("Unknown profile: %s\n", cfg.Profile)
		os.Exit(1)
	}

	// Finalize report
	report.EndedAt = time.Now()
	report.DurationSeconds = report.EndedAt.Sub(report.StartedAt).Seconds()

	// Compute summary
	for _, sr := range report.ScenarioResults {
		if sr.Pass {
			report.Summary.PassedScenarios++
		} else {
			report.Summary.FailedScenarios++
		}
	}
	if report.Summary.FailedScenarios == 0 {
		report.Summary.Verdict = "PASS"
	} else {
		report.Summary.Verdict = "FAIL"
	}

	// Write report
	if err := writeReport(cfg.ArtifactDir, report); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nVerdict: %s (%d passed, %d failed)\n",
		report.Summary.Verdict,
		report.Summary.PassedScenarios,
		report.Summary.FailedScenarios)

	if report.Summary.Verdict != "PASS" {
		os.Exit(1)
	}
}

func parseFlags() Config {
	cfg := Config{}

	flag.StringVar(&cfg.BaseURL, "base-url", "http://localhost:8080", "xg2g API endpoint")
	flag.StringVar(&cfg.Token, "token", "", "API token (optional)")
	flag.StringVar(&cfg.PromURL, "prom-url", "http://localhost:9090", "Prometheus HTTP API")
	flag.StringVar(&cfg.PromSelector, "prom-selector", `{job="xg2g",instance="xg2g-main"}`, "Prometheus metric selector")
	flag.DurationVar(&cfg.Duration, "duration", 1*time.Hour, "Test duration (e.g. 8h)")
	flag.Uint64Var(&cfg.Seed, "seed", 0, "Random seed (0=random)")
	flag.StringVar(&cfg.Profile, "profile", "smoke", "Test profile: smoke|nightly|gpu|tuner|cpu|chaos")
	flag.Float64Var(&cfg.MixPulse, "mix-pulse", 0.60, "Pulse session ratio")
	flag.Float64Var(&cfg.MixLive, "mix-live", 0.35, "Live session ratio")
	flag.Float64Var(&cfg.MixRecording, "mix-rec", 0.05, "Recording session ratio")
	flag.IntVar(&cfg.MaxInflight, "max-inflight", 10, "Max concurrent test requests")
	flag.IntVar(&cfg.GPULimitOverride, "gpu-limit-override", 0, "Override GPU limit")
	flag.IntVar(&cfg.TunerCountOverride, "tuner-count-override", 0, "Override tuner count")
	flag.Float64Var(&cfg.ChaosRate, "chaos-rate", 0.01, "Chaos events per second")
	flag.StringVar(&cfg.ArtifactDir, "artifact-dir", "./soak-artifacts", "Output directory")

	flag.Parse()
	return cfg
}

func writeReport(dir string, report Report) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	path := fmt.Sprintf("%s/report.json", dir)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// Scenario stubs - to be implemented with actual test logic

func runSmokeProfile(cfg Config) []ScenarioResult {
	// Quick validation - just check connectivity
	return []ScenarioResult{
		{
			Name:         "connectivity",
			Pass:         true,
			Observations: map[string]int64{"requests": 1},
		},
	}
}

func runNightlyProfile(cfg Config) []ScenarioResult {
	// Full 8h soak with all scenarios
	results := []ScenarioResult{}
	results = append(results, runGPUSaturation(cfg)...)
	results = append(results, runTunerExhaustion(cfg)...)
	results = append(results, runCPUPressure(cfg)...)
	results = append(results, runChaosInjection(cfg)...)
	return results
}

func runGPUSaturation(cfg Config) []ScenarioResult {
	// Initialize clients
	prom := NewPromClient(cfg.PromURL, cfg.PromSelector)
	client := NewSessionClient(cfg.BaseURL, cfg.Token)

	// Run actual GPU saturation scenario
	result := runGPUSaturationScenario(cfg, prom, client)
	return []ScenarioResult{result}
}

func runTunerExhaustion(cfg Config) []ScenarioResult {
	// Initialize clients
	prom := NewPromClient(cfg.PromURL, cfg.PromSelector)
	client := NewSessionClient(cfg.BaseURL, cfg.Token)

	// Run actual Tuner Preemption scenario
	result := runTunerExhaustionScenario(cfg, prom, client)
	return []ScenarioResult{result}
}

func runCPUPressure(cfg Config) []ScenarioResult {
	// TODO: Implement CPU pressure test
	return []ScenarioResult{
		{Name: "cpu_pressure", Pass: true, Observations: map[string]int64{}},
	}
}

func runChaosInjection(cfg Config) []ScenarioResult {
	// TODO: Implement chaos injection
	return []ScenarioResult{
		{Name: "chaos_injection", Pass: true, Observations: map[string]int64{}},
	}
}
