package health

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	sqliteverify "github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/ManuGH/xg2g/internal/storageinventory"
)

type LifecycleRestoreArtifactAssessment struct {
	ID          string                            `json:"id"`
	Description string                            `json:"description,omitempty"`
	RuntimePath string                            `json:"runtimePath,omitempty"`
	RestorePath string                            `json:"restorePath,omitempty"`
	Class       storageinventory.ArtifactClass    `json:"class"`
	Verify      storageinventory.VerifyKind       `json:"verify,omitempty"`
	Required    bool                              `json:"required"`
	Exists      bool                              `json:"exists"`
	Schema      *LifecycleUpgradeSchemaAssessment `json:"schema,omitempty"`
}

type LifecycleRestoreSecretAssessment struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Satisfied   bool   `json:"satisfied"`
}

type LifecycleRestoreAssessment struct {
	RestoreRoot     string                               `json:"restoreRoot,omitempty"`
	Inventory       []LifecycleRestoreArtifactAssessment `json:"inventory,omitempty"`
	ExternalSecrets []LifecycleRestoreSecretAssessment   `json:"externalSecrets,omitempty"`
	MissingRequired []string                             `json:"missingRequired,omitempty"`
	MissingOptional []string                             `json:"missingOptional,omitempty"`
}

func evaluateLifecycleRestoreAssessment(cfg config.AppConfig, opts LifecyclePreflightOptions, add func(LifecyclePreflightFinding)) *LifecycleRestoreAssessment {
	assessment := &LifecycleRestoreAssessment{
		RestoreRoot: strings.TrimSpace(opts.RestoreRoot),
	}

	if opts.RuntimeSnapshot == nil {
		add(LifecyclePreflightFinding{
			Code:     "restore.runtime_snapshot.required",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "backup_restore_contract",
			Field:    "runtime",
			Summary:  "restore preflight requires runtime truth snapshot",
			Detail:   "re-run with --runtime-snapshot so restore gates can verify external secret and runtime prerequisites",
		})
	} else {
		assessment.ExternalSecrets = assessLifecycleRestoreExternalSecrets(opts.RuntimeSnapshot)
		for _, requirement := range assessment.ExternalSecrets {
			if requirement.Satisfied {
				continue
			}
			add(LifecyclePreflightFinding{
				Code:     "restore.external_secret.missing",
				Severity: LifecyclePreflightSeverityBlock,
				Contract: "backup_restore_contract",
				Field:    "runtime.env",
				Summary:  "required runtime secret for restore is missing",
				Detail:   requirement.Name,
			})
		}
	}

	if assessment.RestoreRoot == "" {
		add(LifecyclePreflightFinding{
			Code:     "restore.root.required",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "backup_restore_contract",
			Field:    "restoreRoot",
			Summary:  "restore preflight requires a restore artifact root",
			Detail:   "provide --restore-root pointing at the backup set to assess",
		})
		return assessment
	}

	restoreRootInfo, err := os.Stat(assessment.RestoreRoot)
	switch {
	case os.IsNotExist(err):
		add(LifecyclePreflightFinding{
			Code:     "restore.root.missing",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "backup_restore_contract",
			Field:    "restoreRoot",
			Summary:  "restore artifact root does not exist",
			Detail:   assessment.RestoreRoot,
		})
		return assessment
	case err != nil:
		add(LifecyclePreflightFinding{
			Code:     "restore.root.unreadable",
			Severity: LifecyclePreflightSeverityFatal,
			Contract: "backup_restore_contract",
			Field:    "restoreRoot",
			Summary:  "restore artifact root could not be inspected",
			Detail:   err.Error(),
		})
		return assessment
	case !restoreRootInfo.IsDir():
		add(LifecyclePreflightFinding{
			Code:     "restore.root.not_directory",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "backup_restore_contract",
			Field:    "restoreRoot",
			Summary:  "restore artifact root must be a directory",
			Detail:   assessment.RestoreRoot,
		})
		return assessment
	}

	restoreArtifacts := lifecycleRestoreArtifacts(cfg)
	requiredSchemas := make([]LifecycleRuntimeSQLiteSchema, 0, len(restoreArtifacts))
	inventory := make([]LifecycleRestoreArtifactAssessment, 0, len(restoreArtifacts))
	for _, artifact := range restoreArtifacts {
		restorePath, exists := resolveRestoreArtifactPath(assessment.RestoreRoot, artifact.Path)
		entry := LifecycleRestoreArtifactAssessment{
			ID:          artifact.ID,
			Description: artifact.Description,
			RuntimePath: artifact.Path,
			RestorePath: restorePath,
			Class:       artifact.Class,
			Verify:      artifact.Verify,
			Required:    !artifact.Optional,
			Exists:      exists,
		}
		if !exists {
			if entry.Required {
				assessment.MissingRequired = append(assessment.MissingRequired, artifact.ID)
				add(LifecyclePreflightFinding{
					Code:     "restore.artifact.required_missing",
					Severity: LifecyclePreflightSeverityBlock,
					Contract: "backup_restore_contract",
					Field:    "restore." + artifact.ID,
					Summary:  "required restore artifact is missing",
					Detail:   restorePath,
				})
			} else {
				assessment.MissingOptional = append(assessment.MissingOptional, artifact.ID)
				add(LifecyclePreflightFinding{
					Code:     "restore.artifact.optional_missing",
					Severity: LifecyclePreflightSeverityWarn,
					Contract: "backup_restore_contract",
					Field:    "restore." + artifact.ID,
					Summary:  "optional restore artifact is missing",
					Detail:   restorePath,
				})
			}
			inventory = append(inventory, entry)
			continue
		}

		artifactValid := true
		if err := verifyLifecycleRestoreArtifact(artifact, restorePath); err != nil {
			artifactValid = false
			add(LifecyclePreflightFinding{
				Code:     "restore.artifact.invalid",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "backup_restore_contract",
				Field:    "restore." + artifact.ID,
				Summary:  "restore artifact failed integrity verification",
				Detail:   fmt.Sprintf("%s: %v", restorePath, err),
			})
		}

		if artifactValid && artifact.Verify == storageinventory.VerifySQLite {
			schema := LifecycleRuntimeSQLiteSchema{
				ID:     artifact.ID,
				Path:   restorePath,
				Exists: true,
			}
			if version, ok := readSQLiteUserVersion(restorePath); ok {
				schema.UserVersion = &version
			}
			requiredSchemas = append(requiredSchemas, schema)
		}
		inventory = append(inventory, entry)
	}

	schemaChecks := assessLifecycleUpgradeSchemas(requiredSchemas)
	byID := make(map[string]LifecycleUpgradeSchemaAssessment, len(schemaChecks))
	for _, check := range schemaChecks {
		byID[check.ID] = check
	}
	for i := range inventory {
		check, ok := byID[inventory[i].ID]
		if !ok {
			continue
		}
		checkCopy := check
		inventory[i].Schema = &checkCopy
		switch check.Status {
		case LifecycleUpgradeSchemaStatusMigrationRequired:
			add(LifecyclePreflightFinding{
				Code:     "restore.state_schema.migration_required",
				Severity: LifecyclePreflightSeverityWarn,
				Contract: "backup_restore_contract",
				Field:    "restore." + inventory[i].ID,
				Summary:  "restore artifact will require schema migration on first start",
				Detail:   fmt.Sprintf("%s: current=%d target=%d", check.Path, derefInt(check.CurrentUserVersion), derefInt(check.ExpectedUserVersion)),
			})
		case LifecycleUpgradeSchemaStatusForwardIncompatible:
			add(LifecyclePreflightFinding{
				Code:     "restore.state_schema.forward_incompatible",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "backup_restore_contract",
				Field:    "restore." + inventory[i].ID,
				Summary:  "restore artifact schema is newer than the current binary supports",
				Detail:   fmt.Sprintf("%s: current=%d target=%d", check.Path, derefInt(check.CurrentUserVersion), derefInt(check.ExpectedUserVersion)),
			})
		case LifecycleUpgradeSchemaStatusUnversioned:
			add(LifecyclePreflightFinding{
				Code:     "restore.state_schema.unversioned",
				Severity: LifecyclePreflightSeverityWarn,
				Contract: "backup_restore_contract",
				Field:    "restore." + inventory[i].ID,
				Summary:  "restore artifact schema could not be versioned",
				Detail:   check.Path,
			})
		}
	}

	assessment.Inventory = inventory
	slices.Sort(assessment.MissingRequired)
	slices.Sort(assessment.MissingOptional)
	return assessment
}

