// SPDX-License-Identifier: MIT

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
)

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
