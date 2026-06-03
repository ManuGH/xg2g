package health

import (
	"context"
	"maps"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	appversion "github.com/ManuGH/xg2g/internal/version"
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
			maps.Copy(snapshot.Compose.Environment, svc.Environment)
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
