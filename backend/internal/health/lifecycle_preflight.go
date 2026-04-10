// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package health

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
)

type LifecycleOperation string

const (
	LifecycleOperationStartup  LifecycleOperation = "startup"
	LifecycleOperationInstall  LifecycleOperation = "install"
	LifecycleOperationUpgrade  LifecycleOperation = "upgrade"
	LifecycleOperationRestore  LifecycleOperation = "restore"
	LifecycleOperationRollback LifecycleOperation = "rollback"
)

type LifecyclePreflightSeverity string

const (
	LifecyclePreflightSeverityOK    LifecyclePreflightSeverity = "ok"
	LifecyclePreflightSeverityWarn  LifecyclePreflightSeverity = "warn"
	LifecyclePreflightSeverityBlock LifecyclePreflightSeverity = "block"
	LifecyclePreflightSeverityFatal LifecyclePreflightSeverity = "fatal"
)

type LifecyclePreflightFinding struct {
	Code     string                     `json:"code"`
	Severity LifecyclePreflightSeverity `json:"severity"`
	Contract string                     `json:"contract,omitempty"`
	Field    string                     `json:"field,omitempty"`
	Summary  string                     `json:"summary"`
	Detail   string                     `json:"detail,omitempty"`
}

type LifecyclePreflightReport struct {
	Operation LifecycleOperation          `json:"operation"`
	Status    LifecyclePreflightSeverity  `json:"status"`
	Fatal     bool                        `json:"fatal"`
	Blocking  bool                        `json:"blocking"`
	Findings  []LifecyclePreflightFinding `json:"findings"`
	Runtime   *LifecycleRuntimeSnapshot   `json:"runtime,omitempty"`
	Upgrade   *LifecycleUpgradeAssessment `json:"upgrade,omitempty"`
	Restore   *LifecycleRestoreAssessment `json:"restore,omitempty"`
}

type LifecyclePreflightOptions struct {
	Operation       LifecycleOperation
	RuntimeSnapshot *LifecycleRuntimeSnapshot
	FileConfig      *config.FileConfig
	ConfigPath      string
	TargetRelease   string
	RestoreRoot     string
}

func EvaluateLifecyclePreflight(ctx context.Context, cfg config.AppConfig, opts LifecyclePreflightOptions) LifecyclePreflightReport {
	_ = ctx

	report := LifecyclePreflightReport{
		Operation: normalizeLifecycleOperation(opts.Operation),
		Status:    LifecyclePreflightSeverityOK,
		Findings:  []LifecyclePreflightFinding{},
		Runtime:   opts.RuntimeSnapshot,
	}

	add := func(f LifecyclePreflightFinding) {
		if strings.TrimSpace(f.Code) == "" {
			f.Code = "lifecycle.preflight.unknown"
		}
		if strings.TrimSpace(f.Summary) == "" {
			f.Summary = f.Code
		}
		report.Findings = append(report.Findings, f)
		if lifecycleSeverityRank(f.Severity) > lifecycleSeverityRank(report.Status) {
			report.Status = f.Severity
		}
		switch f.Severity {
		case LifecyclePreflightSeverityFatal:
			report.Fatal = true
			report.Blocking = true
		case LifecyclePreflightSeverityBlock:
			report.Blocking = true
		}
	}

	if err := evaluateLifecycleRuntimeChecks(cfg, add); err != nil {
		add(LifecyclePreflightFinding{
			Code:     "lifecycle.preflight.internal_error",
			Severity: LifecyclePreflightSeverityFatal,
			Contract: "lifecycle_preflight",
			Summary:  "lifecycle preflight evaluation failed",
			Detail:   err.Error(),
		})
	}
	if opts.RuntimeSnapshot != nil {
		for _, finding := range opts.RuntimeSnapshot.Drift.Findings {
			add(LifecyclePreflightFinding{
				Code:     finding.Code,
				Severity: lifecycleSeverityFromRuntimeDriftClass(finding.Class),
				Contract: "runtime_truth",
				Field:    finding.Field,
				Summary:  finding.Summary,
				Detail:   finding.Detail,
			})
		}
	}
	if report.Operation == LifecycleOperationUpgrade {
		report.Upgrade = evaluateLifecycleUpgradeAssessment(cfg, opts, add)
	}
	if report.Operation == LifecycleOperationRestore {
		report.Restore = evaluateLifecycleRestoreAssessment(cfg, opts, add)
	}

	return report
}

