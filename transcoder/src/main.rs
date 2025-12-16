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
    http::{header, HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use std::net::SocketAddr;
use std::sync::Arc;
use tracing::{error, info, warn};

// Import from the library crate
use xg2g_transcoder::metrics;
use xg2g_transcoder::server::{
    AppState, ErrorResponse,
    check_vaapi, health_handler, metrics_handler, transcode_handler,
};
use xg2g_transcoder::transcoder::{TranscoderConfig, VaapiTranscoder};

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
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "xg2g_transcoder=info".into()),
        )
        .with_writer(std::io::stdout)
        .with_ansi(false)
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

async fn transcode_stream_handler(
    axum::extract::State(state): axum::extract::State<Arc<AppState>>,
    _headers: HeaderMap,
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
