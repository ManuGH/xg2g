// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// Limits of one telemetry upload. They mirror the PlaybackTelemetry* schemas in
// openapi.yaml, which the server does not validate at runtime, so they are
// enforced here.
const (
	maxPlaybackTelemetryBodyBytes = 64 << 10
	maxPlaybackTelemetryEvents    = 20
	maxPlaybackTelemetryMetrics   = 48
	maxPlaybackTelemetryReasons   = 8
)

var (
	// lowerCamelCase, as the schema documents. Keys become log field names, so
	// anything else is refused rather than escaped.
	playbackTelemetryMetricKey = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,47}$`)
	playbackTelemetryReason    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// PostPlaybackTelemetry implements POST /telemetry/playback.
//
// The server delivers a stream and can see only that it delivered it. Whether
// the picture moved and the sound kept playing is known on the client alone, so
// the client reports it here. Every event is written to the log, correlated by
// zap id and service reference with the server's own records of that stream,
// and counted; nothing is stored.
func (s *Server) PostPlaybackTelemetry(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPlaybackTelemetryBodyBytes)

	var batch PlaybackTelemetryBatch
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&batch); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid telemetry batch")
		return
	}
	if err := validatePlaybackTelemetry(batch); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}

	logger := log.WithComponentFromContext(r.Context(), "client_telemetry")
	deviceID := ""
	if principal := auth.PrincipalFromContext(r.Context()); principal != nil {
		deviceID = principal.DeviceID
	}
	for _, ev := range batch.Events {
		logPlaybackTelemetryEvent(logger, batch.Client, deviceID, ev)
		metrics.RecordClientPlaybackEvent(string(batch.Client.Platform), string(ev.Kind))
	}

	w.WriteHeader(http.StatusAccepted)
}

// validatePlaybackTelemetry enforces what the schema declares. A batch is
// accepted whole or refused whole: a client sending one bad event has a bug,
// and logging the rest of its batch would hide it.
func validatePlaybackTelemetry(batch PlaybackTelemetryBatch) error {
	switch batch.Client.Platform {
	case PlaybackTelemetryClientPlatformIos, PlaybackTelemetryClientPlatformTvos,
		PlaybackTelemetryClientPlatformAndroid, PlaybackTelemetryClientPlatformWeb:
	default:
		return fmt.Errorf("client.platform %q is not a known platform", batch.Client.Platform)
	}
	if err := checkOptionalLength("client.appVersion", batch.Client.AppVersion, 64); err != nil {
		return err
	}
	if err := checkOptionalLength("client.build", batch.Client.Build, 64); err != nil {
		return err
	}
	if err := checkOptionalLength("client.device", batch.Client.Device, 64); err != nil {
		return err
	}

	if len(batch.Events) == 0 {
		return errors.New("events must not be empty")
	}
	if len(batch.Events) > maxPlaybackTelemetryEvents {
		return fmt.Errorf("events holds %d entries, at most %d are accepted", len(batch.Events), maxPlaybackTelemetryEvents)
	}
	for i, ev := range batch.Events {
		if err := validatePlaybackTelemetryEvent(ev); err != nil {
			return fmt.Errorf("events[%d]: %w", i, err)
		}
	}
	return nil
}

func validatePlaybackTelemetryEvent(ev PlaybackTelemetryEvent) error {
	switch ev.Kind {
	case PlaybackTelemetryEventKindSessionStart, PlaybackTelemetryEventKindHeartbeat,
		PlaybackTelemetryEventKindDegraded, PlaybackTelemetryEventKindSessionEnd:
	default:
		return fmt.Errorf("kind %q is not a known event kind", ev.Kind)
	}
	if ev.OccurredAt.IsZero() {
		return errors.New("occurredAt is required")
	}
	for _, field := range []struct {
		name  string
		value *string
		limit int
	}{
		{"zapId", ev.ZapId, 64},
		{"serviceRef", ev.ServiceRef, 128},
		{"sessionId", ev.SessionId, 64},
		{"detail", ev.Detail, 512},
	} {
		if err := checkOptionalLength(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	if ev.Reasons != nil {
		if len(*ev.Reasons) > maxPlaybackTelemetryReasons {
			return fmt.Errorf("reasons holds %d entries, at most %d are accepted", len(*ev.Reasons), maxPlaybackTelemetryReasons)
		}
		for _, reason := range *ev.Reasons {
			if !playbackTelemetryReason.MatchString(reason) {
				return fmt.Errorf("reason %q is not a lower_snake_case identifier", reason)
			}
		}
	}
	if ev.Metrics != nil {
		if len(*ev.Metrics) > maxPlaybackTelemetryMetrics {
			return fmt.Errorf("metrics holds %d entries, at most %d are accepted", len(*ev.Metrics), maxPlaybackTelemetryMetrics)
		}
		for key, value := range *ev.Metrics {
			if !playbackTelemetryMetricKey.MatchString(key) {
				return fmt.Errorf("metric key %q is not lowerCamelCase", key)
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("metric %q is not a finite number", key)
			}
		}
	}
	return nil
}

func checkOptionalLength(name string, value *string, limit int) error {
	if value != nil && utf8.RuneCountInString(*value) > limit {
		return fmt.Errorf("%s exceeds %d characters", name, limit)
	}
	return nil
}

// logPlaybackTelemetryEvent writes one event. A degraded window is a warning,
// because it is the client saying the viewer saw a fault; everything else is the
// record around it and is logged at info.
func logPlaybackTelemetryEvent(logger zerolog.Logger, client PlaybackTelemetryClient, deviceID string, ev PlaybackTelemetryEvent) {
	entry := logger.Info()
	if ev.Kind == PlaybackTelemetryEventKindDegraded {
		entry = logger.Warn()
	}

	entry = entry.
		Str("event", "client.playback."+string(ev.Kind)).
		Str("platform", string(client.Platform)).
		Time("occurred_at", ev.OccurredAt.UTC().Truncate(time.Millisecond))
	entry = withOptionalStr(entry, "app_version", client.AppVersion)
	entry = withOptionalStr(entry, "build", client.Build)
	entry = withOptionalStr(entry, "device", client.Device)
	if deviceID != "" {
		entry = entry.Str("device_id", deviceID)
	}
	entry = withOptionalStr(entry, "zap_id", ev.ZapId)
	entry = withOptionalStr(entry, "service_ref", ev.ServiceRef)
	entry = withOptionalStr(entry, "session_id", ev.SessionId)
	entry = withOptionalStr(entry, "detail", ev.Detail)
	if ev.Reasons != nil && len(*ev.Reasons) > 0 {
		entry = entry.Strs("reasons", *ev.Reasons)
	}
	if ev.Metrics != nil && len(*ev.Metrics) > 0 {
		keys := make([]string, 0, len(*ev.Metrics))
		for key := range *ev.Metrics {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		dict := zerolog.Dict()
		for _, key := range keys {
			dict = dict.Float64(key, (*ev.Metrics)[key])
		}
		entry = entry.Dict("metrics", dict)
	}
	entry.Msg("client playback telemetry")
}

func withOptionalStr(entry *zerolog.Event, key string, value *string) *zerolog.Event {
	if value == nil || *value == "" {
		return entry
	}
	return entry.Str(key, *value)
}
