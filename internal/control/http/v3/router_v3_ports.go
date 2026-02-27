// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

type authRoutes interface {
	CreateSession(w http.ResponseWriter, r *http.Request)
}

type dvrRoutes interface {
	GetDvrCapabilities(w http.ResponseWriter, r *http.Request)
	GetDvrStatus(w http.ResponseWriter, r *http.Request)
}

type epgRoutes interface {
	GetEpg(w http.ResponseWriter, r *http.Request)
}

type intentRoutes interface {
	CreateIntent(w http.ResponseWriter, r *http.Request)
}

type livePlaybackRoutes interface {
	PostLivePlaybackInfo(w http.ResponseWriter, r *http.Request)
}

type logRoutes interface {
	GetLogs(w http.ResponseWriter, r *http.Request)
}

type receiverRoutes interface {
	GetReceiverCurrent(w http.ResponseWriter, r *http.Request)
}

type recordingRoutes interface {
	GetRecordings(w http.ResponseWriter, r *http.Request)
	DeleteRecording(w http.ResponseWriter, r *http.Request)
	GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request)
	GetRecordingHLSPlaylistHead(w http.ResponseWriter, r *http.Request)
	GetRecordingsRecordingIdStatus(w http.ResponseWriter, r *http.Request)
	GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request)
	PostRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request)
	StreamRecordingDirect(w http.ResponseWriter, r *http.Request)
	ProbeRecordingMp4(w http.ResponseWriter, r *http.Request)
	GetRecordingHLSTimeshift(w http.ResponseWriter, r *http.Request)
	GetRecordingHLSTimeshiftHead(w http.ResponseWriter, r *http.Request)
	GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request)
	GetRecordingHLSCustomSegmentHead(w http.ResponseWriter, r *http.Request)
}

type seriesRoutes interface {
	GetSeriesRules(w http.ResponseWriter, r *http.Request)
	CreateSeriesRule(w http.ResponseWriter, r *http.Request)
	RunAllSeriesRules(w http.ResponseWriter, r *http.Request)
	DeleteSeriesRule(w http.ResponseWriter, r *http.Request)
	UpdateSeriesRule(w http.ResponseWriter, r *http.Request)
	RunSeriesRule(w http.ResponseWriter, r *http.Request)
}

type serviceRoutes interface {
	GetServices(w http.ResponseWriter, r *http.Request)
	GetServicesBouquets(w http.ResponseWriter, r *http.Request)
	PostServicesNowNext(w http.ResponseWriter, r *http.Request)
	PostServicesIdToggle(w http.ResponseWriter, r *http.Request)
}

type sessionRoutes interface {
	ListSessions(w http.ResponseWriter, r *http.Request)
	GetSessionState(w http.ResponseWriter, r *http.Request)
	ServeHLS(w http.ResponseWriter, r *http.Request)
	ServeHLSHead(w http.ResponseWriter, r *http.Request)
	ReportPlaybackFeedback(w http.ResponseWriter, r *http.Request)
}

type streamRoutes interface {
	GetStreams(w http.ResponseWriter, r *http.Request)
	DeleteStreamsId(w http.ResponseWriter, r *http.Request)
}

type configRoutes interface {
	GetSystemConfig(w http.ResponseWriter, r *http.Request)
	PutSystemConfig(w http.ResponseWriter, r *http.Request)
}

type systemRoutes interface {
	GetSystemHealth(w http.ResponseWriter, r *http.Request)
	GetSystemHealthz(w http.ResponseWriter, r *http.Request)
	GetSystemInfo(w http.ResponseWriter, r *http.Request)
	PostSystemRefresh(w http.ResponseWriter, r *http.Request)
	GetSystemScanStatus(w http.ResponseWriter, r *http.Request)
	TriggerSystemScan(w http.ResponseWriter, r *http.Request)
}

type timerRoutes interface {
	GetTimers(w http.ResponseWriter, r *http.Request)
	AddTimer(w http.ResponseWriter, r *http.Request)
	PreviewConflicts(w http.ResponseWriter, r *http.Request)
	DeleteTimer(w http.ResponseWriter, r *http.Request)
	GetTimer(w http.ResponseWriter, r *http.Request)
	UpdateTimer(w http.ResponseWriter, r *http.Request)
}