func (r LifecyclePreflightReport) StartupError() error {
	if !r.Fatal {
		return nil
	}
	return fmt.Errorf("startup preflight failed: %s", r.Summary(LifecyclePreflightSeverityFatal))
}

func (r LifecyclePreflightReport) Summary(min LifecyclePreflightSeverity) string {
	parts := make([]string, 0, len(r.Findings))
	for _, finding := range r.Findings {
		if lifecycleSeverityRank(finding.Severity) < lifecycleSeverityRank(min) {
			continue
		}
		parts = append(parts, summarizeLifecycleFinding(finding))
	}
	return strings.Join(parts, "; ")
}

func normalizeLifecycleOperation(operation LifecycleOperation) LifecycleOperation {
	switch strings.ToLower(strings.TrimSpace(string(operation))) {
	case "", string(LifecycleOperationStartup):
		return LifecycleOperationStartup
	case string(LifecycleOperationInstall):
		return LifecycleOperationInstall
	case string(LifecycleOperationUpgrade):
		return LifecycleOperationUpgrade
	case string(LifecycleOperationRestore):
		return LifecycleOperationRestore
	case string(LifecycleOperationRollback):
		return LifecycleOperationRollback
	default:
		return operation
	}
}

func lifecycleSeverityRank(severity LifecyclePreflightSeverity) int {
	switch severity {
	case LifecyclePreflightSeverityWarn:
		return 1
	case LifecyclePreflightSeverityBlock:
		return 2
	case LifecyclePreflightSeverityFatal:
		return 3
	default:
		return 0
	}
}

func summarizeLifecycleFinding(finding LifecyclePreflightFinding) string {
	parts := make([]string, 0, 3)
	if contract := strings.TrimSpace(strings.ReplaceAll(finding.Contract, "_", " ")); contract != "" {
		parts = append(parts, contract)
	}
	if field := strings.TrimSpace(finding.Field); field != "" {
		parts = append(parts, field)
	}
	summary := strings.TrimSpace(finding.Summary)
	detail := strings.TrimSpace(finding.Detail)
	switch {
	case summary != "" && detail != "" && summary != detail:
		parts = append(parts, summary+" ("+detail+")")
	case summary != "":
		parts = append(parts, summary)
	case detail != "":
		parts = append(parts, detail)
	}
	if len(parts) == 0 {
		return finding.Code
	}
	return strings.Join(parts, ": ")
}

