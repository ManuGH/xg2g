// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package health

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	appversion "github.com/ManuGH/xg2g/internal/version"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type LifecycleRuntimeDriftClass string

const (
	LifecycleRuntimeDriftClassSupported         LifecycleRuntimeDriftClass = "supported"
	LifecycleRuntimeDriftClassDriftedButAllowed LifecycleRuntimeDriftClass = "drifted_but_allowed"
	LifecycleRuntimeDriftClassUnsupported       LifecycleRuntimeDriftClass = "unsupported"
	LifecycleRuntimeDriftClassBlocking          LifecycleRuntimeDriftClass = "blocking"
)

type LifecycleRuntimeDriftFinding struct {
	Code    string                     `json:"code"`
	Class   LifecycleRuntimeDriftClass `json:"class"`
	Field   string                     `json:"field,omitempty"`
	Summary string                     `json:"summary"`
	Detail  string                     `json:"detail,omitempty"`
}

type LifecycleRuntimeAssessment struct {
	Class    LifecycleRuntimeDriftClass     `json:"class"`
	Findings []LifecycleRuntimeDriftFinding `json:"findings"`
}

type LifecycleRuntimeFileSnapshot struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Mode   string `json:"mode,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type LifecycleRuntimeVolumeSnapshot struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode,omitempty"`
}

type LifecycleRuntimeComposeSnapshot struct {
	BaseFile        LifecycleRuntimeFileSnapshot     `json:"baseFile"`
	RepoBaseFile    *LifecycleRuntimeFileSnapshot    `json:"repoBaseFile,omitempty"`
	OptionalFiles   []LifecycleRuntimeFileSnapshot   `json:"optionalFiles,omitempty"`
	ConfiguredFiles []string                         `json:"configuredFiles,omitempty"`
	SelectedFiles   []string                         `json:"selectedFiles,omitempty"`
	ServiceName     string                           `json:"serviceName,omitempty"`
	Image           string                           `json:"image,omitempty"`
	RepoImage       string                           `json:"repoImage,omitempty"`
	EnvFiles        []string                         `json:"envFiles,omitempty"`
	Environment     map[string]string                `json:"environment,omitempty"`
	Volumes         []LifecycleRuntimeVolumeSnapshot `json:"volumes,omitempty"`
}

type LifecycleRuntimeEnvSnapshot struct {
	File               LifecycleRuntimeFileSnapshot `json:"file"`
	PresentKeys        []string                     `json:"presentKeys,omitempty"`
	MissingRequired    []string                     `json:"missingRequired,omitempty"`
	SelectedCompose    []string                     `json:"selectedCompose,omitempty"`
	LegacyReceiverKeys []string                     `json:"legacyReceiverKeys,omitempty"`
}

type LifecycleRuntimeUnitSnapshot struct {
	Installed  LifecycleRuntimeFileSnapshot  `json:"installed"`
	Canonical  LifecycleRuntimeFileSnapshot  `json:"canonical"`
	RepoBundle *LifecycleRuntimeFileSnapshot `json:"repoBundle,omitempty"`
}

type LifecycleRuntimePathSnapshot struct {
	DataDir   string `json:"dataDir"`
	StorePath string `json:"storePath,omitempty"`
	HLSRoot   string `json:"hlsRoot,omitempty"`
}

type LifecycleRuntimeSQLiteSchema struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	UserVersion *int   `json:"userVersion,omitempty"`
}

type LifecycleRuntimeStateSnapshot struct {
	ConfigVersion     string                         `json:"configVersion,omitempty"`
	StoreBackend      string                         `json:"storeBackend,omitempty"`
	DriftStateVersion *int                           `json:"driftStateVersion,omitempty"`
	SQLiteSchemas     []LifecycleRuntimeSQLiteSchema `json:"sqliteSchemas,omitempty"`
}

type LifecycleRuntimeBuildSnapshot struct {
	Release string `json:"release,omitempty"`
	Digest  string `json:"digest,omitempty"`
	Go      string `json:"go,omitempty"`
}

type LifecycleRuntimeSnapshot struct {
	InstallRoot string                          `json:"installRoot,omitempty"`
	RepoRoot    string                          `json:"repoRoot,omitempty"`
	Build       LifecycleRuntimeBuildSnapshot   `json:"build"`
	Unit        LifecycleRuntimeUnitSnapshot    `json:"unit"`
	Compose     LifecycleRuntimeComposeSnapshot `json:"compose"`
	Env         LifecycleRuntimeEnvSnapshot     `json:"env"`
	Paths       LifecycleRuntimePathSnapshot    `json:"paths"`
	State       LifecycleRuntimeStateSnapshot   `json:"state"`
	Drift       LifecycleRuntimeAssessment      `json:"drift"`
}

