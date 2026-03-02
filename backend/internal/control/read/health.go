package read

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/health"
)

// HealthInfo captures the system health state for read-model consumption.
// It is intended for internal use by API layers to map to their specific DTOs.
type HealthInfo struct {
	OverallStatus   string
	Version         string
	UptimeSeconds   int64
	Timestamp       time.Time
	ReceiverStatus  string
	EpgStatus       string
	MissingChannels int
}

// HealthSource defines the minimal interface required to fetch health data.
type HealthSource interface {
	Health(ctx context.Context, detailed bool) health.HealthResponse
}

// GetHealthInfo assembles health information from the provided HealthSource.
func GetHealthInfo(ctx context.Context, src HealthSource) (HealthInfo, error) {
	if src == nil {
		return HealthInfo{}, nil
	}

	respH := src.Health(ctx, true)

	info := HealthInfo{
		OverallStatus: string(respH.Status),
		Version:       respH.Version,
		UptimeSeconds: respH.Uptime,
		Timestamp:     respH.Timestamp,
	}

	// Map Receiver Status
	if res, ok := respH.Checks["receiver_connection"]; ok {
		info.ReceiverStatus = string(res.Status)
	} else {
		info.ReceiverStatus = string(health.StatusUnhealthy)
	}

	// Map EPG Status
	if res, ok := respH.Checks["epg_status"]; ok {
		info.EpgStatus = string(res.Status)
	} else {
		info.EpgStatus = string(health.StatusUnhealthy)
	}

	// MissingChannels logic could be added here if/when HealthManager supports it.
	info.MissingChannels = 0

	return info, nil
}
