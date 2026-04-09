package storageinventory

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	platformpaths "github.com/ManuGH/xg2g/internal/platform/paths"
)

type ArtifactClass string

const (
	ClassPersistent      ArtifactClass = "persistent"
	ClassTransient       ArtifactClass = "transient"
	ClassMaterialized    ArtifactClass = "materialized"
	ClassOperational     ArtifactClass = "operational"
	ClassReconstructable ArtifactClass = "reconstructable"
)

type VerifyKind string

const (
	VerifyNone   VerifyKind = ""
	VerifySQLite VerifyKind = "sqlite"
	VerifyJSON   VerifyKind = "json"
)

type Artifact struct {
	ID          string
	Path        string
	Description string
	Class       ArtifactClass
	Verify      VerifyKind
	Optional    bool
	Backup      bool
}

type RuntimePaths struct {
	DataDir          string
	StorePath        string
	StoreBackend     string
	HLSRoot          string
	PlaylistFilename string
	XMLTVPath        string
	LibraryDBPath    string
}

// ResolveRuntimePathsFromEnv captures the storage-relevant runtime paths without
// requiring full config loading. This keeps storage tooling usable even when
// unrelated runtime config such as API auth is intentionally absent.
func ResolveRuntimePathsFromEnv() RuntimePaths {
	env := config.ReadOSRuntimeEnvOrDefault()
	paths := RuntimePaths{
		DataDir:          strings.TrimSpace(config.ResolveDataDirFromEnv()),
		StorePath:        strings.TrimSpace(config.ParseString("XG2G_STORE_PATH", "")),
		StoreBackend:     normalizeStoreBackend(config.ParseString("XG2G_STORE_BACKEND", "sqlite")),
		HLSRoot:          strings.TrimSpace(config.ParseString(platformpaths.EnvHLSRoot, "")),
		PlaylistFilename: strings.TrimSpace(env.Runtime.PlaylistFilename),
		XMLTVPath:        strings.TrimSpace(config.ParseString("XG2G_XMLTV", "xmltv.xml")),
	}
	if paths.HLSRoot == "" {
		paths.HLSRoot = strings.TrimSpace(config.ParseString(platformpaths.EnvLegacyHLSRoot, ""))
	}
	mergeFileConfigPaths(&paths)
	if paths.HLSRoot == "" && paths.DataDir != "" {
		paths.HLSRoot = filepath.Join(paths.DataDir, platformpaths.TargetDirName)
	}
	return paths
}

func normalizeStoreBackend(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "sqlite"
	}
	return value
}

func ResolveSQLiteArtifactPath(dataDir, storePath, dbName string) string {
	dataDir = strings.TrimSpace(dataDir)
	storePath = strings.TrimSpace(storePath)
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return ""
	}

	candidates := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	addCandidate := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		path := filepath.Join(dir, dbName)
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	addCandidate(storePath)
	addCandidate(filepath.Join(dataDir, "store"))
	addCandidate(dataDir)

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return dbName
}

// VerifiableArtifacts returns the storage artifacts that xg2g can validate
// today. Missing optional artifacts are skipped by callers.
func VerifiableArtifacts(paths RuntimePaths) []Artifact {
	all := Inventory(paths)
	out := make([]Artifact, 0, len(all))
	for _, artifact := range all {
		if artifact.Verify == VerifyNone {
			continue
		}
		out = append(out, artifact)
	}
	return out
}

// BackupArtifacts returns the state that operators should preserve for backup.
func BackupArtifacts(paths RuntimePaths) []Artifact {
	all := Inventory(paths)
	out := make([]Artifact, 0, len(all))
	for _, artifact := range all {
		if !artifact.Backup {
			continue
		}
		out = append(out, artifact)
	}
	return out
}

