// SPDX-License-Identifier: MIT
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
)

func RecordBouquetsCount(n int) { bouquetsTotal.Set(float64(n)) }
func IncBouquetDiscoveryError() { bouquetDiscoveryErrors.Inc() }

func RecordServicesCount(bouquet string, n int) {
	servicesDiscovered.WithLabelValues(bouquet).Set(float64(n))
}
func IncServicesResolution(bouquet, outcome string) {
	servicesResolutionTotal.WithLabelValues(bouquet, outcome).Inc()
}

func IncStreamURLBuild(outcome string) { streamURLBuildTotal.WithLabelValues(outcome).Inc() }

func RecordChannelTypeCounts(hd, sd, radio, unknown int) {
	channelTypes.WithLabelValues("hd").Set(float64(hd))
	channelTypes.WithLabelValues("sd").Set(float64(sd))
	channelTypes.WithLabelValues("radio").Set(float64(radio))
	channelTypes.WithLabelValues("unknown").Set(float64(unknown))
}

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

func IncConfigValidationError()      { configValidationErrors.Inc() }
func IncRefreshFailure(stage string) { refreshFailuresTotal.WithLabelValues(stage).Inc() }

// EPG metrics functions
func IncEPGChannelError() {
	epgRequestsTotal.WithLabelValues("error").Inc()
}

func RecordEPGChannelSuccess(programmes int) {
	epgRequestsTotal.WithLabelValues("success").Inc()
}

func RecordEPGCollection(totalProgrammes, channelsWithData int, duration float64) {
	epgProgrammesCollected.Set(float64(totalProgrammes))
	epgChannelsWithData.Set(float64(channelsWithData))
	epgCollectionDurationSeconds.Observe(duration)
}
