// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package metrics provides Prometheus metrics collection.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Business metrics
	bouquetsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_bouquets_total",
		Help: "Total number of bouquets discovered (last refresh)",
	})

	bouquetDiscoveryErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_bouquet_discovery_errors_total",
		Help: "Total number of bouquet discovery failures",
	})

	servicesDiscovered = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_services_discovered",
		Help: "Number of services discovered per bouquet (last refresh)",
	}, []string{"bouquet"})

	servicesResolutionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_services_resolution_total",
		Help: "Service resolution attempts per bouquet by outcome",
	}, []string{"bouquet", "outcome"}) // outcome=success|failure

	streamURLBuildTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_stream_url_build_total",
		Help: "Stream URL generation attempts by outcome",
	}, []string{"outcome"}) // outcome=success|failure

	channelTypes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_channel_types",
		Help: "Channels by type in last refresh",
	}, []string{"type"}) // type=hd|sd|radio|unknown

	xmltvChannelsWritten = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_xmltv_channels_written",
		Help: "Number of channels written to XMLTV in last refresh",
	})
	xmltvWriteErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_xmltv_write_errors_total",
		Help: "Total number of XMLTV write failures",
	})
	xmltvEnabled = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_xmltv_enabled",
		Help: "Whether XMLTV generation is enabled (1) or disabled (0)",
	})

	// Operational metrics
	configValidationErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_config_validation_errors_total",
		Help: "Total number of configuration validation errors",
	})

	// Error metrics for refresh stages
	refreshFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_refresh_failures_total",
		Help: "Total number of refresh failures by stage",
	}, []string{"stage"}) // stage=config|bouquets|services|streamurl|write_m3u|xmltv

	// EPG metrics
	epgRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_epg_requests_total",
		Help: "Total number of EPG requests made",
	}, []string{"status"}) // status=success|error|timeout

	epgProgrammesCollected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_epg_programmes_collected",
		Help: "Total number of EPG programmes collected in last refresh",
	})

	epgChannelsWithData = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_epg_channels_with_data",
		Help: "Number of channels that have EPG data",
	})

	epgCollectionDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xg2g_epg_collection_duration_seconds",
		Help:    "Time spent collecting EPG data for all channels",
		Buckets: prometheus.DefBuckets,
	})

	// Playlist validity metrics
	playlistFileValid = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_playlist_file_valid",
		Help: "Whether playlist files exist and are readable (1=valid, 0=invalid)",
	}, []string{"type"}) // type=m3u|xmltv

	// Stream metrics
	activeStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_active_streams",
		Help: "Number of currently active streams",
	}, []string{"type"}) // type=direct|transcode|repair

	transcodeErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_transcode_errors_total",
		Help: "Total number of transcoding failures",
	})

	ffmpegRestartsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_ffmpeg_restarts_total",
		Help: "Total number of ffmpeg process restarts",
	})

	// Picon metrics
	piconFetchesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_picon_fetches_total",
		Help: "Total number of picon fetch attempts by result",
	}, []string{"result"}) // result=hit_disk|downloaded|notfound|error|negcache|dedup|dropped
)

func init() {
	// Initialize vector metrics to 0 so they appear in output
	activeStreams.WithLabelValues("direct")
	activeStreams.WithLabelValues("transcode")
	activeStreams.WithLabelValues("repair")

	// Phase 9: Initialize VOD metrics
	vodBuildsActive.WithLabelValues("ffmpeg")
}

// RecordBouquetsCount records the total number of bouquets discovered.
func RecordBouquetsCount(n int) { bouquetsTotal.Set(float64(n)) }

// IncBouquetDiscoveryError increments the bouquet discovery error counter.
func IncBouquetDiscoveryError() { bouquetDiscoveryErrors.Inc() }

// RecordServicesCount records the number of services discovered for a bouquet.
func RecordServicesCount(bouquet string, n int) {
	servicesDiscovered.WithLabelValues(bouquet).Set(float64(n))
}