func evaluateLifecycleRuntimeChecks(cfg config.AppConfig, add func(LifecyclePreflightFinding)) error {
	if err := addWritableDirFinding(add, "runtime", "runtime.data_dir.not_writable", "DataDir", "data directory must be writable", cfg.DataDir, false); err != nil {
		return err
	}

	if cfg.APIListenAddr != "" {
		_, port, err := net.SplitHostPort(cfg.APIListenAddr)
		if err != nil {
			add(LifecyclePreflightFinding{
				Code:     "runtime.api_listen_addr.invalid",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "runtime",
				Field:    "APIListenAddr",
				Summary:  "API listen address is invalid",
				Detail:   err.Error(),
			})
		} else {
			portNum, err := strconv.Atoi(port)
			if err != nil || portNum < 0 || portNum > 65535 {
				add(LifecyclePreflightFinding{
					Code:     "runtime.api_listen_port.invalid",
					Severity: LifecyclePreflightSeverityFatal,
					Contract: "runtime",
					Field:    "APIListenAddr",
					Summary:  "API listen port is invalid",
					Detail:   fmt.Sprintf("invalid port %q in %q", port, cfg.APIListenAddr),
				})
			}
		}
	}

	if strings.TrimSpace(cfg.Enigma2.BaseURL) != "" {
		u, err := url.Parse(cfg.Enigma2.BaseURL)
		if err != nil {
			add(LifecyclePreflightFinding{
				Code:     "runtime.enigma2_base_url.invalid",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "runtime",
				Field:    "Enigma2.BaseURL",
				Summary:  "Enigma2 base URL is invalid",
				Detail:   err.Error(),
			})
		} else if u.Scheme != "http" && u.Scheme != "https" {
			add(LifecyclePreflightFinding{
				Code:     "runtime.enigma2_base_url.scheme_invalid",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "runtime",
				Field:    "Enigma2.BaseURL",
				Summary:  "Enigma2 base URL must use http or https",
				Detail:   fmt.Sprintf("unsupported scheme %q", u.Scheme),
			})
		}
	}

	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		if cfg.TLSCert == "" || cfg.TLSKey == "" {
			add(LifecyclePreflightFinding{
				Code:     "runtime.tls.partial",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "runtime",
				Field:    "TLS",
				Summary:  "TLS requires both certificate and key",
				Detail:   "set both TLSCert and TLSKey or neither",
			})
		} else {
			if err := checkFileReadable(cfg.TLSCert); err != nil {
				add(LifecyclePreflightFinding{
					Code:     "runtime.tls.cert_unreadable",
					Severity: LifecyclePreflightSeverityFatal,
					Contract: "runtime",
					Field:    "TLSCert",
					Summary:  "TLS certificate is not readable",
					Detail:   err.Error(),
				})
			}
			if err := checkFileReadable(cfg.TLSKey); err != nil {
				add(LifecyclePreflightFinding{
					Code:     "runtime.tls.key_unreadable",
					Severity: LifecyclePreflightSeverityFatal,
					Contract: "runtime",
					Field:    "TLSKey",
					Summary:  "TLS key is not readable",
					Detail:   err.Error(),
				})
			}
		}
	}

	for id, path := range cfg.RecordingRoots {
		field := fmt.Sprintf("RecordingRoots.%s", id)
		if strings.TrimSpace(path) == "" {
			add(LifecyclePreflightFinding{
				Code:     "runtime.recording_root.empty",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "runtime",
				Field:    field,
				Summary:  "recording root path cannot be empty",
			})
			continue
		}
		if !filepath.IsAbs(path) {
			add(LifecyclePreflightFinding{
				Code:     "runtime.recording_root.not_absolute",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "runtime",
				Field:    field,
				Summary:  "recording root must be an absolute path",
				Detail:   path,
			})
			continue
		}
		if err := addWritableDirFinding(add, "runtime", "runtime.recording_root.not_writable", field, "recording root must be writable", path, true); err != nil {
			return err
		}
	}

	storeBackend := strings.ToLower(strings.TrimSpace(cfg.Store.Backend))
	if storeBackend != "memory" {
		if err := addWritableDirFinding(add, "runtime", "runtime.store_path.not_writable", "Store.Path", "store path must be writable", cfg.Store.Path, true); err != nil {
			return err
		}
	}

	if cfg.Engine.Enabled {
		if err := addWritableDirFinding(add, "runtime", "runtime.hls_root.not_writable", "HLS.Root", "HLS root must be writable", cfg.HLS.Root, true); err != nil {
			return err
		}

		if !strings.EqualFold(cfg.Engine.Mode, "virtual") {
			ffmpegBin := strings.TrimSpace(cfg.FFmpeg.Bin)
			if ffmpegBin == "" {
				ffmpegBin = "ffmpeg"
			}
			if _, err := exec.LookPath(ffmpegBin); err != nil {
				add(LifecyclePreflightFinding{
					Code:     "runtime.ffmpeg.not_found",
					Severity: LifecyclePreflightSeverityFatal,
					Contract: "runtime",
					Field:    "FFmpeg.Bin",
					Summary:  "ffmpeg binary is not available",
					Detail:   err.Error(),
				})
			}
		}

		if strings.EqualFold(cfg.Store.Backend, "memory") {
			add(LifecyclePreflightFinding{
				Code:     "runtime.store_backend.memory",
				Severity: LifecyclePreflightSeverityWarn,
				Contract: "runtime",
				Field:    "Store.Backend",
				Summary:  "engine uses in-memory store",
				Detail:   "sessions and device state will not survive restart while Store.Backend=memory",
			})
		}

		tempDir := filepath.Clean(os.TempDir())
		dataDir := filepath.Clean(cfg.DataDir)
		if tempDir != "." && (dataDir == tempDir || strings.HasPrefix(dataDir, tempDir+string(filepath.Separator))) {
			add(LifecyclePreflightFinding{
				Code:     "runtime.data_dir.under_temp",
				Severity: LifecyclePreflightSeverityWarn,
				Contract: "runtime",
				Field:    "DataDir",
				Summary:  "data directory is under a temp path",
				Detail:   "cached data and product state may be lost on reboot",
			})
		}

		storePath := filepath.Clean(cfg.Store.Path)
		if storeBackend != "memory" && tempDir != "." && (storePath == tempDir || strings.HasPrefix(storePath, tempDir+string(filepath.Separator))) {
			add(LifecyclePreflightFinding{
				Code:     "runtime.store_path.under_temp",
				Severity: LifecyclePreflightSeverityWarn,
				Contract: "runtime",
				Field:    "Store.Path",
				Summary:  "store path is under a temp path",
				Detail:   "durable state may be lost on reboot",
			})
		}
	}

	connectivityReport, err := config.BuildConnectivityContract(cfg)
	if err != nil {
		add(LifecyclePreflightFinding{
			Code:     "public_deployment_contract.evaluation_failed",
			Severity: LifecyclePreflightSeverityFatal,
			Contract: "public_deployment_contract",
			Field:    "Connectivity",
			Summary:  "public deployment contract evaluation failed",
			Detail:   err.Error(),
		})
	} else {
		for _, finding := range connectivityReport.Findings {
			severity := lifecycleSeverityFromConnectivityFinding(finding)
			if severity == LifecyclePreflightSeverityOK {
				continue
			}

			detail := strings.TrimSpace(finding.Detail)
			if detail == "" {
				detail = strings.TrimSpace(finding.Summary)
			}

			add(LifecyclePreflightFinding{
				Code:     finding.Code,
				Severity: severity,
				Contract: "public_deployment_contract",
				Field:    finding.Field,
				Summary:  finding.Summary,
				Detail:   detail,
			})
		}
	}

	for _, finding := range config.PublicExposureSecurityFindings(cfg) {
		add(LifecyclePreflightFinding{
			Code:     "public_exposure_security_contract.rejected",
			Severity: LifecyclePreflightSeverityFatal,
			Contract: "public_exposure_security_contract",
			Field:    finding.Field,
			Summary:  "public exposure security contract rejected configuration",
			Detail:   finding.Message,
		})
	}

	return nil
}

