// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerRecordingRoutes(register routeRegistrar, handler recordingRoutes) {
	register.add(http.MethodGet, "/recordings", "GetRecordings", handler.GetRecordings)
	register.add(http.MethodDelete, "/recordings/{recordingId}", "DeleteRecording", handler.DeleteRecording)
	register.add(http.MethodGet, "/recordings/{recordingId}/playlist.m3u8", "GetRecordingHLSPlaylist", handler.GetRecordingHLSPlaylist)
	register.add(http.MethodHead, "/recordings/{recordingId}/playlist.m3u8", "GetRecordingHLSPlaylistHead", handler.GetRecordingHLSPlaylistHead)
	register.add(http.MethodGet, "/recordings/{recordingId}/status", "GetRecordingsRecordingIdStatus", handler.GetRecordingsRecordingIdStatus)
	register.add(http.MethodGet, "/recordings/{recordingId}/stream-info", "GetRecordingPlaybackInfo", handler.GetRecordingPlaybackInfo)
	register.add(http.MethodPost, "/recordings/{recordingId}/stream-info", "PostRecordingPlaybackInfo", handler.PostRecordingPlaybackInfo)
	register.add(http.MethodGet, "/recordings/{recordingId}/stream.mp4", "StreamRecordingDirect", handler.StreamRecordingDirect)
	register.add(http.MethodHead, "/recordings/{recordingId}/stream.mp4", "ProbeRecordingMp4", handler.ProbeRecordingMp4)
	register.add(http.MethodGet, "/recordings/{recordingId}/timeshift.m3u8", "GetRecordingHLSTimeshift", handler.GetRecordingHLSTimeshift)
	register.add(http.MethodHead, "/recordings/{recordingId}/timeshift.m3u8", "GetRecordingHLSTimeshiftHead", handler.GetRecordingHLSTimeshiftHead)
	register.add(http.MethodGet, "/recordings/{recordingId}/{segment}", "GetRecordingHLSCustomSegment", handler.GetRecordingHLSCustomSegment)
	register.add(http.MethodHead, "/recordings/{recordingId}/{segment}", "GetRecordingHLSCustomSegmentHead", handler.GetRecordingHLSCustomSegmentHead)
}
