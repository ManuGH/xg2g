//! Shared server functionality for the GPU transcoder
//!
//! This module contains the server components that are used by both
//! the standalone binary (main.rs) and the FFI layer (ffi.rs).

use axum::{
    body::Body,
    extract::Query,
    http::{header, StatusCode},
    response::{IntoResponse, Response},
    Json,
};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tokio::process::Command;
use tracing::{error, info, warn};

use crate::metrics::MetricsGuard;
use crate::transcoder::{TranscoderConfig, VaapiTranscoder};

/// Application state shared across handlers
pub struct AppState {
    pub config: TranscoderConfig,
    pub vaapi_available: bool,
    pub metrics_handle: metrics_exporter_prometheus::PrometheusHandle,
}

/// Health check response
#[derive(Debug, Serialize)]
pub struct HealthResponse {
    pub status: String,
    pub vaapi_available: bool,
    pub version: String,
}

/// Error response
#[derive(Debug, Serialize)]
pub struct ErrorResponse {
    pub error: String,
}

/// Transcode request parameters
#[derive(Debug, Deserialize)]
pub struct TranscodeParams {
    pub source_url: String,
    #[serde(default)]
    pub video_bitrate: Option<String>,
    #[serde(default)]
    pub audio_bitrate: Option<String>,
}

/// Check if VAAPI hardware acceleration is available
pub async fn check_vaapi() -> bool {
    let output = Command::new("vainfo").output().await;

    match output {
        Ok(output) if output.status.success() => {
            let stdout = String::from_utf8_lossy(&output.stdout);
            info!("VAAPI check output:\n{}", stdout);
            true
        }
        _ => {
            warn!("vainfo command failed - VAAPI might not be available");
            false
        }
    }
}

/// Health check handler
pub async fn health_handler(
    axum::extract::State(state): axum::extract::State<Arc<AppState>>,
) -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok".to_string(),
        vaapi_available: state.vaapi_available,
        version: env!("CARGO_PKG_VERSION").to_string(),
    })
}

/// Prometheus metrics handler
pub async fn metrics_handler(
    axum::extract::State(state): axum::extract::State<Arc<AppState>>,
) -> Response {
    let metrics = state.metrics_handle.render();
    (
        StatusCode::OK,
        [(header::CONTENT_TYPE, "text/plain; version=0.0.4")],
        metrics,
    )
        .into_response()
}

/// HTTP GET transcode handler (source_url parameter)
pub async fn transcode_handler(
    axum::extract::State(state): axum::extract::State<Arc<AppState>>,
    Query(params): Query<TranscodeParams>,
) -> Response {
    let _guard = MetricsGuard::new();
    info!("Transcode request: source_url={}", params.source_url);

    if !state.vaapi_available {
        warn!("VAAPI not available, cannot transcode");
        _guard.error();
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(ErrorResponse {
                error: "GPU acceleration not available".to_string(),
            }),
        )
            .into_response();
    }

    // Override config with request params
    let mut config = state.config.clone();
    if let Some(vb) = params.video_bitrate {
        config.video_bitrate = vb;
    }
    if let Some(ab) = params.audio_bitrate {
        config.audio_bitrate = ab;
    }

    // Create transcoder
    let transcoder = VaapiTranscoder::new(config);

    // Start transcoding
    match transcoder.transcode_stream(&params.source_url).await {
        Ok(stream) => {
            _guard.success();

            // Set appropriate headers
            let headers = [
                (header::CONTENT_TYPE, "video/mp2t"),
                (header::CACHE_CONTROL, "no-cache, no-store, must-revalidate"),
                (header::CONNECTION, "close"),
            ];

            (StatusCode::OK, headers, Body::from_stream(stream)).into_response()
        }
        Err(e) => {
            error!("Transcode error: {}", e);
            _guard.error();
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(ErrorResponse {
                    error: format!("Transcode failed: {}", e),
                }),
            )
                .into_response()
        }
    }
}