func lifecycleRestoreArtifacts(cfg config.AppConfig) []storageinventory.Artifact {
	return storageinventory.BackupArtifacts(storageinventory.RuntimePaths{
		DataDir:          cfg.DataDir,
		StorePath:        cfg.Store.Path,
		StoreBackend:     cfg.Store.Backend,
		HLSRoot:          cfg.HLS.Root,
		PlaylistFilename: filepath.Base(strings.TrimSpace(cfg.XMLTVPath)),
		XMLTVPath:        cfg.XMLTVPath,
		LibraryDBPath:    cfg.Library.DBPath,
	})
}

func assessLifecycleRestoreExternalSecrets(snapshot *LifecycleRuntimeSnapshot) []LifecycleRestoreSecretAssessment {
	if snapshot == nil {
		return nil
	}
	present := make(map[string]struct{}, len(snapshot.Env.PresentKeys))
	for _, key := range snapshot.Env.PresentKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		present[key] = struct{}{}
	}
	has := func(keys ...string) bool {
		for _, key := range keys {
			if _, ok := present[key]; ok {
				return true
			}
		}
		return false
	}
	return []LifecycleRestoreSecretAssessment{
		{
			Name:        "XG2G_API_TOKEN|XG2G_API_TOKENS",
			Description: "API auth token source must remain available after restore",
			Satisfied:   has("XG2G_API_TOKEN", "XG2G_API_TOKENS"),
		},
		{
			Name:        "XG2G_DECISION_SECRET|XG2G_PLAYBACK_DECISION_SECRET",
			Description: "playback decision signing secret must remain available after restore",
			Satisfied:   has("XG2G_DECISION_SECRET", "XG2G_PLAYBACK_DECISION_SECRET"),
		},
	}
}

func resolveRestoreArtifactPath(root, runtimePath string) (string, bool) {
	root = strings.TrimSpace(root)
	runtimePath = strings.TrimSpace(runtimePath)
	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	addCandidate := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	addCandidate(filepath.Join(root, filepath.Base(runtimePath)))
	if rel := strings.TrimPrefix(filepath.Clean(runtimePath), string(filepath.Separator)); rel != "" {
		addCandidate(filepath.Join(root, rel))
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	if len(candidates) == 0 {
		return root, false
	}
	return candidates[0], false
}

func verifyLifecycleRestoreArtifact(artifact storageinventory.Artifact, path string) error {
	switch artifact.Verify {
	case storageinventory.VerifySQLite:
		issues, err := sqliteverify.VerifyIntegrity(path, "quick")
		if err != nil {
			return err
		}
		if len(issues) > 0 {
			return fmt.Errorf("%s", strings.Join(issues, "; "))
		}
		return nil
	case storageinventory.VerifyJSON:
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return fmt.Errorf("invalid json")
		}
		return nil
	default:
		return nil
	}
}
