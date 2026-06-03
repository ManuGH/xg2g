package capreg

import (
	"database/sql"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

type scanner interface {
	Scan(dest ...any) error
}

func scanObservation(row scanner) (PlaybackObservation, error) {
	var (
		observedAtMS         int64
		networkKindValue     string
		networkMeteredValue  sql.NullInt64
		networkDownlinkValue int
	)
	observation := PlaybackObservation{}
	if err := row.Scan(
		&observedAtMS,
		&observation.RequestID,
		&observation.ObservationKind,
		&observation.Outcome,
		&observation.SessionID,
		&observation.SourceRef,
		&observation.SourceFingerprint,
		&observation.SubjectKind,
		&observation.RequestedIntent,
		&observation.ResolvedIntent,
		&observation.Mode,
		&observation.SelectedContainer,
		&observation.SelectedVideoCodec,
		&observation.SelectedAudioCodec,
		&observation.SourceWidth,
		&observation.SourceHeight,
		&observation.SourceFPS,
		&observation.HostFingerprint,
		&observation.DeviceFingerprint,
		&observation.ClientCapsHash,
		&observation.FeedbackEvent,
		&observation.FeedbackCode,
		&observation.FeedbackMessage,
		&networkKindValue,
		&networkMeteredValue,
		&networkDownlinkValue,
	); err != nil {
		return PlaybackObservation{}, err
	}
	observation.ObservedAt = time.UnixMilli(observedAtMS).UTC()
	if networkKindValue != "" || networkDownlinkValue > 0 || networkMeteredValue.Valid {
		network := &capabilities.NetworkContext{
			Kind:         networkKindValue,
			DownlinkKbps: networkDownlinkValue,
		}
		if networkMeteredValue.Valid {
			metered := networkMeteredValue.Int64 != 0
			network.Metered = &metered
		}
		observation.Network = network
	}
	return canonicalObservation(observation), nil
}

func deviceContextString(ctx *capabilities.DeviceContext, pick func(*capabilities.DeviceContext) string) string {
	if ctx == nil {
		return ""
	}
	return pick(ctx)
}

func deviceContextInt(ctx *capabilities.DeviceContext, pick func(*capabilities.DeviceContext) int) int {
	if ctx == nil {
		return 0
	}
	return pick(ctx)
}

func networkKind(ctx *capabilities.NetworkContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.Kind
}

func networkMetered(ctx *capabilities.NetworkContext) any {
	if ctx == nil || ctx.Metered == nil {
		return nil
	}
	if *ctx.Metered {
		return 1
	}
	return 0
}

func networkDownlink(ctx *capabilities.NetworkContext) int {
	if ctx == nil {
		return 0
	}
	return ctx.DownlinkKbps
}
