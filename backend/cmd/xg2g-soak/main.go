// Command xg2g-soak runs operator-initiated playback lifecycle checks.
//
// Mutating profiles are intentionally restricted to the loopback maintainer
// staging port. They never target production and never inject host/container
// chaos.
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	profileSmoke   = "smoke"
	profileSoak    = "soak"
	profileNightly = "nightly"

	scenarioStatusPass = "pass"
	scenarioStatusFail = "fail"

	maxRecordedFailures = 100
)

type report struct {
	RunID           string           `json:"run_id"`
	Seed            uint64           `json:"seed"`
	Profile         string           `json:"profile"`
	Target          string           `json:"target"`
	StartedAt       time.Time        `json:"started_at"`
	EndedAt         time.Time        `json:"ended_at"`
	DurationSeconds float64          `json:"duration_s"`
	ScenarioResults []scenarioResult `json:"scenario_results"`
	Summary         summary          `json:"summary"`
}

type scenarioResult struct {
	Name         string           `json:"name"`
	Pass         bool             `json:"pass"`
	Status       string           `json:"status"`
	Observations map[string]int64 `json:"observations"`
	Failures     []failure        `json:"failures"`
}

type failure struct {
	Time    time.Time `json:"time"`
	RuleID  string    `json:"rule_id"`
	Message string    `json:"message"`
}

type summary struct {
	PassedScenarios int    `json:"passed_scenarios"`
	FailedScenarios int    `json:"failed_scenarios"`
	Verdict         string `json:"verdict"`
}