// IncServicesResolution increments the services resolution counter by outcome.
func IncServicesResolution(bouquet, outcome string) {
	servicesResolutionTotal.WithLabelValues(bouquet, outcome).Inc()
}

// IncStreamURLBuild increments the stream URL build counter by outcome.
func IncStreamURLBuild(outcome string) { streamURLBuildTotal.WithLabelValues(outcome).Inc() }

// RecordChannelTypeCounts records the distribution of channel types.
func RecordChannelTypeCounts(hd, sd, radio, unknown int) {
	channelTypes.WithLabelValues("hd").Set(float64(hd))
	channelTypes.WithLabelValues("sd").Set(float64(sd))
	channelTypes.WithLabelValues("radio").Set(float64(radio))
	channelTypes.WithLabelValues("unknown").Set(float64(unknown))
}

// RecordXMLTV records XMLTV generation status and metrics.
func RecordXMLTV(enabled bool, channels int, writeErr error) {
	if enabled {
		xmltvEnabled.Set(1)
		xmltvChannelsWritten.Set(float64(channels))
		if writeErr != nil {
			xmltvWriteErrors.Inc()
		}
	} else {
		xmltvEnabled.Set(0)
		xmltvChannelsWritten.Set(0)
	}
}

// IncConfigValidationError increments the config validation error counter.
func IncConfigValidationError() { configValidationErrors.Inc() }

// IncRefreshFailure increments the refresh failure counter by stage.
func IncRefreshFailure(stage string) { refreshFailuresTotal.WithLabelValues(stage).Inc() }

// IncEPGChannelError increments the EPG error counter.
func IncEPGChannelError() {
	epgRequestsTotal.WithLabelValues("error").Inc()
}

// RecordEPGChannelSuccess records successful EPG channel operations.
func RecordEPGChannelSuccess(_ int) {
	epgRequestsTotal.WithLabelValues("success").Inc()
}

// RecordEPGCollection records EPG collection metrics including events and duration.
func RecordEPGCollection(totalProgrammes, channelsWithData int, duration float64) {
	epgProgrammesCollected.Set(float64(totalProgrammes))
	epgChannelsWithData.Set(float64(channelsWithData))
	epgCollectionDurationSeconds.Observe(duration)
}

// RecordPlaylistFileValidity records whether playlist files are valid (exist and readable).
func RecordPlaylistFileValidity(fileType string, valid bool) {
	if valid {
		playlistFileValid.WithLabelValues(fileType).Set(1)
	} else {
		playlistFileValid.WithLabelValues(fileType).Set(0)
	}
}

// IncActiveStreams increments the active stream gauge for a given type.
func IncActiveStreams(streamType string) {
	activeStreams.WithLabelValues(streamType).Inc()
}

// DecActiveStreams decrements the active stream gauge for a given type.
func DecActiveStreams(streamType string) {
	activeStreams.WithLabelValues(streamType).Dec()
}

// IncTranscodeError increments the transcode error counter.
func IncTranscodeError() {
	transcodeErrorsTotal.Inc()
}

// IncFFmpegRestart increments the ffmpeg restart counter.
func IncFFmpegRestart() {
	ffmpegRestartsTotal.Inc()
}

// IncPiconFetch increments the picon fetch counter by result.
func IncPiconFetch(result string) {
	piconFetchesTotal.WithLabelValues(result).Inc()
}

// P2.5 Observability Metrics

var (
	streamSessionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_stream_sessions_total",
		Help: "Total number of stream sessions by mode and outcome",
	}, []string{"mode", "outcome"}) // mode=direct|transcode|repair|hls_*, outcome=success|client_disconnect|ffmpeg_exit|...

	streamSessionDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_stream_session_duration_seconds",
		Help:    "Duration of stream sessions in seconds",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200}, // Up to 2h
	}, []string{"mode", "outcome"})

	procTerminateTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_proc_terminate_total",
		Help: "Total process group termination attempts by signal and outcome",
	}, []string{"sig", "outcome"}) // sig=SIGTERM|SIGKILL, outcome=sent|esrch|error

	procWaitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_proc_wait_total",
		Help: "Total process wait outcomes",
	}, []string{"outcome"}) // outcome=exit0|exit_nonzero|forced_exit0|forced_error

	hlsStartupSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_hls_startup_seconds",
		Help:    "Time until first HLS segment is ready",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
	}, []string{"profile"}) // profile=generic|safari|llhls
)

