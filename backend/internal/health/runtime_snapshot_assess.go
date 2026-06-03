package health

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

func assessLifecycleRuntimeSnapshot(cfg config.AppConfig, snapshot LifecycleRuntimeSnapshot, envValues map[string]string) LifecycleRuntimeAssessment {
	assessment := LifecycleRuntimeAssessment{
		Class:    LifecycleRuntimeDriftClassSupported,
		Findings: []LifecycleRuntimeDriftFinding{},
	}
	add := func(f LifecycleRuntimeDriftFinding) {
		if strings.TrimSpace(f.Code) == "" {
			f.Code = "runtime_snapshot.unknown"
		}
		if strings.TrimSpace(f.Summary) == "" {
			f.Summary = f.Code
		}
		assessment.Findings = append(assessment.Findings, f)
		if runtimeDriftRank(f.Class) > runtimeDriftRank(assessment.Class) {
			assessment.Class = f.Class
		}
	}

	assessRuntimeUnitDrift(snapshot, add)
	assessRuntimeComposeDrift(cfg, snapshot, add)
	assessRuntimeEnvDrift(snapshot, add)
	assessRuntimeStateDrift(snapshot, add)

	return assessment
}

// assessRuntimeUnitDrift evaluates the systemd unit portion of the runtime
// snapshot, emitting findings for missing installed/canonical units and for
// drift between the installed unit, the canonical host copy, and the repo
// deploy bundle.
func assessRuntimeUnitDrift(snapshot LifecycleRuntimeSnapshot, add func(LifecycleRuntimeDriftFinding)) {
	if !snapshot.Unit.Installed.Exists {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.unit.installed_missing",
			Class:   LifecycleRuntimeDriftClassBlocking,
			Field:   "systemd.unit",
			Summary: "installed systemd unit is missing",
			Detail:  snapshot.Unit.Installed.Path,
		})
	}
	if !snapshot.Unit.Canonical.Exists {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.unit.canonical_missing",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "systemd.unit",
			Summary: "canonical host unit copy is missing",
			Detail:  snapshot.Unit.Canonical.Path,
		})
	}
	if snapshot.Unit.Installed.Exists && snapshot.Unit.Canonical.Exists && snapshot.Unit.Installed.SHA256 != "" &&
		snapshot.Unit.Canonical.SHA256 != "" && snapshot.Unit.Installed.SHA256 != snapshot.Unit.Canonical.SHA256 {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.unit.drifted_from_canonical",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "systemd.unit",
			Summary: "installed systemd unit drifts from canonical host copy",
			Detail:  snapshot.Unit.Installed.Path,
		})
	}
	if snapshot.Unit.RepoBundle != nil && snapshot.Unit.RepoBundle.Exists && snapshot.Unit.Canonical.Exists &&
		snapshot.Unit.RepoBundle.SHA256 != "" && snapshot.Unit.Canonical.SHA256 != "" &&
		snapshot.Unit.RepoBundle.SHA256 != snapshot.Unit.Canonical.SHA256 {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.unit.host_vs_repo_drift",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "systemd.unit",
			Summary: "canonical host unit copy drifts from repo deploy bundle",
			Detail:  snapshot.Unit.Canonical.Path,
		})
	}
}