func addWritableDirFinding(add func(LifecyclePreflightFinding), contract, code, field, summary, path string, createIfMissing bool) error {
	_, err := probeWritableDir(path, createIfMissing)
	if err == nil {
		return nil
	}

	add(LifecyclePreflightFinding{
		Code:     code,
		Severity: LifecyclePreflightSeverityFatal,
		Contract: contract,
		Field:    field,
		Summary:  summary,
		Detail:   err.Error(),
	})
	return nil
}

func lifecycleSeverityFromConnectivityFinding(finding connectivitydomain.ContractFinding) LifecyclePreflightSeverity {
	switch finding.Severity {
	case connectivitydomain.FindingSeverityWarn:
		return LifecyclePreflightSeverityWarn
	case connectivitydomain.FindingSeverityFatal:
		return LifecyclePreflightSeverityFatal
	case connectivitydomain.FindingSeverityDegraded:
		if slices.Contains(finding.Scopes, connectivitydomain.FindingScopeReadiness) ||
			slices.Contains(finding.Scopes, connectivitydomain.FindingScopePairing) ||
			slices.Contains(finding.Scopes, connectivitydomain.FindingScopeWeb) {
			return LifecyclePreflightSeverityBlock
		}
		return LifecyclePreflightSeverityWarn
	default:
		return LifecyclePreflightSeverityOK
	}
}

func lifecycleSeverityFromRuntimeDriftClass(class LifecycleRuntimeDriftClass) LifecyclePreflightSeverity {
	switch class {
	case LifecycleRuntimeDriftClassDriftedButAllowed:
		return LifecyclePreflightSeverityWarn
	case LifecycleRuntimeDriftClassUnsupported, LifecycleRuntimeDriftClassBlocking:
		return LifecyclePreflightSeverityBlock
	default:
		return LifecyclePreflightSeverityOK
	}
}
