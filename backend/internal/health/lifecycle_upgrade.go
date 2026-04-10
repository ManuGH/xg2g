package health

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	recordingcapreg "github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	recordingdecision "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/household"
	resumestore "github.com/ManuGH/xg2g/internal/pipeline/resume"
	scanstore "github.com/ManuGH/xg2g/internal/pipeline/scan"
	"golang.org/x/mod/semver"
)

type LifecycleUpgradeSchemaStatus string

const (
	LifecycleUpgradeSchemaStatusCurrent             LifecycleUpgradeSchemaStatus = "current"
	LifecycleUpgradeSchemaStatusMigrationRequired   LifecycleUpgradeSchemaStatus = "migration_required"
	LifecycleUpgradeSchemaStatusForwardIncompatible LifecycleUpgradeSchemaStatus = "forward_incompatible"
	LifecycleUpgradeSchemaStatusUnversioned         LifecycleUpgradeSchemaStatus = "unversioned"
)

type LifecycleUpgradeSchemaAssessment struct {
	ID                  string                       `json:"id"`
	Path                string                       `json:"path"`
	Status              LifecycleUpgradeSchemaStatus `json:"status"`
	CurrentUserVersion  *int                         `json:"currentUserVersion,omitempty"`
	ExpectedUserVersion *int                         `json:"expectedUserVersion,omitempty"`
}

type LifecycleUpgradeAssessment struct {
	CurrentRelease         string                             `json:"currentRelease,omitempty"`
	TargetRelease          string                             `json:"targetRelease,omitempty"`
	CurrentImage           string                             `json:"currentImage,omitempty"`
	TargetImage            string                             `json:"targetImage,omitempty"`
	ConfigPath             string                             `json:"configPath,omitempty"`
	FileConfigVersion      string                             `json:"fileConfigVersion,omitempty"`
	TargetConfigVersion    string                             `json:"targetConfigVersion,omitempty"`
	ConfigMigrationChanges []string                           `json:"configMigrationChanges,omitempty"`
	DeprecatedSurfaces     []string                           `json:"deprecatedSurfaces,omitempty"`
	StateSchemas           []LifecycleUpgradeSchemaAssessment `json:"stateSchemas,omitempty"`
}

