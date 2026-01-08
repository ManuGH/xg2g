package read

import (
	"context"

	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// ScanSource defines the minimal interface required to fetch scan status.
type ScanSource interface {
	GetStatus() scan.ScanStatus
}

// ScanStatus represents the public-facing status of a capability scan.
type ScanStatus struct {
	State           string `json:"state"`
	StartedAt       int64  `json:"started_at,omitempty"`
	FinishedAt      int64  `json:"finished_at,omitempty"`
	TotalChannels   int    `json:"total_channels"`
	ScannedChannels int    `json:"scanned_channels"`
	UpdatedCount    int    `json:"updated_count"`
	LastError       string `json:"last_error,omitempty"`
}

// GetScanStatus returns the current status of the channel scanner.
func GetScanStatus(ctx context.Context, src ScanSource) (ScanStatus, error) {
	if src == nil {
		return ScanStatus{State: "unavailable"}, nil
	}

	st := src.GetStatus()

	return ScanStatus{
		State:           st.State,
		StartedAt:       st.StartedAt,
		FinishedAt:      st.FinishedAt,
		TotalChannels:   st.TotalChannels,
		ScannedChannels: st.ScannedChannels,
		UpdatedCount:    st.UpdatedCount,
		LastError:       st.LastError,
	}, nil
}