// ObserveStreamSession records a stream session's duration and outcome.
func ObserveStreamSession(mode, outcome string, duration float64) {
	streamSessionsTotal.WithLabelValues(mode, outcome).Inc()
	streamSessionDurationSeconds.WithLabelValues(mode, outcome).Observe(duration)
}

// IncProcTerminate records a process termination attempt.
func IncProcTerminate(sig, outcome string) {
	procTerminateTotal.WithLabelValues(sig, outcome).Inc()
}

// IncProcWait records a process wait outcome.
func IncProcWait(outcome string) {
	procWaitTotal.WithLabelValues(outcome).Inc()
}

// ObserveHLSStartup records HLS startup latency.
func ObserveHLSStartup(profile string, duration float64) {
	hlsStartupSeconds.WithLabelValues(profile).Observe(duration)
}

// Phase 9: VOD Metrics
var (
	vodBuildsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_vod_builds_active",
		Help: "Number of currently running VOD builds",
	}, []string{"type"}) // type=ffmpeg

	vodBuildsRejectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_vod_builds_rejected_total",
		Help: "Total number of VOD builds rejected (429)",
	}, []string{"reason"})

	vodBuildsStaleKilledTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_vod_builds_stale_killed_total",
		Help: "Total number of stale VOD builds terminated",
	}, []string{"method"}) // method=cancel|kill

	vodCacheEvictedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_vod_cache_evicted_total",
		Help: "Total number of VOD cache directories evicted",
	})

	vodBuildDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_vod_build_duration_seconds",
		Help:    "Duration of VOD build attempts",
		Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800},
	}, []string{"result"}) // result=success|failed|canceled|stale

	vodSetupSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_vod_setup_seconds",
		Help:    "Latency until VOD stream is ready for playback",
		Buckets: []float64{0.5, 1, 2.5, 5, 10, 20, 30},
	}, []string{"stage", "mode"}) // stage=live_ready, mode=fast|robust

	// Phase 10: Circuit Breaker Metrics
	vodCircuitOpen = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_vod_circuit_open_total",
		Help: "Total number of times circuit breaker opened",
	}, []string{"key"}) // recording root

	vodCircuitTrips = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_vod_circuit_trips_total",
		Help: "Total number of circuit breaker trips by reason",
	}, []string{"reason"})

	vodCircuitHalfOpen = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_vod_circuit_halfopen_total",
		Help: "Total number of times circuit breaker entered half-open state",
	})
)

func IncVODBuildsActive() { vodBuildsActive.WithLabelValues("ffmpeg").Inc() }
func DecVODBuildsActive() { vodBuildsActive.WithLabelValues("ffmpeg").Dec() }

func IncVODBuildRejected(reason string) {
	vodBuildsRejectedTotal.WithLabelValues(reason).Inc()
}

func IncVODBuildStaleKilled(method string) {
	vodBuildsStaleKilledTotal.WithLabelValues(method).Inc()
}

func IncVODCacheEvicted() {
	vodCacheEvictedTotal.Inc()
}

func ObserveVODSetupLatency(stage, mode string, duration float64) {
	vodSetupSeconds.WithLabelValues(stage, mode).Observe(duration)
}

func IncVODCircuitOpen(key string) {
	vodCircuitOpen.WithLabelValues(key).Inc()
}

func IncVODCircuitTrips(reason string) {
	vodCircuitTrips.WithLabelValues(reason).Inc()
}

func IncVODCircuitHalfOpen() {
	vodCircuitHalfOpen.Inc()
}

func ObserveVODBuildDuration(result string, duration float64) {
	vodBuildDurationSeconds.WithLabelValues(result).Observe(duration)
}
