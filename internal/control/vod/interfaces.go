package vod

import (
	vodtypes "github.com/ManuGH/xg2g/internal/domain/vod"
)

// Re-export domain types for backward compatibility.
// New code should import github.com/ManuGH/xg2g/internal/domain/vod directly.
type Spec = vodtypes.Spec
type Profile = vodtypes.Profile
type ProgressEvent = vodtypes.ProgressEvent
type Runner = vodtypes.Runner
type Handle = vodtypes.Handle

const (
	ProfileDefault = vodtypes.ProfileDefault
	ProfileHigh    = vodtypes.ProfileHigh
	ProfileLow     = vodtypes.ProfileLow
)
