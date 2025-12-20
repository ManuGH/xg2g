// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

use metrics::{counter, gauge, histogram};
use std::time::Instant;

/// Initialize Prometheus metrics exporter and return handle
pub fn init_metrics() -> metrics_exporter_prometheus::PrometheusHandle {
    let builder = metrics_exporter_prometheus::PrometheusBuilder::new();
    builder
        .install_recorder()
        .expect("failed to install Prometheus exporter")
}

/// Record a new transcoding request
pub fn record_transcode_request() {
    counter!("xg2g_transcoder_requests_total").increment(1);
}

/// Record transcoding success
pub fn record_transcode_success() {
    counter!("xg2g_transcoder_success_total").increment(1);
}

/// Record transcoding failure
pub fn record_transcode_error() {
    counter!("xg2g_transcoder_errors_total").increment(1);
}

/// Record active transcoding sessions
pub fn set_active_sessions(count: usize) {
    gauge!("xg2g_transcoder_active_sessions").set(count as f64);
}

/// Record transcoding duration
pub fn record_transcode_duration(duration: std::time::Duration) {
    histogram!("xg2g_transcoder_duration_seconds").record(duration.as_secs_f64());
}

/// Record bytes transcoded
pub fn record_bytes_transcoded(bytes: u64) {
    counter!("xg2g_transcoder_bytes_total").increment(bytes);
}

/// Record FFmpeg startup time
pub fn record_ffmpeg_startup(duration: std::time::Duration) {
    histogram!("xg2g_transcoder_ffmpeg_startup_seconds").record(duration.as_secs_f64());
}

/// Metrics guard that tracks duration
pub struct MetricsGuard {
    start: Instant,
}

impl MetricsGuard {
    pub fn new() -> Self {
        record_transcode_request();
        Self {
            start: Instant::now(),
        }
    }

    pub fn success(self) {
        record_transcode_success();
        record_transcode_duration(self.start.elapsed());
    }

    pub fn error(self) {
        record_transcode_error();
        record_transcode_duration(self.start.elapsed());
    }
}

impl Default for MetricsGuard {
    fn default() -> Self {
        Self::new()
    }
}