type LifecycleRuntimeSnapshotOptions struct {
	InstallRoot string
	RepoRoot    string
}

func CollectLifecycleRuntimeSnapshot(ctx context.Context, cfg config.AppConfig, opts LifecycleRuntimeSnapshotOptions) LifecycleRuntimeSnapshot {
	_ = ctx

	installRoot := normalizeInstallRoot(opts.InstallRoot)
	repoRoot := strings.TrimSpace(opts.RepoRoot)

	snapshot := LifecycleRuntimeSnapshot{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
		Build: LifecycleRuntimeBuildSnapshot{
			Release: appversion.Version,
			Digest:  appversion.Commit,
			Go:      runtime.Version(),
		},
		Unit: LifecycleRuntimeUnitSnapshot{
			Installed: snapshotFile(runtimeJoin(installRoot, "/etc/systemd/system/xg2g.service")),
			Canonical: snapshotFile(runtimeJoin(installRoot, "/srv/xg2g/docs/ops/xg2g.service")),
		},
		Compose: LifecycleRuntimeComposeSnapshot{
			BaseFile: snapshotFile(runtimeJoin(installRoot, "/srv/xg2g/docker-compose.yml")),
		},
		Env: LifecycleRuntimeEnvSnapshot{
			File: snapshotFile(runtimeJoin(installRoot, "/etc/xg2g/xg2g.env")),
		},
		Paths: LifecycleRuntimePathSnapshot{
			DataDir:   cfg.DataDir,
			StorePath: cfg.Store.Path,
			HLSRoot:   cfg.HLS.Root,
		},
		State: LifecycleRuntimeStateSnapshot{
			ConfigVersion: cfg.ConfigVersion,
			StoreBackend:  strings.ToLower(strings.TrimSpace(cfg.Store.Backend)),
		},
	}

	if repoRoot != "" {
		repoUnit := snapshotFile(filepath.Join(repoRoot, "deploy", "xg2g.service"))
		snapshot.Unit.RepoBundle = &repoUnit
		repoCompose := snapshotFile(filepath.Join(repoRoot, "deploy", "docker-compose.yml"))
		snapshot.Compose.RepoBaseFile = &repoCompose
	}
	snapshot.Compose.OptionalFiles = collectOptionalComposeFiles(installRoot)

	envValues := map[string]string{}
	if snapshot.Env.File.Exists {
		envValues = parseEnvFile(snapshot.Env.File.Path)
		snapshot.Env.PresentKeys = sortedKeys(envValues)
		if composeFiles := strings.TrimSpace(envValues["COMPOSE_FILE"]); composeFiles != "" {
			snapshot.Env.SelectedCompose = splitComposeFiles(composeFiles)
			snapshot.Compose.ConfiguredFiles = append([]string(nil), snapshot.Env.SelectedCompose...)
		}
		if _, ok := envValues["XG2G_OWI_BASE"]; ok {
			snapshot.Env.LegacyReceiverKeys = append(snapshot.Env.LegacyReceiverKeys, "XG2G_OWI_BASE")
		}
		snapshot.Env.MissingRequired = missingRequiredEnvKeys(envValues)
		sort.Strings(snapshot.Env.LegacyReceiverKeys)
	}
	snapshot.Compose.SelectedFiles = selectedComposeFiles(installRoot, snapshot.Compose)

	composeSpec, err := readComposeSpec(snapshot.Compose.BaseFile.Path)
	if err == nil {
		svc, ok := composeSpec.Services["xg2g"]
		if ok {
			snapshot.Compose.ServiceName = "xg2g"
			snapshot.Compose.Image = strings.TrimSpace(svc.Image)
			snapshot.Compose.EnvFiles = append([]string(nil), svc.EnvFile...)
			snapshot.Compose.Environment = map[string]string{}
			for k, v := range svc.Environment {
				snapshot.Compose.Environment[k] = v
			}
			snapshot.Compose.Volumes = append([]LifecycleRuntimeVolumeSnapshot(nil), svc.Volumes...)
		}
	}
	if snapshot.Compose.RepoBaseFile != nil && snapshot.Compose.RepoBaseFile.Exists {
		repoSpec, repoErr := readComposeSpec(snapshot.Compose.RepoBaseFile.Path)
		if repoErr == nil {
			if repoSvc, ok := repoSpec.Services["xg2g"]; ok {
				snapshot.Compose.RepoImage = strings.TrimSpace(repoSvc.Image)
			}
		}
	}

	driftStatePath := runtimeJoin(installRoot, filepath.Join(cfg.DataDir, "drift_state.json"))
	if version, ok := readJSONVersion(driftStatePath); ok {
		snapshot.State.DriftStateVersion = &version
	}
	snapshot.State.SQLiteSchemas = collectSQLiteSchemas(installRoot, cfg)
	snapshot.Drift = assessLifecycleRuntimeSnapshot(cfg, snapshot, envValues)
	return snapshot
}

func normalizeInstallRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "/"
	}
	return filepath.Clean(root)
}

func runtimeJoin(root, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if root == "" || root == "/" {
		if filepath.IsAbs(target) {
			return filepath.Clean(target)
		}
		return filepath.Clean("/" + target)
	}
	target = strings.TrimPrefix(filepath.Clean(target), string(filepath.Separator))
	return filepath.Join(root, target)
}

func snapshotFile(path string) LifecycleRuntimeFileSnapshot {
	path = strings.TrimSpace(path)
	snapshot := LifecycleRuntimeFileSnapshot{Path: path}
	if path == "" {
		return snapshot
	}
	info, err := os.Stat(path)
	if err != nil {
		return snapshot
	}
	snapshot.Exists = true
	snapshot.Mode = fmt.Sprintf("%03o", info.Mode().Perm())
	if info.Mode().IsRegular() {
		if data, readErr := os.ReadFile(path); readErr == nil {
			sum := sha256.Sum256(data)
			snapshot.SHA256 = hex.EncodeToString(sum[:])
		}
	}
	return snapshot
}

func parseEnvFile(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return out
	}
	lines := strings.Split(string(data), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if key != "" {
			out[key] = val
		}
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func splitComposeFiles(raw string) []string {
	parts := strings.Split(raw, ":")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func collectOptionalComposeFiles(installRoot string) []LifecycleRuntimeFileSnapshot {
	paths := []string{
		runtimeJoin(installRoot, "/srv/xg2g/docker-compose.gpu.yml"),
		runtimeJoin(installRoot, "/srv/xg2g/docker-compose.nvidia.yml"),
	}
	out := make([]LifecycleRuntimeFileSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot := snapshotFile(path)
		if snapshot.Exists {
			out = append(out, snapshot)
		}
	}
	return out
}

func selectedComposeFiles(installRoot string, compose LifecycleRuntimeComposeSnapshot) []string {
	if len(compose.ConfiguredFiles) > 0 {
		out := make([]string, 0, len(compose.ConfiguredFiles))
		for _, file := range compose.ConfiguredFiles {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			if filepath.IsAbs(file) {
				out = append(out, filepath.Clean(runtimeJoin(installRoot, file)))
				continue
			}
			out = append(out, runtimeJoin(installRoot, filepath.Join("/srv/xg2g", file)))
		}
		return out
	}

	out := []string{}
	if strings.TrimSpace(compose.BaseFile.Path) != "" {
		out = append(out, compose.BaseFile.Path)
	}
	for _, optional := range compose.OptionalFiles {
		if optional.Exists && strings.TrimSpace(optional.Path) != "" {
			out = append(out, optional.Path)
		}
	}
	return out
}

type composeFileSpec struct {
	Services map[string]composeServiceSpec `yaml:"services"`
}

type composeServiceSpec struct {
	Image       string
	EnvFile     []string
	Environment map[string]string
	Volumes     []LifecycleRuntimeVolumeSnapshot
}

func readComposeSpec(path string) (composeFileSpec, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return composeFileSpec{}, err
	}
	var raw struct {
		Services map[string]struct {
			Image       string      `yaml:"image"`
			EnvFile     interface{} `yaml:"env_file"`
			Environment interface{} `yaml:"environment"`
			Volumes     interface{} `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return composeFileSpec{}, err
	}

	spec := composeFileSpec{Services: map[string]composeServiceSpec{}}
	for name, svc := range raw.Services {
		spec.Services[name] = composeServiceSpec{
			Image:       strings.TrimSpace(svc.Image),
			EnvFile:     normalizeComposeStringList(svc.EnvFile),
			Environment: normalizeComposeEnvironment(svc.Environment),
			Volumes:     normalizeComposeVolumes(svc.Volumes),
		}
	}
	return spec, nil
}

func normalizeComposeStringList(raw interface{}) []string {
	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return []string{value}
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func normalizeComposeEnvironment(raw interface{}) map[string]string {
	out := map[string]string{}
	switch value := raw.(type) {
	case nil:
		return out
	case map[string]interface{}:
		for key, rawValue := range value {
			out[strings.TrimSpace(key)] = strings.TrimSpace(fmt.Sprint(rawValue))
		}
	case []interface{}:
		for _, item := range value {
			entry := strings.TrimSpace(fmt.Sprint(item))
			if entry == "" {
				continue
			}
			if idx := strings.Index(entry, "="); idx > 0 {
				out[strings.TrimSpace(entry[:idx])] = strings.TrimSpace(entry[idx+1:])
			} else {
				out[entry] = ""
			}
		}
	}
	return out
}

func normalizeComposeVolumes(raw interface{}) []LifecycleRuntimeVolumeSnapshot {
	switch value := raw.(type) {
	case nil:
		return nil
	case []interface{}:
		out := make([]LifecycleRuntimeVolumeSnapshot, 0, len(value))
		for _, item := range value {
			entry := strings.TrimSpace(fmt.Sprint(item))
			if entry == "" {
				continue
			}
			parts := strings.Split(entry, ":")
			switch len(parts) {
			case 1:
				out = append(out, LifecycleRuntimeVolumeSnapshot{Target: parts[0]})
			case 2:
				out = append(out, LifecycleRuntimeVolumeSnapshot{Source: parts[0], Target: parts[1]})
			default:
				out = append(out, LifecycleRuntimeVolumeSnapshot{Source: parts[0], Target: parts[1], Mode: strings.Join(parts[2:], ":")})
			}
		}
		return out
	default:
		return nil
	}
}

func readJSONVersion(path string) (int, bool) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, false
	}
	var payload struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, false
	}
	return payload.Version, true
}

func collectSQLiteSchemas(installRoot string, cfg config.AppConfig) []LifecycleRuntimeSQLiteSchema {
	if strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") {
		return nil
	}

	dbs := []struct {
		id   string
		path string
	}{
		{id: "sessions", path: filepath.Join(cfg.Store.Path, "sessions.sqlite")},
		{id: "deviceauth", path: filepath.Join(cfg.Store.Path, "deviceauth.sqlite")},
		{id: "resume", path: filepath.Join(cfg.Store.Path, "resume.sqlite")},
		{id: "capabilities", path: filepath.Join(cfg.Store.Path, "capabilities.sqlite")},
		{id: "decision_audit", path: filepath.Join(cfg.Store.Path, "decision_audit.sqlite")},
		{id: "capability_registry", path: filepath.Join(cfg.Store.Path, "capability_registry.sqlite")},
		{id: "entitlements", path: filepath.Join(cfg.Store.Path, "entitlements.sqlite")},
		{id: "household", path: filepath.Join(cfg.Store.Path, "household.sqlite")},
	}
	if strings.TrimSpace(cfg.Library.DBPath) != "" {
		dbs = append(dbs, struct {
			id   string
			path string
		}{id: "library", path: cfg.Library.DBPath})
	}

	out := make([]LifecycleRuntimeSQLiteSchema, 0, len(dbs))
	for _, dbInfo := range dbs {
		actualPath := runtimeJoin(installRoot, dbInfo.path)
		schema := LifecycleRuntimeSQLiteSchema{
			ID:     dbInfo.id,
			Path:   actualPath,
			Exists: false,
		}
		if _, err := os.Stat(actualPath); err != nil {
			out = append(out, schema)
			continue
		}
		schema.Exists = true
		if version, ok := readSQLiteUserVersion(actualPath); ok {
			schema.UserVersion = &version
		}
		out = append(out, schema)
	}
	return out
}

func readSQLiteUserVersion(path string) (int, bool) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, false
	}
	defer func() { _ = db.Close() }()

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, false
	}
	return version, true
}

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

	if snapshot.State.DriftStateVersion != nil && *snapshot.State.DriftStateVersion != 1 {
		add(LifecycleRuntimeDriftFinding{
			Code:    "runtime_snapshot.state.drift_version_unknown",
			Class:   LifecycleRuntimeDriftClassUnsupported,
			Field:   "state.driftStateVersion",
			Summary: "drift_state.json version is unsupported",
			Detail:  fmt.Sprintf("got %d", *snapshot.State.DriftStateVersion),
		})
	}

	return assessment
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
