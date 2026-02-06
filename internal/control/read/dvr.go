package read

import (
	"context"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// DvrSource defines the minimal interface required to fetch DVR status and capabilities.
// DvrSource defines the minimal interface required to fetch DVR status and capabilities.
type DvrSource interface {
	GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error)
	DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error)
}

// DvrCapabilities represents the capabilities of the DVR subsystem.
type DvrCapabilities struct {
	CanDelete         bool   `json:"can_delete"`
	CanEdit           bool   `json:"can_edit"`
	ReadBackVerify    bool   `json:"read_back_verify"`
	ConflictsPreview  bool   `json:"conflicts_preview"`
	ReceiverAware     bool   `json:"receiver_aware"`
	SeriesSupported   bool   `json:"series_supported"`
	SeriesMode        string `json:"series_mode"` // "none", "managed", "delegated"
	DelegatedProvider string `json:"delegated_provider,omitempty"`
}

// DvrStatus represents the current status of the DVR engine.
type DvrStatus struct {
	IsRecording bool   `json:"is_recording"`
	ServiceName string `json:"service_name,omitempty"`
}

// GetDvrCapabilities returns the capabilities of the DVR system.
func GetDvrCapabilities(ctx context.Context, src DvrSource) (DvrCapabilities, error) {
	if src == nil {
		return DvrCapabilities{SeriesMode: "none"}, nil
	}

	canEdit := true // Defaults to true via fallback?
	cap, err := src.DetectTimerChange(ctx)
	if err == nil {
		canEdit = cap.Supported
	}

	return DvrCapabilities{
		CanDelete:        true,
		CanEdit:          canEdit,
		ReadBackVerify:   true,
		ConflictsPreview: true,
		ReceiverAware:    canEdit,
		SeriesSupported:  false,
		SeriesMode:       "none",
	}, nil
}

// GetDvrStatus returns the current recording status.
func GetDvrStatus(ctx context.Context, src DvrSource) (DvrStatus, error) {
	if src == nil {
		return DvrStatus{}, nil
	}

	info, err := src.GetStatusInfo(ctx)
	if err != nil {
		return DvrStatus{}, err
	}

	return DvrStatus{
		IsRecording: info.IsRecording == "true",
		ServiceName: info.ServiceName,
	}, nil
}