func evaluateLifecycleUpgradeAssessment(cfg config.AppConfig, opts LifecyclePreflightOptions, add func(LifecyclePreflightFinding)) *LifecycleUpgradeAssessment {
	assessment := &LifecycleUpgradeAssessment{
		ConfigPath:          strings.TrimSpace(opts.ConfigPath),
		TargetConfigVersion: config.V3ConfigVersion,
	}

	if opts.RuntimeSnapshot == nil {
		add(LifecyclePreflightFinding{
			Code:     "upgrade.runtime_snapshot.required",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "upgrade_migration_contract",
			Field:    "runtime",
			Summary:  "upgrade preflight requires runtime truth snapshot",
			Detail:   "re-run with --runtime-snapshot to compare the live deployment against the target release",
		})
	} else {
		assessment.CurrentImage = strings.TrimSpace(opts.RuntimeSnapshot.Compose.Image)
		assessment.TargetImage = strings.TrimSpace(opts.RuntimeSnapshot.Compose.RepoImage)
		assessment.CurrentRelease = extractLifecycleRelease(assessment.CurrentImage)
		assessment.TargetRelease = resolveLifecycleTargetRelease(opts, assessment.TargetImage, opts.RuntimeSnapshot.Build.Release)

		switch {
		case assessment.CurrentRelease == "":
			add(LifecyclePreflightFinding{
				Code:     "upgrade.current_release.unknown",
				Severity: LifecyclePreflightSeverityBlock,
				Contract: "upgrade_migration_contract",
				Field:    "runtime.compose.image",
				Summary:  "current runtime release could not be determined from the live compose image",
				Detail:   assessment.CurrentImage,
			})
		case !semver.IsValid(assessment.CurrentRelease):
			add(LifecyclePreflightFinding{
				Code:     "upgrade.current_release.invalid",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "upgrade_migration_contract",
				Field:    "runtime.compose.image",
				Summary:  "current runtime release is not a valid semantic version",
				Detail:   assessment.CurrentRelease,
			})
		}

		switch {
		case assessment.TargetRelease == "":
			add(LifecyclePreflightFinding{
				Code:     "upgrade.target_release.unknown",
				Severity: LifecyclePreflightSeverityBlock,
				Contract: "upgrade_migration_contract",
				Field:    "targetRelease",
				Summary:  "target release could not be determined",
				Detail:   "provide --target-version or ensure the repo deploy bundle pins a tagged release image",
			})
		case !semver.IsValid(assessment.TargetRelease):
			add(LifecyclePreflightFinding{
				Code:     "upgrade.target_release.invalid",
				Severity: LifecyclePreflightSeverityFatal,
				Contract: "upgrade_migration_contract",
				Field:    "targetRelease",
				Summary:  "target release is not a valid semantic version",
				Detail:   assessment.TargetRelease,
			})
		}

		if assessment.TargetImage != "" {
			targetImageRelease := extractLifecycleRelease(assessment.TargetImage)
			if targetImageRelease != "" && assessment.TargetRelease != "" && targetImageRelease != assessment.TargetRelease {
				add(LifecyclePreflightFinding{
					Code:     "upgrade.target_release.repo_image_mismatch",
					Severity: LifecyclePreflightSeverityBlock,
					Contract: "upgrade_migration_contract",
					Field:    "targetRelease",
					Summary:  "target release does not match the repo deploy bundle image tag",
					Detail:   fmt.Sprintf("target=%s repoImage=%s", assessment.TargetRelease, targetImageRelease),
				})
			}
		}

		if semver.IsValid(assessment.CurrentRelease) && semver.IsValid(assessment.TargetRelease) {
			switch cmp := semver.Compare(assessment.CurrentRelease, assessment.TargetRelease); {
			case cmp > 0:
				add(LifecyclePreflightFinding{
					Code:     "upgrade.release.downgrade_requested",
					Severity: LifecyclePreflightSeverityBlock,
					Contract: "upgrade_migration_contract",
					Field:    "targetRelease",
					Summary:  "upgrade target is older than the current runtime release",
					Detail:   fmt.Sprintf("current=%s target=%s", assessment.CurrentRelease, assessment.TargetRelease),
				})
			case cmp == 0:
				add(LifecyclePreflightFinding{
					Code:     "upgrade.release.already_current",
					Severity: LifecyclePreflightSeverityWarn,
					Contract: "upgrade_migration_contract",
					Field:    "targetRelease",
					Summary:  "target release already matches the current runtime release",
					Detail:   assessment.TargetRelease,
				})
			}
		}

		legacyEnvKeys := config.FindLegacyEnvKeys(presentKeysAsEnviron(opts.RuntimeSnapshot.Env.PresentKeys))
		for _, key := range legacyEnvKeys {
			assessment.DeprecatedSurfaces = append(assessment.DeprecatedSurfaces, "env:"+key)
			add(LifecyclePreflightFinding{
				Code:     "upgrade.runtime_env.legacy_key",
				Severity: LifecyclePreflightSeverityBlock,
				Contract: "upgrade_migration_contract",
				Field:    "runtime.env",
				Summary:  "runtime env file still uses a removed legacy key",
				Detail:   key,
			})
		}
		deprecatedRuntimeEnvKeys := configuredDeprecatedRuntimeEnvKeys(opts.RuntimeSnapshot.Env.PresentKeys)
		for _, key := range deprecatedRuntimeEnvKeys {
			assessment.DeprecatedSurfaces = append(assessment.DeprecatedSurfaces, "env:"+key)
			add(LifecyclePreflightFinding{
				Code:     "upgrade.runtime_env.deprecated_key",
				Severity: LifecyclePreflightSeverityBlock,
				Contract: "upgrade_migration_contract",
				Field:    "runtime.env",
				Summary:  "runtime env file still configures a deprecated key",
				Detail:   key,
			})
		}

		assessment.StateSchemas = assessLifecycleUpgradeSchemas(opts.RuntimeSnapshot.State.SQLiteSchemas)
		for _, schema := range assessment.StateSchemas {
			switch schema.Status {
			case LifecycleUpgradeSchemaStatusMigrationRequired:
				add(LifecyclePreflightFinding{
					Code:     "upgrade.state_schema.migration_required",
					Severity: LifecyclePreflightSeverityWarn,
					Contract: "upgrade_migration_contract",
					Field:    "state." + schema.ID,
					Summary:  "state store will require migration on first start of the target release",
					Detail:   fmt.Sprintf("%s: current=%d target=%d", schema.Path, derefInt(schema.CurrentUserVersion), derefInt(schema.ExpectedUserVersion)),
				})
			case LifecycleUpgradeSchemaStatusForwardIncompatible:
				add(LifecyclePreflightFinding{
					Code:     "upgrade.state_schema.forward_incompatible",
					Severity: LifecyclePreflightSeverityBlock,
					Contract: "upgrade_migration_contract",
					Field:    "state." + schema.ID,
					Summary:  "state store schema is newer than the target release supports",
					Detail:   fmt.Sprintf("%s: current=%d target=%d", schema.Path, derefInt(schema.CurrentUserVersion), derefInt(schema.ExpectedUserVersion)),
				})
			}
		}
	}

	if opts.FileConfig == nil {
		add(LifecyclePreflightFinding{
			Code:     "upgrade.config_file.unavailable",
			Severity: LifecyclePreflightSeverityWarn,
			Contract: "upgrade_migration_contract",
			Field:    "config",
			Summary:  "raw file config was not available for upgrade migration checks",
			Detail:   "config migration and deprecated YAML surface checks were skipped",
		})
		return assessment
	}

	assessment.FileConfigVersion = fileConfigVersion(*opts.FileConfig)
	updated, changes, err := config.MigrateFileConfig(*opts.FileConfig, config.V3ConfigVersion)
	_ = updated
	if err != nil {
		add(LifecyclePreflightFinding{
			Code:     "upgrade.config_migration.evaluation_failed",
			Severity: LifecyclePreflightSeverityFatal,
			Contract: "upgrade_migration_contract",
			Field:    "configVersion",
			Summary:  "config migration assessment failed",
			Detail:   err.Error(),
		})
	} else if len(changes) > 0 {
		assessment.ConfigMigrationChanges = append([]string(nil), changes...)
		add(LifecyclePreflightFinding{
			Code:     "upgrade.config_migration.required",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "upgrade_migration_contract",
			Field:    "configVersion",
			Summary:  "config file requires migration before upgrade",
			Detail:   strings.Join(changes, "; "),
		})
	}

	for _, path := range config.DeprecatedFileConfigPaths(*opts.FileConfig) {
		assessment.DeprecatedSurfaces = append(assessment.DeprecatedSurfaces, "file:"+path)
		add(LifecyclePreflightFinding{
			Code:     "upgrade.config.deprecated_path",
			Severity: LifecyclePreflightSeverityBlock,
			Contract: "upgrade_migration_contract",
			Field:    path,
			Summary:  "config file still uses a deprecated path",
			Detail:   path,
		})
	}
	assessment.DeprecatedSurfaces = slices.Compact(assessment.DeprecatedSurfaces)
	return assessment
}

