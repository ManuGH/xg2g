// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerRecordingRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/recordings", "GetRecordings", wrapper.GetRecordings)
	register.add(http.MethodDelete, "/recordings/{recordingId}", "DeleteRecording", wrapper.DeleteRecording)
	register.add(http.MethodGet, "/recordings/{recordingId}/playlist.m3u8", "GetRecordingHLSPlaylist", wrapper.GetRecordingHLSPlaylist)
	register.add(http.MethodHead, "/recordings/{recordingId}/playlist.m3u8", "GetRecordingHLSPlaylistHead", wrapper.GetRecordingHLSPlaylistHead)
	register.add(http.MethodGet, "/recordings/{recordingId}/status", "GetRecordingsRecordingIdStatus", wrapper.GetRecordingsRecordingIdStatus)
	register.add(http.MethodGet, "/recordings/{recordingId}/stream-info", "GetRecordingPlaybackInfo", wrapper.GetRecordingPlaybackInfo)
	register.add(http.MethodPost, "/recordings/{recordingId}/stream-info", "PostRecordingPlaybackInfo", wrapper.PostRecordingPlaybackInfo)
	register.add(http.MethodGet, "/recordings/{recordingId}/stream.mp4", "StreamRecordingDirect", wrapper.StreamRecordingDirect)
	register.add(http.MethodHead, "/recordings/{recordingId}/stream.mp4", "ProbeRecordingMp4", wrapper.ProbeRecordingMp4)
	register.add(http.MethodGet, "/recordings/{recordingId}/timeshift.m3u8", "GetRecordingHLSTimeshift", wrapper.GetRecordingHLSTimeshift)
	register.add(http.MethodHead, "/recordings/{recordingId}/timeshift.m3u8", "GetRecordingHLSTimeshiftHead", wrapper.GetRecordingHLSTimeshiftHead)
	register.add(http.MethodGet, "/recordings/{recordingId}/{segment}", "GetRecordingHLSCustomSegment", wrapper.GetRecordingHLSCustomSegment)
	register.add(http.MethodHead, "/recordings/{recordingId}/{segment}", "GetRecordingHLSCustomSegmentHead", wrapper.GetRecordingHLSCustomSegmentHead)
}