// assessRuntimeComposeDrift evaluates the docker compose portion of the runtime
// snapshot, covering the base file, service/image presence, image drift,
// env-file inclusion, stack selection overrides, volume coverage for configured
// paths, and the XG2G_DATA environment mapping.
func assessRuntimeComposeDrift(cfg config.AppConfig, snapshot LifecycleRuntimeSnapshot, add func(LifecycleRuntimeDriftFinding)) {
	if !snapshot.Compose.BaseFile.Exists {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.base_missing",
			Class:   LifecycleRuntimeDriftClassBlocking,
			Field:   "compose.base",
			Summary: "base compose file is missing",
			Detail:  snapshot.Compose.BaseFile.Path,
		})
	}
	if strings.TrimSpace(snapshot.Compose.ServiceName) == "" {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.service_missing",
			Class:   LifecycleRuntimeDriftClassBlocking,
			Field:   "compose.service",
			Summary: "compose service xg2g is missing",
			Detail:  snapshot.Compose.BaseFile.Path,
		})
	}
	if strings.TrimSpace(snapshot.Compose.Image) == "" {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.image_missing",
			Class:   LifecycleRuntimeDriftClassBlocking,
			Field:   "compose.image",
			Summary: "compose service image is missing",
			Detail:  snapshot.Compose.BaseFile.Path,
		})
	}
	if snapshot.Compose.RepoImage != "" && snapshot.Compose.Image != "" && snapshot.Compose.Image != snapshot.Compose.RepoImage {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.image_drift",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "compose.image",
			Summary: "live compose image drifts from repo deploy bundle",
			Detail:  fmt.Sprintf("expected %s, got %s", snapshot.Compose.RepoImage, snapshot.Compose.Image),
		})
	}
	if !containsString(snapshot.Compose.EnvFiles, "/etc/xg2g/xg2g.env") {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.env_file_missing",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "compose.env_file",
			Summary: "compose service must load /etc/xg2g/xg2g.env",
			Detail:  snapshot.Compose.BaseFile.Path,
		})
	}
	defaultComposeFiles := selectedComposeFiles(snapshot.InstallRoot, LifecycleRuntimeComposeSnapshot{
		BaseFile:      snapshot.Compose.BaseFile,
		OptionalFiles: snapshot.Compose.OptionalFiles,
	})
	if len(snapshot.Compose.ConfiguredFiles) > 0 && !sameStrings(defaultComposeFiles, snapshot.Compose.SelectedFiles) {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.stack_override",
			Class:   LifecycleRuntimeDriftClassDriftedButAllowed,
			Field:   "env.COMPOSE_FILE",
			Summary: "runtime compose stack overrides canonical base selection",
			Detail:  strings.Join(snapshot.Compose.SelectedFiles, ", "),
		})
	}
	if dataDir := strings.TrimSpace(cfg.DataDir); dataDir != "" && !pathCoveredByVolumes(dataDir, snapshot.Compose.Volumes) {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.data_volume_missing",
			Class:   LifecycleRuntimeDriftClassBlocking,
			Field:   "compose.volumes",
			Summary: "compose volumes do not cover configured dataDir",
			Detail:  dataDir,
		})
	}
	if storePath := strings.TrimSpace(cfg.Store.Path); storePath != "" && !strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") && !pathCoveredByVolumes(storePath, snapshot.Compose.Volumes) {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.store_volume_missing",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "compose.volumes",
			Summary: "compose volumes do not cover configured store path",
			Detail:  storePath,
		})
	}
	if hlsRoot := strings.TrimSpace(cfg.HLS.Root); hlsRoot != "" && cfg.Engine.Enabled && !pathCoveredByVolumes(hlsRoot, snapshot.Compose.Volumes) {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.hls_volume_missing",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "compose.volumes",
			Summary: "compose volumes do not cover configured HLS root",
			Detail:  hlsRoot,
		})
	}
	if dataEnv := strings.TrimSpace(snapshot.Compose.Environment["XG2G_DATA"]); dataEnv != "" && strings.TrimSpace(cfg.DataDir) != "" && dataEnv != cfg.DataDir {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.compose.data_dir_mismatch",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "compose.environment",
			Summary: "compose XG2G_DATA does not match effective dataDir",
			Detail:  fmt.Sprintf("compose=%s config=%s", dataEnv, cfg.DataDir),
		})
	}
}

