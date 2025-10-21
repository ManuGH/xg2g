// Jemalloc - REMOVED due to incompatibility with Tokio runtime initialization
// Issue: jemalloc's initialization conflicts with manual Tokio runtime builder
// For GPU transcoding, bottlenecks are GPU/IO/FFmpeg, not memory allocation
// Standard glibc malloc is sufficient for this use case
//
// #[cfg(not(target_env = "msvc"))]
// use tikv_jemallocator::Jemalloc;
//
// #[cfg(not(target_env = "msvc"))]
// #[global_allocator]
// static GLOBAL: Jemalloc = Jemalloc;

use axum::{
    body::Body,
    extract::Query,
    http::{header, HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use bytes::Bytes;
use serde::{Deserialize, Serialize};
use std::net::SocketAddr;
use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::process::Command;
use tracing::{error, info, warn};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

mod metrics;
mod transcoder;

use metrics::{MetricsGuard, record_bytes_transcoded, record_ffmpeg_startup, set_active_sessions};
use transcoder::{TranscoderConfig, VaapiTranscoder};

#[derive(Debug, Deserialize)]
struct TranscodeParams {
    source_url: String,
    #[serde(default)]
    video_bitrate: Option<String>,
    #[serde(default)]
    audio_bitrate: Option<String>,
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: String,
    vaapi_available: bool,
    version: String,
}

#[derive(Debug, Serialize)]
struct ErrorResponse {
    error: String,
}

fn main() -> anyhow::Result<()> {
    // Early debug output to verify binary starts
    eprintln!("[STARTUP] xg2g-transcoder binary starting...");

    // Build Tokio runtime manually to avoid stdin POLLHUP check
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;

    runtime.block_on(async_main())
}

async fn async_main() -> anyhow::Result<()> {
    eprintln!("[STARTUP] Async runtime started successfully");

    // Initialize tracing with explicit stdout target
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "xg2g_transcoder=info".into()),
        )
        .with(
            tracing_subscriber::fmt::layer()
                .with_writer(std::io::stdout)
                .with_ansi(false)
        )
        .init();

    info!("xg2g GPU Transcoder starting...");

    // Initialize metrics
    let metrics_handle = metrics::init_metrics();
    info!("Prometheus metrics initialized");

    // Check VAAPI availability
    let vaapi_available = check_vaapi().await;
    if !vaapi_available {
        warn!("VAAPI not available - GPU transcoding will not work!");
    } else {
        info!("VAAPI hardware acceleration available");
    }

    // Load configuration from environment
    let config = TranscoderConfig::from_env();
    info!(?config, "Transcoder configuration loaded");

    let app_state = Arc::new(AppState {
        config,
        vaapi_available,
        metrics_handle,
    });

    // Build router
    let app = Router::new()
        .route("/health", get(health_handler))
        .route("/metrics", get(metrics_handler))
        .route("/transcode", get(transcode_handler))
        .route("/transcode/stream", post(transcode_stream_handler))
        .with_state(app_state)
        .layer(
            tower_http::trace::TraceLayer::new_for_http()
                .make_span_with(tower_http::trace::DefaultMakeSpan::default())
                .on_response(tower_http::trace::DefaultOnResponse::default()),
        )
        .layer(tower_http::cors::CorsLayer::permissive());

    // Start server
    let port = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse::<u16>().ok())
        .unwrap_or(8085);
    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    info!("Transcoder listening on {}", addr);

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;

    Ok(())
}

struct AppState {
    config: TranscoderConfig,
    vaapi_available: bool,
    metrics_handle: metrics_exporter_prometheus::PrometheusHandle,
}

async fn health_handler(
    axum::extract::State(state): axum::extract::State<Arc<AppState>>,
) -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok".to_string(),
        vaapi_available: state.vaapi_available,
        version: env!("CARGO_PKG_VERSION").to_string(),
    })
}

async fn metrics_handler(
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

async fn transcode_handler(
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

async fn transcode_stream_handler(
    axum::extract::State(state): axum::extract::State<Arc<AppState>>,
    headers: HeaderMap,
    body: Body,
) -> Response {
    info!("Stream transcode request (POST with body)");

    if !state.vaapi_available {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(ErrorResponse {
                error: "GPU acceleration not available".to_string(),
            }),
        )
            .into_response();
    }

    let transcoder = VaapiTranscoder::new(state.config.clone());

    match transcoder.transcode_stdin(body).await {
        Ok(stream) => {
            let headers = [
                (header::CONTENT_TYPE, "video/mp2t"),
                (header::CACHE_CONTROL, "no-cache, no-store, must-revalidate"),
                (header::CONNECTION, "close"),
            ];

            (StatusCode::OK, headers, Body::from_stream(stream)).into_response()
        }
        Err(e) => {
            error!("Stream transcode error: {}", e);
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(ErrorResponse {
                    error: format!("Stream transcode failed: {}", e),
                }),
            )
                .into_response()
        }
    }
}

async fn check_vaapi() -> bool {
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