type config struct {
	BaseURL        string
	PromURL        string
	PromSelector   string
	TokenFile      string
	Duration       time.Duration
	HoldDuration   time.Duration
	ReadyTimeout   time.Duration
	Seed           uint64
	Profile        string
	CyclesPerSec   float64
	MaxInflight    int
	ServiceRefs    stringList
	ArtifactDir    string
	ConfirmStaging bool
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		writef(stderr, "xg2g-soak: %v\n", err)
		return 2
	}
	if err := validateConfig(&cfg); err != nil {
		writef(stderr, "xg2g-soak: %v\n", err)
		return 2
	}
	if cfg.Seed == 0 {
		cfg.Seed, err = randomSeed()
		if err != nil {
			writef(stderr, "xg2g-soak: generate random seed: %v\n", err)
			return 1
		}
	}

	apiToken := ""
	if cfg.Profile != profileSmoke {
		apiToken, err = readTokenFile(cfg.TokenFile)
		if err != nil {
			writef(stderr, "xg2g-soak: %v\n", err)
			return 2
		}
	}

	startedAt := time.Now().UTC()
	runReport := report{
		RunID:     fmt.Sprintf("soak-%d", cfg.Seed),
		Seed:      cfg.Seed,
		Profile:   cfg.Profile,
		Target:    cfg.BaseURL,
		StartedAt: startedAt,
	}

	writef(stdout, "xg2g-soak profile=%s target=%s seed=%d\n", cfg.Profile, cfg.BaseURL, cfg.Seed)
	client := newSessionClient(cfg.BaseURL, apiToken, nil)
	var prom *promClient
	if cfg.PromURL != "" {
		prom = newPromClient(cfg.PromURL, cfg.PromSelector, nil)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cfg.Profile {
	case profileSmoke:
		runReport.ScenarioResults = []scenarioResult{runSmoke(ctx, client, prom)}
	case profileSoak, profileNightly:
		// #nosec G404,G115 -- deterministic scheduling is required for reproducible soak reports.
		rng := mathrand.New(mathrand.NewSource(int64(cfg.Seed)))
		runReport.ScenarioResults = []scenarioResult{runLifecycleSoak(ctx, cfg, client, prom, rng)}
	default:
		writef(stderr, "xg2g-soak: unsupported profile %q\n", cfg.Profile)
		return 2
	}

	runReport.EndedAt = time.Now().UTC()
	runReport.DurationSeconds = runReport.EndedAt.Sub(startedAt).Seconds()
	for _, result := range runReport.ScenarioResults {
		if result.Pass {
			runReport.Summary.PassedScenarios++
		} else {
			runReport.Summary.FailedScenarios++
		}
	}
	if runReport.Summary.FailedScenarios == 0 {
		runReport.Summary.Verdict = "PASS"
	} else {
		runReport.Summary.Verdict = "FAIL"
	}

	reportPath, err := writeReport(cfg.ArtifactDir, runReport)
	if err != nil {
		writef(stderr, "xg2g-soak: write report: %v\n", err)
		return 1
	}
	writef(
		stdout,
		"verdict=%s passed=%d failed=%d report=%s\n",
		runReport.Summary.Verdict,
		runReport.Summary.PassedScenarios,
		runReport.Summary.FailedScenarios,
		reportPath,
	)
	if runReport.Summary.Verdict != "PASS" {
		return 1
	}
	return 0
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("xg2g-soak", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.BaseURL, "base-url", "http://127.0.0.1:8089", "xg2g loopback endpoint")
	flags.StringVar(&cfg.PromURL, "prom-url", "", "optional loopback Prometheus URL")
	flags.StringVar(
		&cfg.PromSelector,
		"prom-selector",
		`{job="xg2g",instance="xg2g-main"}`,
		"Prometheus target selector",
	)
	flags.StringVar(&cfg.TokenFile, "token-file", "", "0600 file containing a v3 API bearer token")
	flags.DurationVar(&cfg.Duration, "duration", 0, "submission window (soak default 1h, nightly default 8h)")
	flags.DurationVar(&cfg.HoldDuration, "hold", 15*time.Second, "time to keep each ready session alive")
	flags.DurationVar(&cfg.ReadyTimeout, "ready-timeout", 45*time.Second, "maximum time for a session to become ready")
	flags.Uint64Var(&cfg.Seed, "seed", 0, "reproducible random seed (0 generates one)")
	flags.StringVar(&cfg.Profile, "profile", profileSmoke, "profile: smoke|soak|nightly")
	flags.Float64Var(&cfg.CyclesPerSec, "cycles-per-second", 0.1, "new playback lifecycles submitted per second")
	flags.IntVar(&cfg.MaxInflight, "max-inflight", 2, "maximum concurrent playback lifecycles")
	flags.Var(&cfg.ServiceRefs, "service-ref", "repeatable Enigma2 service reference for mutating profiles")
	flags.StringVar(&cfg.ArtifactDir, "artifact-dir", "./artifacts/soak", "report output directory")
	flags.BoolVar(
		&cfg.ConfirmStaging,
		"confirm-staging",
		false,
		"confirm that mutating traffic may target loopback staging :8089",
	)
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	return cfg, nil
}

