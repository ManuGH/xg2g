package capreg

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func (s *SqliteStore) RememberHost(ctx context.Context, snapshot HostSnapshot) error {
	snapshot = canonicalHostSnapshot(snapshot)
	fingerprint := snapshot.Identity.Fingerprint()
	if fingerprint == "" {
		return nil
	}
	runtimeJSON, err := json.Marshal(snapshot.Runtime)
	if err != nil {
		return err
	}
	encoderJSON, err := json.Marshal(snapshot.EncoderCapabilities)
	if err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
	INSERT INTO capability_hosts (
		host_fingerprint, hostname, os_name, os_version, architecture, runtime_json, encoder_caps_json, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(host_fingerprint) DO UPDATE SET
		hostname = excluded.hostname,
		os_name = excluded.os_name,
		os_version = excluded.os_version,
		architecture = excluded.architecture,
		runtime_json = excluded.runtime_json,
		encoder_caps_json = excluded.encoder_caps_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		fingerprint,
		snapshot.Identity.Hostname,
		snapshot.Identity.OSName,
		snapshot.Identity.OSVersion,
		snapshot.Identity.Architecture,
		string(runtimeJSON),
		string(encoderJSON),
		snapshot.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) RememberDevice(ctx context.Context, snapshot DeviceSnapshot) error {
	snapshot = canonicalDeviceSnapshot(snapshot)
	fingerprint := snapshot.Identity.Fingerprint()
	if fingerprint == "" || snapshot.Capabilities.CapabilitiesVersion == 0 {
		return nil
	}
	capsJSON, err := json.Marshal(snapshot.Capabilities)
	if err != nil {
		return err
	}
	var networkJSON any
	if snapshot.Network != nil {
		payload, err := json.Marshal(snapshot.Network)
		if err != nil {
			return err
		}
		networkJSON = string(payload)
	}

	deviceCtx := snapshot.Identity.DeviceContext
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO capability_devices (
			device_fingerprint, client_family, client_caps_source, device_type, brand, product, device_name, platform, manufacturer, model, os_name, os_version, sdk_int,
			capabilities_json, capabilities_hash, network_json, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_fingerprint) DO UPDATE SET
			client_family = excluded.client_family,
			client_caps_source = excluded.client_caps_source,
			device_type = excluded.device_type,
			brand = excluded.brand,
			product = excluded.product,
			device_name = excluded.device_name,
			platform = excluded.platform,
			manufacturer = excluded.manufacturer,
			model = excluded.model,
		os_name = excluded.os_name,
		os_version = excluded.os_version,
		sdk_int = excluded.sdk_int,
		capabilities_json = excluded.capabilities_json,
		capabilities_hash = excluded.capabilities_hash,
		network_json = excluded.network_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		fingerprint,
		snapshot.Identity.ClientFamily,
		snapshot.Identity.ClientCapsSource,
		snapshot.Identity.DeviceType,
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Brand }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Product }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Device }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Platform }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Manufacturer }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Model }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.OSName }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.OSVersion }),
		deviceContextInt(deviceCtx, func(v *capabilities.DeviceContext) int { return v.SDKInt }),
		string(capsJSON),
		HashCapabilitiesSnapshot(snapshot.Capabilities),
		networkJSON,
		snapshot.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) RememberSource(ctx context.Context, snapshot SourceSnapshot) error {
	snapshot = canonicalSourceSnapshot(snapshot)
	fingerprint := snapshot.Fingerprint()
	if fingerprint == "" {
		return nil
	}
	problemFlagsJSON, err := json.Marshal(snapshot.ProblemFlags)
	if err != nil {
		return err
	}
	var receiverContextJSON any
	if snapshot.ReceiverContext != nil {
		payload, err := json.Marshal(snapshot.ReceiverContext)
		if err != nil {
			return err
		}
		receiverContextJSON = string(payload)
	}

	interlaced := 0
	if snapshot.Interlaced {
		interlaced = 1
	}

	_, err = s.DB.ExecContext(ctx, `
	INSERT INTO capability_sources (
		source_fingerprint, subject_kind, origin, container, video_codec, audio_codec, bitrate_confidence, bitrate_bucket, width, height, fps, signal_fps, interlaced, problem_flags_json, receiver_context_json, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(source_fingerprint) DO UPDATE SET
		subject_kind = excluded.subject_kind,
		origin = excluded.origin,
		container = excluded.container,
		video_codec = excluded.video_codec,
		audio_codec = excluded.audio_codec,
		bitrate_confidence = excluded.bitrate_confidence,
		bitrate_bucket = excluded.bitrate_bucket,
		width = excluded.width,
		height = excluded.height,
		fps = excluded.fps,
		signal_fps = excluded.signal_fps,
		interlaced = excluded.interlaced,
		problem_flags_json = excluded.problem_flags_json,
		receiver_context_json = excluded.receiver_context_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		fingerprint,
		snapshot.SubjectKind,
		snapshot.Origin,
		snapshot.Container,
		snapshot.VideoCodec,
		snapshot.AudioCodec,
		snapshot.BitrateConfidence,
		snapshot.BitrateBucket,
		snapshot.Width,
		snapshot.Height,
		snapshot.FPS,
		snapshot.SignalFPS,
		interlaced,
		string(problemFlagsJSON),
		receiverContextJSON,
		snapshot.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) LookupCapabilities(ctx context.Context, identity DeviceIdentity) (capabilities.PlaybackCapabilities, bool, error) {
	fingerprint := identity.Fingerprint()
	if fingerprint == "" {
		return capabilities.PlaybackCapabilities{}, false, nil
	}

	var capsJSON string
	err := s.DB.QueryRowContext(ctx, `
	SELECT capabilities_json
	FROM capability_devices
	WHERE device_fingerprint = ?
	`, fingerprint).Scan(&capsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return capabilities.PlaybackCapabilities{}, false, nil
		}
		return capabilities.PlaybackCapabilities{}, false, err
	}

	var caps capabilities.PlaybackCapabilities
	if err := json.Unmarshal([]byte(capsJSON), &caps); err != nil {
		return capabilities.PlaybackCapabilities{}, false, err
	}
	return capabilities.CanonicalizeCapabilities(caps), true, nil
}

func (s *SqliteStore) LookupDecisionObservation(ctx context.Context, requestID string) (PlaybackObservation, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return PlaybackObservation{}, false, nil
	}

	row := s.DB.QueryRowContext(ctx, `
	SELECT
		observed_at_ms,
		request_id,
		observation_kind,
		outcome,
		session_id,
		source_ref,
		source_fingerprint,
		subject_kind,
		requested_intent,
		resolved_intent,
		mode,
		selected_container,
		selected_video_codec,
		selected_audio_codec,
		source_width,
		source_height,
		source_fps,
		host_fingerprint,
		device_fingerprint,
		client_caps_hash,
		feedback_event,
		feedback_code,
		feedback_message,
		network_kind,
		network_metered,
		network_downlink_kbps
	FROM capability_observations
	WHERE request_id = ? AND observation_kind = 'decision'
	ORDER BY observed_at_ms DESC, id DESC
	LIMIT 1
	`, requestID)

	observation, err := scanObservation(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return PlaybackObservation{}, false, nil
		}
		return PlaybackObservation{}, false, err
	}
	return observation, true, nil
}

func (s *SqliteStore) LookupRecentFeedbackSummary(ctx context.Context, query FeedbackSummaryQuery) (FeedbackSummary, bool, error) {
	query = canonicalFeedbackSummaryQuery(query)
	if query.SourceFingerprint == "" || query.DeviceFingerprint == "" || query.HostFingerprint == "" {
		return FeedbackSummary{}, false, nil
	}

	observations, err := s.LookupRecentFeedbackObservations(ctx, query)
	if err != nil {
		return FeedbackSummary{}, false, err
	}
	if len(observations) == 0 {
		return FeedbackSummary{}, false, nil
	}
	return summarizeFeedbackObservations(observations), true, nil
}

func (s *SqliteStore) LookupRecentFeedbackObservations(ctx context.Context, query FeedbackSummaryQuery) ([]PlaybackObservation, error) {
	query = canonicalFeedbackSummaryQuery(query)
	if query.SourceFingerprint == "" || query.DeviceFingerprint == "" || query.HostFingerprint == "" {
		return nil, nil
	}

	rows, err := s.DB.QueryContext(ctx, `
	SELECT
		observed_at_ms,
		request_id,
		observation_kind,
		outcome,
		session_id,
		source_ref,
		source_fingerprint,
		subject_kind,
		requested_intent,
		resolved_intent,
		mode,
		selected_container,
		selected_video_codec,
		selected_audio_codec,
		source_width,
		source_height,
		source_fps,
		host_fingerprint,
		device_fingerprint,
		client_caps_hash,
		feedback_event,
		feedback_code,
		feedback_message,
		network_kind,
		network_metered,
		network_downlink_kbps
	FROM capability_observations
	WHERE observation_kind = 'feedback'
		AND source_fingerprint = ?
		AND device_fingerprint = ?
		AND host_fingerprint = ?
		AND (? = '' OR subject_kind = ?)
		AND observed_at_ms >= ?
	ORDER BY observed_at_ms DESC, id DESC
	LIMIT ?
	`,
		query.SourceFingerprint,
		query.DeviceFingerprint,
		query.HostFingerprint,
		query.SubjectKind,
		query.SubjectKind,
		query.Since.UnixMilli(),
		query.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	observations := make([]PlaybackObservation, 0, query.Limit)
	for rows.Next() {
		observation, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return observations, nil
}

func (s *SqliteStore) RememberPlaybackPolicyState(ctx context.Context, state PlaybackPolicyState) error {
	state = canonicalPlaybackPolicyState(state)
	if state.Fingerprint() == "" {
		return nil
	}
	payload, err := json.Marshal(state.Confidence)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
	INSERT INTO capability_policy_state (
		subject_kind, source_fingerprint, device_fingerprint, host_fingerprint, max_quality_rung, confidence_json, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(subject_kind, source_fingerprint, device_fingerprint, host_fingerprint) DO UPDATE SET
		max_quality_rung = excluded.max_quality_rung,
		confidence_json = excluded.confidence_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		state.SubjectKind,
		state.SourceFingerprint,
		state.DeviceFingerprint,
		state.HostFingerprint,
		string(state.MaxQualityRung),
		string(payload),
		state.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) LookupPlaybackPolicyState(ctx context.Context, query PlaybackPolicyStateQuery) (PlaybackPolicyState, bool, error) {
	query = canonicalPlaybackPolicyStateQuery(query)
	if queryFingerprint(query) == "" {
		return PlaybackPolicyState{}, false, nil
	}

	var state PlaybackPolicyState
	var payload string
	var updatedAtMS int64
	err := s.DB.QueryRowContext(ctx, `
	SELECT subject_kind, source_fingerprint, device_fingerprint, host_fingerprint, max_quality_rung, confidence_json, updated_at_ms
	FROM capability_policy_state
	WHERE subject_kind = ? AND source_fingerprint = ? AND device_fingerprint = ? AND host_fingerprint = ?
	`,
		query.SubjectKind,
		query.SourceFingerprint,
		query.DeviceFingerprint,
		query.HostFingerprint,
	).Scan(
		&state.SubjectKind,
		&state.SourceFingerprint,
		&state.DeviceFingerprint,
		&state.HostFingerprint,
		&state.MaxQualityRung,
		&payload,
		&updatedAtMS,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return PlaybackPolicyState{}, false, nil
		}
		return PlaybackPolicyState{}, false, err
	}
	if err := json.Unmarshal([]byte(payload), &state.Confidence); err != nil {
		return PlaybackPolicyState{}, false, err
	}
	state.MaxQualityRung = playbackprofile.NormalizeQualityRung(string(state.MaxQualityRung))
	if updatedAtMS > 0 {
		state.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()
	}
	return state, true, nil
}

func (s *SqliteStore) RecordObservation(ctx context.Context, observation PlaybackObservation) error {
	observation = canonicalObservation(observation)
	_, err := s.DB.ExecContext(ctx, `
	INSERT INTO capability_observations (
		observed_at_ms, request_id, observation_kind, outcome, session_id, source_ref, source_fingerprint, subject_kind, requested_intent, resolved_intent, mode,
		selected_container, selected_video_codec, selected_audio_codec, source_width, source_height, source_fps,
		host_fingerprint, device_fingerprint, client_caps_hash, feedback_event, feedback_code, feedback_message, network_kind, network_metered, network_downlink_kbps
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		observation.ObservedAt.UnixMilli(),
		observation.RequestID,
		observation.ObservationKind,
		observation.Outcome,
		observation.SessionID,
		observation.SourceRef,
		observation.SourceFingerprint,
		observation.SubjectKind,
		observation.RequestedIntent,
		observation.ResolvedIntent,
		observation.Mode,
		observation.SelectedContainer,
		observation.SelectedVideoCodec,
		observation.SelectedAudioCodec,
		observation.SourceWidth,
		observation.SourceHeight,
		observation.SourceFPS,
		observation.HostFingerprint,
		observation.DeviceFingerprint,
		observation.ClientCapsHash,
		observation.FeedbackEvent,
		observation.FeedbackCode,
		observation.FeedbackMessage,
		networkKind(observation.Network),
		networkMetered(observation.Network),
		networkDownlink(observation.Network),
	)
	return err
}