func Inventory(paths RuntimePaths) []Artifact {
	playlistName := strings.TrimSpace(paths.PlaylistFilename)
	if playlistName == "" {
		playlistName = "playlist.m3u8"
	}
	xmltvPath := strings.TrimSpace(paths.XMLTVPath)
	if xmltvPath == "" {
		xmltvPath = "xmltv.xml"
	}

	artifacts := make([]Artifact, 0, 16)
	if normalizeStoreBackend(paths.StoreBackend) != "memory" {
		storeDBs := []struct {
			id          string
			file        string
			description string
		}{
			{id: "sessions", file: "sessions.sqlite", description: "Active and historical playback session state"},
			{id: "resume", file: "resume.sqlite", description: "Playback resume positions for recordings"},
			{id: "capabilities", file: "capabilities.sqlite", description: "Receiver/source capability scan results"},
			{id: "decision_audit", file: "decision_audit.sqlite", description: "Persisted playback decision history"},
			{id: "capability_registry", file: "capability_registry.sqlite", description: "Observed host/device/source capability registry"},
			{id: "entitlements", file: "entitlements.sqlite", description: "Purchased and granted playback entitlements"},
			{id: "household", file: "household.sqlite", description: "Household profiles and PIN-gated access state"},
		}
		for _, db := range storeDBs {
			artifacts = append(artifacts, Artifact{
				ID:          db.id,
				Path:        ResolveSQLiteArtifactPath(paths.DataDir, paths.StorePath, db.file),
				Description: db.description,
				Class:       ClassPersistent,
				Verify:      VerifySQLite,
				Backup:      true,
			})
		}

		decisionAuditPath := ResolveSQLiteArtifactPath(paths.DataDir, paths.StorePath, "decision_audit.sqlite")
		if decisionAuditPath != "" {
			artifacts = append(artifacts, Artifact{
				ID:          "last_sweep",
				Path:        filepath.Join(filepath.Dir(decisionAuditPath), "last_sweep.json"),
				Description: "Persisted decision-sweep diff baseline",
				Class:       ClassOperational,
				Verify:      VerifyJSON,
				Optional:    true,
			})
		}
	}

	if strings.TrimSpace(paths.LibraryDBPath) != "" {
		artifacts = append(artifacts, Artifact{
			ID:          "library_db",
			Path:        strings.TrimSpace(paths.LibraryDBPath),
			Description: "Library scan and duration cache",
			Class:       ClassReconstructable,
			Verify:      VerifySQLite,
			Optional:    true,
		})
	}

	dataDir := strings.TrimSpace(paths.DataDir)
	if strings.TrimSpace(paths.HLSRoot) != "" {
		artifacts = append(artifacts,
			Artifact{
				ID:          "live_sessions_root",
				Path:        platformpaths.LiveSessionsRoot(paths.HLSRoot),
				Description: "Ephemeral live session HLS artifacts and first-frame markers",
				Class:       ClassTransient,
				Optional:    true,
			},
			Artifact{
				ID:          "recording_artifacts_root",
				Path:        platformpaths.RecordingArtifactsRoot(paths.HLSRoot),
				Description: "Materialized recording HLS artifacts with eviction and rehydration semantics",
				Class:       ClassMaterialized,
				Optional:    true,
			},
		)
	}

	if dataDir == "" {
		return artifacts
	}

	artifacts = append(artifacts,
		Artifact{
			ID:          "channels",
			Path:        filepath.Join(dataDir, "channels.json"),
			Description: "Persisted channel enable/disable state",
			Class:       ClassPersistent,
			Verify:      VerifyJSON,
			Optional:    true,
			Backup:      true,
		},
		Artifact{
			ID:          "series_rules",
			Path:        filepath.Join(dataDir, "series_rules.json"),
			Description: "Persisted DVR series-rule configuration",
			Class:       ClassPersistent,
			Verify:      VerifyJSON,
			Optional:    true,
			Backup:      true,
		},
		Artifact{
			ID:          "drift_state",
			Path:        filepath.Join(dataDir, "drift_state.json"),
			Description: "Persisted verification drift snapshot",
			Class:       ClassOperational,
			Verify:      VerifyJSON,
			Optional:    true,
		},
		Artifact{
			ID:          "playlist",
			Path:        filepath.Join(dataDir, playlistName),
			Description: "Generated playlist output",
			Class:       ClassReconstructable,
			Optional:    true,
		},
		Artifact{
			ID:          "xmltv",
			Path:        filepath.Join(dataDir, xmltvPath),
			Description: "Generated XMLTV output",
			Class:       ClassReconstructable,
			Optional:    true,
		},
		Artifact{
			ID:          "picons",
			Path:        filepath.Join(dataDir, "picons"),
			Description: "Downloaded picon cache",
			Class:       ClassReconstructable,
			Optional:    true,
		},
	)

	return artifacts
}

func mergeFileConfigPaths(paths *RuntimePaths) {
	if paths == nil {
		return
	}
	dataDir := strings.TrimSpace(paths.DataDir)
	if dataDir == "" {
		return
	}

	configPath := filepath.Join(dataDir, "config.yaml")
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() {
		return
	}

	fileCfg, err := config.LoadFileConfig(configPath)
	if err != nil || fileCfg == nil {
		return
	}

	if !config.HasProcessEnv("XG2G_STORE_PATH") && fileCfg.Store != nil {
		if value := strings.TrimSpace(fileCfg.Store.Path); value != "" {
			paths.StorePath = value
		}
	}
	if !config.HasProcessEnv("XG2G_STORE_BACKEND") && fileCfg.Store != nil {
		if value := strings.TrimSpace(fileCfg.Store.Backend); value != "" {
			paths.StoreBackend = normalizeStoreBackend(value)
		}
	}
	if !config.HasProcessEnv("XG2G_XMLTV") {
		if value := strings.TrimSpace(fileCfg.EPG.XMLTVPath); value != "" {
			paths.XMLTVPath = value
		}
	}
	if !config.HasProcessEnv(platformpaths.EnvHLSRoot) && fileCfg.HLS != nil {
		if value := strings.TrimSpace(fileCfg.HLS.Root); value != "" {
			paths.HLSRoot = value
		}
	}
	libraryEnabled := fileCfg.Library.Enabled != nil && *fileCfg.Library.Enabled
	if libraryEnabled {
		paths.LibraryDBPath = strings.TrimSpace(fileCfg.Library.DBPath)
	}
}