// assessRuntimeEnvDrift evaluates the runtime env file: its existence, secure
// file mode, presence of required keys, and that every selected compose file
// referenced via COMPOSE_FILE actually exists on disk.
func assessRuntimeEnvDrift(snapshot LifecycleRuntimeSnapshot, add func(LifecycleRuntimeDriftFinding)) {
	if !snapshot.Env.File.Exists {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.env.missing",
			Class:   LifecycleRuntimeDriftClassBlocking,
			Field:   "env.file",
			Summary: "runtime env file is missing",
			Detail:  snapshot.Env.File.Path,
		})
	}
	if snapshot.Env.File.Exists && snapshot.Env.File.Mode != "" && snapshot.Env.File.Mode != "600" {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.env.insecure_mode",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "env.file",
			Summary: "runtime env file must be mode 600",
			Detail:  fmt.Sprintf("got %s", snapshot.Env.File.Mode),
		})
	}
	if snapshot.Env.File.Exists {
		requiredMissing := append([]string(nil), snapshot.Env.MissingRequired...)
		if len(requiredMissing) > 0 {
			add(LifecycleRuntimeDriftFinding{
				Code:    "runtime_snapshot.env.required_missing",
				Class:   LifecycleRuntimeDriftClassBlocking,
				Field:   "env.keys",
				Summary: "runtime env file is missing required keys",
				Detail:  strings.Join(requiredMissing, ", "),
			})
		}
		for _, composeFile := range snapshot.Compose.SelectedFiles {
			if _, err := os.Stat(composeFile); err != nil {
				add(LifecycleRuntimeDriftFinding{
					Code:    "runtime_snapshot.env.compose_file_missing",
					Class:   LifecycleRuntimeDriftClassUnsupported,
					Field:   "env.COMPOSE_FILE",
					Summary: "COMPOSE_FILE references a missing compose path",
					Detail:  composeFile,
				})
			}
		}
	}
}

// assessRuntimeStateDrift evaluates the persisted drift-state snapshot, flagging
// an unsupported drift_state.json schema version.
func assessRuntimeStateDrift(snapshot LifecycleRuntimeSnapshot, add func(LifecycleRuntimeDriftFinding)) {
	if snapshot.State.DriftStateVersion != nil && *snapshot.State.DriftStateVersion != 1 {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.state.drift_version_unknown",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "state.driftStateVersion",
			Summary: "drift_state.json version is unsupported",
			Detail:  fmt.Sprintf("got %d", *snapshot.State.DriftStateVersion),
		})
	}
}

func runtimeDriftRank(class LifecycleRuntimeDriftClass) int {
	switch class {
	case LifecycleRuntimeDriftClassDriftedButAllowed:
		return 1
	case LifecycleRuntimeDriftClassUnsupported:
		return 2
	case LifecycleRuntimeDriftClassBlocking:
		return 3
	default:
		return 0
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func pathCoveredByVolumes(path string, volumes []LifecycleRuntimeVolumeSnapshot) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	for _, volume := range volumes {
		target := filepath.Clean(strings.TrimSpace(volume.Target))
		if target == "" {
			continue
		}
		if path == target || strings.HasPrefix(path, target+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func missingRequiredEnvKeys(values map[string]string) []string {
	var missing []string
	if strings.TrimSpace(values["XG2G_E2_HOST"]) == "" && strings.TrimSpace(values["XG2G_OWI_BASE"]) == "" {
		missing = append(missing, "XG2G_E2_HOST|XG2G_OWI_BASE")
	}
	if strings.TrimSpace(values["XG2G_API_TOKEN"]) == "" && strings.TrimSpace(values["XG2G_API_TOKENS"]) == "" {
		missing = append(missing, "XG2G_API_TOKEN|XG2G_API_TOKENS")
	}
	if strings.TrimSpace(values["XG2G_DECISION_SECRET"]) == "" && strings.TrimSpace(values["XG2G_PLAYBACK_DECISION_SECRET"]) == "" {
		missing = append(missing, "XG2G_DECISION_SECRET|XG2G_PLAYBACK_DECISION_SECRET")
	}
	return missing
}