func validateConfig(cfg *config) error {
	cfg.Profile = strings.ToLower(strings.TrimSpace(cfg.Profile))
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.PromURL = strings.TrimRight(strings.TrimSpace(cfg.PromURL), "/")
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
	cfg.ArtifactDir = strings.TrimSpace(cfg.ArtifactDir)

	switch cfg.Profile {
	case profileSmoke:
		if cfg.Duration < 0 {
			return errors.New("duration cannot be negative")
		}
	case profileSoak:
		if cfg.Duration == 0 {
			cfg.Duration = time.Hour
		}
	case profileNightly:
		if cfg.Duration == 0 {
			cfg.Duration = 8 * time.Hour
		}
	default:
		return fmt.Errorf("profile must be one of smoke, soak, nightly (got %q)", cfg.Profile)
	}

	target, err := validateLoopbackURL(cfg.BaseURL, map[string]bool{"8080": true, "8088": true, "8089": true})
	if err != nil {
		return fmt.Errorf("base URL: %w", err)
	}
	if target.Port() == "8088" {
		return errors.New("production port 8088 is never a valid soak target")
	}

	if cfg.PromURL != "" {
		if _, err := validateLoopbackURL(cfg.PromURL, map[string]bool{"9090": true, "9091": true}); err != nil {
			return fmt.Errorf("prometheus URL: %w", err)
		}
	}
	if cfg.ArtifactDir == "" {
		return errors.New("artifact-dir is required")
	}
	if cfg.MaxInflight < 1 || cfg.MaxInflight > 100 {
		return errors.New("max-inflight must be between 1 and 100")
	}
	if math.IsNaN(cfg.CyclesPerSec) || math.IsInf(cfg.CyclesPerSec, 0) ||
		cfg.CyclesPerSec <= 0 || cfg.CyclesPerSec > 100 {
		return errors.New("cycles-per-second must be greater than 0 and at most 100")
	}
	if cfg.ReadyTimeout <= 0 || cfg.ReadyTimeout > 10*time.Minute {
		return errors.New("ready-timeout must be greater than 0 and at most 10m")
	}
	if cfg.HoldDuration < 0 || cfg.HoldDuration > time.Hour {
		return errors.New("hold must be between 0 and 1h")
	}

	if cfg.Profile == profileSmoke {
		return nil
	}
	if target.Port() != "8089" {
		return errors.New("mutating profiles are restricted to loopback staging port 8089")
	}
	if !cfg.ConfirmStaging {
		return errors.New("mutating profiles require --confirm-staging")
	}
	if cfg.Duration <= 0 || cfg.Duration > 24*time.Hour {
		return errors.New("duration must be greater than 0 and at most 24h")
	}
	if cfg.TokenFile == "" {
		return errors.New("mutating profiles require --token-file")
	}
	if len(cfg.ServiceRefs) == 0 {
		return errors.New("mutating profiles require at least one --service-ref")
	}
	for _, serviceRef := range cfg.ServiceRefs {
		if strings.TrimSpace(serviceRef) == "" || !strings.Contains(serviceRef, ":") {
			return fmt.Errorf("invalid service reference %q", serviceRef)
		}
	}
	return nil
}

func validateLoopbackURL(rawURL string, allowedPorts map[string]bool) (*url.URL, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, errors.New("scheme must be http or https")
	}
	if target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("userinfo, query, and fragment are not allowed")
	}
	if target.Path != "" && target.Path != "/" {
		return nil, errors.New("path must be empty")
	}
	host := target.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("host must be loopback")
	}
	if !allowedPorts[target.Port()] {
		return nil, fmt.Errorf("port %q is not allowed", target.Port())
	}
	return target, nil
}

func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("token file must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("token file permissions must be 0600 or stricter (got %04o)", info.Mode().Perm())
	}
	// #nosec G304 -- the operator explicitly supplies a validated, private regular token file.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", errors.New("token file must contain exactly one token")
	}
	return token, nil
}

func randomSeed() (uint64, error) {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(data[:]), nil
}

func runSmoke(ctx context.Context, client *sessionClient, prom *promClient) scenarioResult {
	result := scenarioResult{
		Name:         "readiness",
		Pass:         true,
		Status:       scenarioStatusPass,
		Observations: map[string]int64{},
		Failures:     []failure{},
	}

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result.Observations["readiness_requests"] = 1
	if err := client.ready(checkCtx); err != nil {
		addFailure(&result, "READINESS_FAILED", err)
	}
	if prom != nil {
		result.Observations["prometheus_queries"] = 1
		if err := prom.targetUp(checkCtx); err != nil {
			addFailure(&result, "PROMETHEUS_TARGET_DOWN", err)
		}
	}
	return result
}

type cycleResult struct {
	readyLatency time.Duration
	err          error
	cleanupErr   error
}

type lifecycleStats struct {
	attempted      atomic.Int64
	succeeded      atomic.Int64
	cleanupFailed  atomic.Int64
	readyLatencyMS atomic.Int64
	mu             sync.Mutex
	failures       []failure
}

