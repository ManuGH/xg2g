// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidProfileID        = errors.New("invalid recording profile ID")
	ErrUnsupportedContainer    = errors.New("unsupported container format")
	ErrDeletionPolicyForbidden = errors.New("physical file deletion forbidden by management mode or policy")
)

// ContainerFormat defines supported output file formats.
type ContainerFormat string

const (
	ContainerTS  ContainerFormat = "ts"
	ContainerMP4 ContainerFormat = "mp4"
)

// NamingPreset defines generic media naming presets.
type NamingPreset string

const (
	NamingPresetGenericTV NamingPreset = "GENERIC_TV"
	NamingPresetMovies    NamingPreset = "MOVIES"
	NamingPresetSeries    NamingPreset = "SERIES"
	NamingPresetCustom    NamingPreset = "CUSTOM"
)

// AssetManagementMode defines ownership rules for assets and folders shared with external systems.
type AssetManagementMode string

const (
	ManagementXG2GManaged AssetManagementMode = "XG2G_MANAGED"
	ManagementShared      AssetManagementMode = "SHARED"
	ManagementExternal    AssetManagementMode = "EXTERNAL"
)

// DeletePolicy defines default behavior when deleting an asset from xg2g UI.
type DeletePolicy string

const (
	DeleteAssetAndFile DeletePolicy = "DELETE_ASSET_AND_FILE"
	DeleteAssetOnly    DeletePolicy = "DELETE_ASSET_ONLY"
)

// RecordingTarget details the target storage backend and relative folder.
type RecordingTarget struct {
	BackendID    string `json:"backend_id"`
	RelativePath string `json:"relative_path"`
}

// RecordingProfile holds persistent settings for target location, container format, and naming/ownership rules.
type RecordingProfile struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Target           RecordingTarget     `json:"target"`
	ContainerFormat  ContainerFormat     `json:"container_format"`
	NamingPreset     NamingPreset        `json:"naming_preset"`
	FilenameTemplate string              `json:"filename_template,omitempty"`
	ManagementMode   AssetManagementMode `json:"management_mode"`
	DeletePolicy     DeletePolicy        `json:"delete_policy"`
}

// NewRecordingProfile initializes a RecordingProfile with safe defaults.
func NewRecordingProfile(id, name, backendID, relPath string, format ContainerFormat, preset NamingPreset) (*RecordingProfile, error) {
	if id == "" {
		return nil, ErrInvalidProfileID
	}
	if format != ContainerTS && format != ContainerMP4 {
		return nil, fmt.Errorf("%w: '%s'", ErrUnsupportedContainer, format)
	}
	return &RecordingProfile{
		ID:   id,
		Name: name,
		Target: RecordingTarget{
			BackendID:    backendID,
			RelativePath: relPath,
		},
		ContainerFormat: format,
		NamingPreset:    preset,
		ManagementMode:  ManagementXG2GManaged,
		DeletePolicy:    DeleteAssetAndFile,
	}, nil
}

// CanDeletePhysicalFile enforces domain rules governing physical file deletion.
func CanDeletePhysicalFile(mode AssetManagementMode, policy DeletePolicy, force bool) error {
	if mode == ManagementExternal && !force {
		return fmt.Errorf("%w: external assets cannot delete physical files without force", ErrDeletionPolicyForbidden)
	}
	if mode == ManagementShared && policy == DeleteAssetOnly && !force {
		return fmt.Errorf("%w: shared assets with DeleteAssetOnly policy require explicit confirmation/force to delete physical files", ErrDeletionPolicyForbidden)
	}
	if policy == DeleteAssetOnly && !force {
		return fmt.Errorf("%w: DeleteAssetOnly policy forbids physical file deletion without force", ErrDeletionPolicyForbidden)
	}
	return nil
}
