package v3

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func sessionPlaybackClientSnapshot(session *model.SessionRecord) *model.PlaybackClientSnapshot {
	if session == nil {
		return nil
	}
	if session.PlaybackTrace != nil && session.PlaybackTrace.Client != nil {
		return session.PlaybackTrace.Client.Clone()
	}
	if session.ContextData == nil {
		return nil
	}

	snapshot := &model.PlaybackClientSnapshot{
		ClientFamily:       strings.TrimSpace(session.ContextData[model.CtxKeyClientFamily]),
		PreferredHLSEngine: strings.TrimSpace(session.ContextData[model.CtxKeyPreferredEngine]),
		DeviceType:         strings.TrimSpace(session.ContextData[model.CtxKeyDeviceType]),
	}
	if snapshot.ClientFamily == "" && snapshot.PreferredHLSEngine == "" && snapshot.DeviceType == "" {
		return nil
	}
	return snapshot
}

func mapPlaybackClientSnapshot(snapshot *model.PlaybackClientSnapshot) *PlaybackClientSnapshot {
	if snapshot == nil {
		return nil
	}

	dto := &PlaybackClientSnapshot{}
	if snapshot.CapturedAtUnix > 0 {
		capturedAtMs := snapshot.CapturedAtUnix * 1000
		dto.CapturedAtMs = &capturedAtMs
	}
	if capHash := strings.TrimSpace(snapshot.CapHash); capHash != "" {
		dto.CapHash = &capHash
	}
	if clientCapsSource := strings.TrimSpace(snapshot.ClientCapsSource); clientCapsSource != "" {
		dto.ClientCapsSource = &clientCapsSource
	}
	if clientFamily := strings.TrimSpace(snapshot.ClientFamily); clientFamily != "" {
		dto.ClientFamily = &clientFamily
	}
	if preferredEngine := strings.TrimSpace(snapshot.PreferredHLSEngine); preferredEngine != "" {
		dto.PreferredHlsEngine = &preferredEngine
	}
	if deviceType := strings.TrimSpace(snapshot.DeviceType); deviceType != "" {
		dto.DeviceType = &deviceType
	}
	if snapshot.RuntimeProbeUsed {
		runtimeProbeUsed := true
		dto.RuntimeProbeUsed = &runtimeProbeUsed
	}
	if snapshot.RuntimeProbeVersion > 0 {
		runtimeProbeVersion := snapshot.RuntimeProbeVersion
		dto.RuntimeProbeVersion = &runtimeProbeVersion
	}
	if snapshot.DeviceContext != nil {
		dto.DeviceContext = &PlaybackDeviceContext{
			Brand:        optionalStringPtr(snapshot.DeviceContext.Brand),
			Device:       optionalStringPtr(snapshot.DeviceContext.Device),
			Manufacturer: optionalStringPtr(snapshot.DeviceContext.Manufacturer),
			Model:        optionalStringPtr(snapshot.DeviceContext.Model),
			OsName:       optionalStringPtr(snapshot.DeviceContext.OSName),
			OsVersion:    optionalStringPtr(snapshot.DeviceContext.OSVersion),
			Platform:     optionalStringPtr(snapshot.DeviceContext.Platform),
			Product:      optionalStringPtr(snapshot.DeviceContext.Product),
		}
		if snapshot.DeviceContext.SDKInt > 0 {
			sdkInt := snapshot.DeviceContext.SDKInt
			dto.DeviceContext.SdkInt = &sdkInt
		}
	}
	if snapshot.NetworkContext != nil {
		dto.NetworkContext = &PlaybackNetworkContext{
			DownlinkKbps: optionalIntPtr(snapshot.NetworkContext.DownlinkKbps),
			Kind:         optionalStringPtr(snapshot.NetworkContext.Kind),
		}
		if snapshot.NetworkContext.InternetValidated != nil {
			v := *snapshot.NetworkContext.InternetValidated
			dto.NetworkContext.InternetValidated = &v
		}
		if snapshot.NetworkContext.Metered != nil {
			v := *snapshot.NetworkContext.Metered
			dto.NetworkContext.Metered = &v
		}
	}
	if dto.DeviceContext == nil && dto.NetworkContext == nil && dto.CapturedAtMs == nil && dto.CapHash == nil && dto.ClientCapsSource == nil && dto.ClientFamily == nil && dto.PreferredHlsEngine == nil && dto.DeviceType == nil && dto.RuntimeProbeUsed == nil && dto.RuntimeProbeVersion == nil {
		return nil
	}
	return dto
}

func mapPlaybackClientSummary(snapshot *model.PlaybackClientSnapshot) *PlaybackClientSummary {
	if snapshot == nil {
		return nil
	}

	dto := &PlaybackClientSummary{
		CapHash:             optionalStringPtr(snapshot.CapHash),
		ClientCapsSource:    optionalStringPtr(snapshot.ClientCapsSource),
		ClientFamily:        optionalStringPtr(snapshot.ClientFamily),
		PreferredHlsEngine:  optionalStringPtr(snapshot.PreferredHLSEngine),
		DeviceType:          optionalStringPtr(snapshot.DeviceType),
		RuntimeProbeVersion: optionalIntPtr(snapshot.RuntimeProbeVersion),
	}

	if snapshot.DeviceContext != nil {
		dto.Platform = optionalStringPtr(snapshot.DeviceContext.Platform)
		dto.OsName = optionalStringPtr(snapshot.DeviceContext.OSName)
		dto.OsVersion = optionalStringPtr(snapshot.DeviceContext.OSVersion)
		dto.Model = optionalStringPtr(snapshot.DeviceContext.Model)
	}
	if snapshot.NetworkContext != nil {
		dto.NetworkKind = optionalStringPtr(snapshot.NetworkContext.Kind)
	}

	if dto.CapHash == nil && dto.ClientCapsSource == nil && dto.ClientFamily == nil && dto.PreferredHlsEngine == nil && dto.DeviceType == nil && dto.Platform == nil && dto.OsName == nil && dto.OsVersion == nil && dto.Model == nil && dto.NetworkKind == nil && dto.RuntimeProbeVersion == nil {
		return nil
	}
	return dto
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalIntPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