func (stats *lifecycleStats) record(result cycleResult) {
	stats.attempted.Add(1)
	if result.err == nil {
		stats.succeeded.Add(1)
		stats.readyLatencyMS.Add(result.readyLatency.Milliseconds())
	} else {
		stats.recordFailure("PLAYBACK_LIFECYCLE_FAILED", result.err)
	}
	if result.cleanupErr != nil {
		stats.cleanupFailed.Add(1)
		stats.recordFailure("SESSION_CLEANUP_FAILED", result.cleanupErr)
	}
}

func (stats *lifecycleStats) recordFailure(ruleID string, err error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	if len(stats.failures) >= maxRecordedFailures {
		return
	}
	stats.failures = append(stats.failures, failure{
		Time:    time.Now().UTC(),
		RuleID:  ruleID,
		Message: err.Error(),
	})
}

func runLifecycleSoak(
	ctx context.Context,
	cfg config,
	client *sessionClient,
	prom *promClient,
	rng *mathrand.Rand,
) scenarioResult {
	result := scenarioResult{
		Name:         "live_playback_lifecycle",
		Pass:         true,
		Status:       scenarioStatusPass,
		Observations: map[string]int64{},
		Failures:     []failure{},
	}

	if err := client.ready(ctx); err != nil {
		addFailure(&result, "READINESS_FAILED", err)
		return result
	}
	if prom != nil {
		if err := prom.targetUp(ctx); err != nil {
			addFailure(&result, "PROMETHEUS_TARGET_DOWN", err)
			return result
		}
	}

	var stats lifecycleStats
	var workers sync.WaitGroup
	sem := make(chan struct{}, cfg.MaxInflight)
	submissionDone := time.NewTimer(cfg.Duration)
	defer submissionDone.Stop()
	interval := time.Duration(float64(time.Second) / cfg.CyclesPerSec)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var cycleNumber int64
	submit := func() bool {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return false
		case <-submissionDone.C:
			return false
		}
		cycleNumber++
		serviceRef := cfg.ServiceRefs[rng.Intn(len(cfg.ServiceRefs))]
		idempotencyPrefix := fmt.Sprintf("soak-%d-%d", cfg.Seed, cycleNumber)
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-sem }()
			cycleTimeout := cfg.ReadyTimeout + cfg.HoldDuration + 30*time.Second
			cycleCtx, cancel := context.WithTimeout(ctx, cycleTimeout)
			defer cancel()
			stats.record(runLifecycleCycle(cycleCtx, client, serviceRef, idempotencyPrefix, cfg))
		}()
		return true
	}

	if !submit() {
		stats.recordFailure("NO_CYCLE_SUBMITTED", errors.New("submission stopped before the first cycle"))
	} else {
	submissionLoop:
		for {
			select {
			case <-ctx.Done():
				stats.recordFailure("RUN_CANCELLED", ctx.Err())
				break submissionLoop
			case <-submissionDone.C:
				break submissionLoop
			case <-ticker.C:
				if !submit() {
					break submissionLoop
				}
			}
		}
	}
	workers.Wait()

	attempted := stats.attempted.Load()
	succeeded := stats.succeeded.Load()
	result.Observations["cycles_attempted"] = attempted
	result.Observations["cycles_succeeded"] = succeeded
	result.Observations["cycles_failed"] = attempted - succeeded
	result.Observations["cleanup_failures"] = stats.cleanupFailed.Load()
	result.Observations["max_inflight"] = int64(cfg.MaxInflight)
	result.Observations["submission_window_seconds"] = int64(cfg.Duration.Seconds())
	if succeeded > 0 {
		result.Observations["mean_ready_latency_ms"] = stats.readyLatencyMS.Load() / succeeded
	}
	stats.mu.Lock()
	result.Failures = append(result.Failures, stats.failures...)
	hadRecordedFailures := len(stats.failures) != 0
	stats.mu.Unlock()
	if attempted == 0 {
		addFailure(&result, "NO_CYCLES_COMPLETED", errors.New("no playback lifecycle was attempted"))
	}
	if hadRecordedFailures || attempted != succeeded || stats.cleanupFailed.Load() != 0 {
		result.Pass = false
		result.Status = scenarioStatusFail
	}
	return result
}

