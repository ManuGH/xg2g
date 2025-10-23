// SPDX-License-Identifier: MIT

// Package telemetry provides OpenTelemetry tracing utilities for the xg2g application.
package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
)

// Common attribute keys for consistent tracing across the application.
const (
	// HTTP attributes
	HTTPMethodKey     = "http.method"
	HTTPStatusCodeKey = "http.status_code"
	HTTPRouteKey      = "http.route"
	HTTPURLKey        = "http.url"
	HTTPUserAgentKey  = "http.user_agent"

	// Streaming attributes
	StreamChannelKey   = "stream.channel"
	StreamServiceIDKey = "stream.service_id"
	StreamBouquetKey   = "stream.bouquet"

	// Transcoding attributes
	TranscodeCodecKey       = "transcode.codec"
	TranscodeInputCodecKey  = "transcode.input_codec"
	TranscodeOutputCodecKey = "transcode.output_codec"
	TranscodeBitrateKey     = "transcode.bitrate"
	TranscodeResolutionKey  = "transcode.resolution"
	TranscodeDeviceKey      = "transcode.device"
	TranscodeGPUEnabledKey  = "transcode.gpu_enabled"

	// EPG attributes
	EPGDaysKey        = "epg.days"
	EPGChannelsKey    = "epg.channels"
	EPGEventsKey      = "epg.events"
	EPGConcurrencyKey = "epg.concurrency"
	EPGRetriesKey     = "epg.retries"

	// Job attributes
	JobTypeKey     = "job.type"
	JobStatusKey   = "job.status"
	JobDurationKey = "job.duration_ms"

	// Error attributes
	ErrorKey     = "error"
	ErrorTypeKey = "error.type"
)

// HTTPAttributes creates common HTTP span attributes.
func HTTPAttributes(method, route, url string, statusCode int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(HTTPMethodKey, method),
		attribute.String(HTTPRouteKey, route),
		attribute.String(HTTPURLKey, url),
		attribute.Int(HTTPStatusCodeKey, statusCode),
	}
}

// StreamAttributes creates streaming-related span attributes.
func StreamAttributes(channel, serviceID, bouquet string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	if channel != "" {
		attrs = append(attrs, attribute.String(StreamChannelKey, channel))
	}
	if serviceID != "" {
		attrs = append(attrs, attribute.String(StreamServiceIDKey, serviceID))
	}
	if bouquet != "" {
		attrs = append(attrs, attribute.String(StreamBouquetKey, bouquet))
	}
	return attrs
}

// TranscodeAttributes creates transcoding-related span attributes.
func TranscodeAttributes(inputCodec, outputCodec, device string, bitrate int, gpuEnabled bool) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(TranscodeInputCodecKey, inputCodec),
		attribute.String(TranscodeOutputCodecKey, outputCodec),
		attribute.String(TranscodeDeviceKey, device),
		attribute.Int(TranscodeBitrateKey, bitrate),
		attribute.Bool(TranscodeGPUEnabledKey, gpuEnabled),
	}
}

// EPGAttributes creates EPG-related span attributes.
func EPGAttributes(days, channels, events, concurrency, retries int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int(EPGDaysKey, days),
		attribute.Int(EPGChannelsKey, channels),
		attribute.Int(EPGEventsKey, events),
		attribute.Int(EPGConcurrencyKey, concurrency),
		attribute.Int(EPGRetriesKey, retries),
	}
}

// JobAttributes creates job-related span attributes.
func JobAttributes(jobType, status string, durationMS int64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(JobTypeKey, jobType),
		attribute.String(JobStatusKey, status),
		attribute.Int64(JobDurationKey, durationMS),
	}
}

// ErrorAttributes creates error-related span attributes.
func ErrorAttributes(_ error, errorType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Bool(ErrorKey, true),
		attribute.String(ErrorTypeKey, errorType),
	}
}
