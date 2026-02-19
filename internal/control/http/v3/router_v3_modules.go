// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

// sessionsModuleRoutes groups all session-lifecycle/public stream routes.
type sessionsModuleRoutes interface {
	intentRoutes
	sessionRoutes
	streamRoutes
}

func registerSessionsModuleRoutes(register routeRegistrar, handler sessionsModuleRoutes) {
	registerIntentRoutes(register, handler)
	registerSessionRoutes(register, handler)
	registerStreamRoutes(register, handler)
}

// recordingsModuleRoutes groups recording listing/streaming routes.
type recordingsModuleRoutes interface {
	recordingRoutes
}

func registerRecordingsModuleRoutes(register routeRegistrar, handler recordingsModuleRoutes) {
	registerRecordingRoutes(register, handler)
}

// dvrModuleRoutes groups DVR and series-rule routes.
type dvrModuleRoutes interface {
	dvrRoutes
	seriesRoutes
}

func registerDVRModuleRoutes(register routeRegistrar, handler dvrModuleRoutes) {
	registerDVRRoutes(register, handler)
	registerSeriesRoutes(register, handler)
}

// configModuleRoutes groups mutable runtime config routes.
type configModuleRoutes interface {
	configRoutes
}

func registerConfigModuleRoutes(register routeRegistrar, handler configModuleRoutes) {
	registerConfigRoutes(register, handler)
}

// systemModuleRoutes groups system status, health, receiver, EPG, service and timer routes.
type systemModuleRoutes interface {
	authRoutes
	epgRoutes
	logRoutes
	receiverRoutes
	serviceRoutes
	systemRoutes
	timerRoutes
}

func registerSystemModuleRoutes(register routeRegistrar, handler systemModuleRoutes) {
	registerAuthRoutes(register, handler)
	registerEPGRoutes(register, handler)
	registerLogRoutes(register, handler)
	registerReceiverRoutes(register, handler)
	registerServiceRoutes(register, handler)
	registerSystemRoutes(register, handler)
	registerTimerRoutes(register, handler)
}