func runLifecycleCycle(
	ctx context.Context,
	client *sessionClient,
	serviceRef string,
	idempotencyPrefix string,
	cfg config,
) (result cycleResult) {
	decisionToken, err := client.resolvePlayback(ctx, serviceRef)
	if err != nil {
		result.err = fmt.Errorf("resolve playback for %q: %w", serviceRef, err)
		return result
	}
	accepted, err := client.startSession(ctx, serviceRef, decisionToken, idempotencyPrefix+"-start")
	if err != nil {
		result.err = fmt.Errorf("start playback for %q: %w", serviceRef, err)
		return result
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := client.stopSession(cleanupCtx, accepted.SessionID, idempotencyPrefix+"-stop"); err != nil {
			result.cleanupErr = fmt.Errorf("stop session %s: %w", accepted.SessionID, err)
		}
	}()

	readyStarted := time.Now()
	session, err := waitForReady(ctx, client, accepted.SessionID, cfg.ReadyTimeout)
	if err != nil {
		result.err = err
		return result
	}
	result.readyLatency = time.Since(readyStarted)
	if err := holdSession(ctx, client, session, cfg.HoldDuration); err != nil {
		result.err = err
	}
	return result
}

func waitForReady(
	ctx context.Context,
	client *sessionClient,
	sessionID string,
	timeout time.Duration,
) (sessionResponse, error) {
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		session, err := client.getSession(readyCtx, sessionID)
		if err != nil {
			return sessionResponse{}, fmt.Errorf("read session %s: %w", sessionID, err)
		}
		switch session.State {
		case "READY", "DRAINING":
			if strings.TrimSpace(session.PlaybackURL) == "" {
				return sessionResponse{}, fmt.Errorf("session %s became %s without playbackUrl", sessionID, session.State)
			}
			if session.HeartbeatIntervalSeconds <= 0 {
				return sessionResponse{}, fmt.Errorf(
					"session %s returned invalid heartbeat interval %d",
					sessionID,
					session.HeartbeatIntervalSeconds,
				)
			}
			return session, nil
		case "FAILED", "STOPPED", "CANCELLED":
			return sessionResponse{}, fmt.Errorf(
				"session %s entered terminal state %s (%s: %s)",
				sessionID,
				session.State,
				session.Reason,
				session.ReasonDetail,
			)
		}
		select {
		case <-readyCtx.Done():
			return sessionResponse{}, fmt.Errorf("session %s did not become ready: %w", sessionID, readyCtx.Err())
		case <-ticker.C:
		}
	}
}

func holdSession(
	ctx context.Context,
	client *sessionClient,
	session sessionResponse,
	holdDuration time.Duration,
) error {
	if holdDuration == 0 {
		return nil
	}
	holdTimer := time.NewTimer(holdDuration)
	defer holdTimer.Stop()
	heartbeatEvery := time.Duration(session.HeartbeatIntervalSeconds) * time.Second
	heartbeatTicker := time.NewTicker(heartbeatEvery)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("hold session %s: %w", session.SessionID, ctx.Err())
		case <-holdTimer.C:
			return nil
		case <-heartbeatTicker.C:
			if err := client.heartbeat(ctx, session.SessionID); err != nil {
				return fmt.Errorf("heartbeat session %s: %w", session.SessionID, err)
			}
		}
	}
}

func addFailure(result *scenarioResult, ruleID string, err error) {
	result.Pass = false
	result.Status = scenarioStatusFail
	result.Failures = append(result.Failures, failure{
		Time:    time.Now().UTC(),
		RuleID:  ruleID,
		Message: err.Error(),
	})
}

func writeReport(dir string, value report) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "report.json")
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(dir, ".report-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return "", err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func writef(writer io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(writer, format, args...)
}