func resolveLifecycleTargetRelease(opts LifecyclePreflightOptions, targetImage, buildRelease string) string {
	if target := strings.TrimSpace(opts.TargetRelease); target != "" {
		return normalizeLifecycleRelease(target)
	}
	if release := extractLifecycleRelease(targetImage); release != "" {
		return release
	}
	return normalizeLifecycleRelease(buildRelease)
}

func extractLifecycleRelease(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if idx := strings.Index(image, "@"); idx >= 0 {
		image = strings.TrimSpace(image[:idx])
	}
	colon := strings.LastIndex(image, ":")
	slash := strings.LastIndex(image, "/")
	if colon <= slash {
		return ""
	}
	return normalizeLifecycleRelease(image[colon+1:])
}

func normalizeLifecycleRelease(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "v") {
		return value
	}
	if semver.IsValid("v" + value) {
		return "v" + value
	}
	return value
}

func fileConfigVersion(cfg config.FileConfig) string {
	switch {
	case strings.TrimSpace(cfg.ConfigVersion) != "":
		return strings.TrimSpace(cfg.ConfigVersion)
	case strings.TrimSpace(cfg.Version) != "":
		return strings.TrimSpace(cfg.Version)
	default:
		return ""
	}
}

func configuredDeprecatedRuntimeEnvKeys(presentKeys []string) []string {
	deprecated := config.DeprecatedEnvKeys()
	out := make([]string, 0)
	for _, key := range deprecated {
		if slices.Contains(presentKeys, key) {
			out = append(out, key)
		}
	}
	return out
}

func presentKeysAsEnviron(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key+"=")
	}
	return out
}

func assessLifecycleUpgradeSchemas(schemas []LifecycleRuntimeSQLiteSchema) []LifecycleUpgradeSchemaAssessment {
	expected := lifecycleExpectedSQLiteSchemaVersions()
	out := make([]LifecycleUpgradeSchemaAssessment, 0, len(schemas))
	for _, schema := range schemas {
		check := LifecycleUpgradeSchemaAssessment{
			ID:   schema.ID,
			Path: schema.Path,
		}
		if want, ok := expected[schema.ID]; ok {
			check.ExpectedUserVersion = intPtr(want)
		}
		if schema.UserVersion != nil {
			value := *schema.UserVersion
			check.CurrentUserVersion = &value
		}

		switch {
		case check.ExpectedUserVersion == nil || check.CurrentUserVersion == nil:
			check.Status = LifecycleUpgradeSchemaStatusUnversioned
		case *check.CurrentUserVersion < *check.ExpectedUserVersion:
			check.Status = LifecycleUpgradeSchemaStatusMigrationRequired
		case *check.CurrentUserVersion > *check.ExpectedUserVersion:
			check.Status = LifecycleUpgradeSchemaStatusForwardIncompatible
		default:
			check.Status = LifecycleUpgradeSchemaStatusCurrent
		}
		out = append(out, check)
	}
	return out
}

func lifecycleExpectedSQLiteSchemaVersions() map[string]int {
	return map[string]int{
		"sessions":            sessionstore.SchemaVersion,
		"resume":              resumestore.SchemaVersion,
		"capabilities":        scanstore.SchemaVersion,
		"decision_audit":      recordingdecision.SQLiteAuditSchemaVersion,
		"capability_registry": recordingcapreg.SQLiteSchemaVersion,
		"entitlements":        entitlements.SQLiteSchemaVersion,
		"household":           household.SQLiteSchemaVersion,
		"deviceauth":          deviceauthstore.SQLiteSchemaVersion,
	}
}

func intPtr(value int) *int {
	return &value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
