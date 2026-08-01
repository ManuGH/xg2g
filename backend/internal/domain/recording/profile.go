// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidProfileID     = errors.New("invalid recording profile ID")
	ErrUnsupportedContainer = errors.New("unsupported container format")
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

// RecordingTarget details the target storage backend and relative folder.
type RecordingTarget struct {
	BackendID    string `json:"backend_id"`
	RelativePath string `json:"relative_path"`
}

// RecordingProfile holds persistent settings for target location, container format, and naming rules.
type RecordingProfile struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Target           RecordingTarget `json:"target"`
	ContainerFormat  ContainerFormat `json:"container_format"`
	NamingPreset     NamingPreset    `json:"naming_preset"`
	FilenameTemplate string          `json:"filename_template,omitempty"`
}

// NewRecordingProfile initializes a RecordingProfile with defaults.
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
	}, nil
}
