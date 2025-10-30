//! FFI (Foreign Function Interface) layer for Go integration
//!
//! This module provides C-compatible functions that can be called from Go using CGO.
//! The Rust audio remuxer is exposed as a simple C API for maximum compatibility.
//!
//! # Safety
//!
//! All functions use `extern "C"` and are marked with `#[no_mangle]` to ensure
//! stable ABI. Panics are caught and converted to error codes.
//!
//! # Memory Management
//!
//! - Go owns input/output buffers
//! - Rust owns the remuxer handle (opaque pointer)
//! - Caller must call `xg2g_audio_remux_free` to prevent leaks

use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_int, c_void};
use std::panic::catch_unwind;
use std::ptr;

use crate::audio_remux::{AudioRemuxConfig, AudioRemuxer};

/// Opaque handle to AudioRemuxer
/// This is passed between Go and Rust as a void pointer
struct RemuxerHandle {
    remuxer: AudioRemuxer,
}

/// Initialize a new audio remuxer
///
/// # Arguments
///
/// * `sample_rate` - Audio sample rate in Hz (e.g., 48000)
/// * `channels` - Number of audio channels (e.g., 2 for stereo)
/// * `bitrate` - Target AAC bitrate in bits per second (e.g., 192000)
///
/// # Returns
///
/// * Opaque handle to remuxer instance, or NULL on error
///
/// # Safety
///
/// The returned handle must be freed with `xg2g_audio_remux_free`.
#[no_mangle]
pub extern "C" fn xg2g_audio_remux_init(
    sample_rate: c_int,
    channels: c_int,
    bitrate: c_int,
) -> *mut c_void {
    // Catch panics and return NULL instead of unwinding across FFI
    let result = catch_unwind(|| {
        let config = AudioRemuxConfig {
            aac_bitrate: bitrate as u32,
            channels: channels as u16,
            sample_rate: sample_rate as u32,
            aac_profile: crate::encoder::AacProfile::AacLc,
        };

        let remuxer = match AudioRemuxer::new(config) {
            Ok(r) => r,
            Err(e) => {
                eprintln!("[RUST FFI ERROR] Failed to create AudioRemuxer: {:#}", e);
                return ptr::null_mut();
            }
        };
        let handle = Box::new(RemuxerHandle { remuxer });

        Box::into_raw(handle) as *mut c_void
    });

    match result {
        Ok(ptr) => ptr,
        Err(e) => {
            eprintln!("[RUST FFI PANIC] AudioRemuxer initialization panicked: {:?}", e);
            ptr::null_mut()
        }
    }
}

/// Process audio data through the remuxer
///
/// # Arguments
///
/// * `handle` - Opaque handle from `xg2g_audio_remux_init`
/// * `input` - Pointer to input MPEG-TS data (MP2/AC3 audio)
/// * `input_len` - Length of input data in bytes
/// * `output` - Pointer to output buffer for MPEG-TS data (AAC audio)
/// * `output_capacity` - Size of output buffer in bytes
///
/// # Returns
///
/// * Number of bytes written to output buffer, or -1 on error
///
/// # Safety
///
/// - `handle` must be valid (from `xg2g_audio_remux_init`)
/// - `input` must point to valid memory of at least `input_len` bytes
/// - `output` must point to valid writable memory of at least `output_capacity` bytes
/// - Caller must ensure buffers don't overlap
#[no_mangle]
pub extern "C" fn xg2g_audio_remux_process(
    handle: *mut c_void,
    input: *const u8,
    input_len: usize,
    output: *mut u8,
    output_capacity: usize,
) -> c_int {
    if handle.is_null() || input.is_null() || output.is_null() {
        return -1;
    }

    let result = catch_unwind(|| {
        // SAFETY: Caller guarantees handle is valid
        let handle = unsafe { &mut *(handle as *mut RemuxerHandle) };

        // SAFETY: Caller guarantees input/output are valid for their lengths
        let input_slice = unsafe { std::slice::from_raw_parts(input, input_len) };
        let output_slice = unsafe { std::slice::from_raw_parts_mut(output, output_capacity) };

        // TODO: Actual remuxing implementation
        // For now, just copy (placeholder)
        let bytes_to_copy = input_len.min(output_capacity);
        output_slice[..bytes_to_copy].copy_from_slice(&input_slice[..bytes_to_copy]);

        bytes_to_copy as c_int
    });

    match result {
        Ok(n) => n,
        Err(_) => -1,
    }
}

/// Free the audio remuxer and release resources
///
/// # Arguments
///
/// * `handle` - Opaque handle from `xg2g_audio_remux_init`
///
/// # Safety
///
/// - `handle` must be valid (from `xg2g_audio_remux_init`)
/// - `handle` must not be used after this call
/// - This function is idempotent (safe to call multiple times with NULL)
#[no_mangle]
pub extern "C" fn xg2g_audio_remux_free(handle: *mut c_void) {
    if handle.is_null() {
        return;
    }

    let _ = catch_unwind(|| {
        // SAFETY: Caller guarantees handle is valid
        unsafe {
            let _ = Box::from_raw(handle as *mut RemuxerHandle);
            // Box is dropped here, freeing the remuxer
        }
    });
}

/// Get version string
///
/// # Returns
///
/// * Pointer to null-terminated version string (static lifetime)
///
/// # Safety
///
/// Returned pointer is valid for the entire program lifetime.
/// Caller must NOT free this pointer.
#[no_mangle]
pub extern "C" fn xg2g_transcoder_version() -> *const c_char {
    static VERSION: &str = concat!(env!("CARGO_PKG_VERSION"), "\0");
    VERSION.as_ptr() as *const c_char
}

/// Get last error message
///
/// # Returns
///
/// * Pointer to null-terminated error string, or NULL if no error
///
/// # Safety
///
/// Caller must free the returned string with `xg2g_free_string`.
#[no_mangle]
pub extern "C" fn xg2g_last_error() -> *mut c_char {
    // TODO: Thread-local error storage
    ptr::null_mut()
}

/// Free a string allocated by Rust
///
/// # Arguments
///
/// * `s` - Pointer to string from Rust functions
///
/// # Safety
///
/// - `s` must have been allocated by a Rust FFI function
/// - `s` must not be used after this call
#[no_mangle]
pub extern "C" fn xg2g_free_string(s: *mut c_char) {
    if s.is_null() {
        return;
    }

    let _ = catch_unwind(|| unsafe {
        let _ = CString::from_raw(s);
    });
}

// =============================================================================
// GPU Transcoding Server FFI (MODE 3)
// =============================================================================

use std::sync::{Arc, Mutex, Once};
use std::thread;

/// Opaque handle to GPU Server
struct GpuServerHandle {
    shutdown_tx: Option<tokio::sync::oneshot::Sender<()>>,
    thread_handle: Option<thread::JoinHandle<()>>,
}

static mut GPU_SERVER: Option<Arc<Mutex<GpuServerHandle>>> = None;
static INIT: Once = Once::new();

/// Start the embedded GPU transcoding server
///
/// # Arguments
///
/// * `listen_addr` - Listen address (e.g., "127.0.0.1:8085")
/// * `vaapi_device` - VAAPI device path (e.g., "/dev/dri/renderD128")
///
/// # Returns
///
/// * 0 on success, -1 on error
///
/// # Safety
///
/// Can only be called once. Subsequent calls will return -1.
/// Call `xg2g_gpu_server_stop` to shutdown.
#[no_mangle]
pub extern "C" fn xg2g_gpu_server_start(
    listen_addr: *const c_char,
    vaapi_device: *const c_char,
) -> c_int {
    let result = catch_unwind(|| {
        if listen_addr.is_null() || vaapi_device.is_null() {
            eprintln!("[GPU FFI ERROR] Null arguments provided");
            return -1;
        }

        // Convert C strings
        let addr_str = unsafe {
            match CStr::from_ptr(listen_addr).to_str() {
                Ok(s) => s.to_string(),
                Err(_) => {
                    eprintln!("[GPU FFI ERROR] Invalid listen_addr string");
                    return -1;
                }
            }
        };

        let vaapi_str = unsafe {
            match CStr::from_ptr(vaapi_device).to_str() {
                Ok(s) => s.to_string(),
                Err(_) => {
                    eprintln!("[GPU FFI ERROR] Invalid vaapi_device string");
                    return -1;
                }
            }
        };

        // Initialize GPU server (only once)
        let mut initialized = false;
        INIT.call_once(|| {
            let (shutdown_tx, shutdown_rx) = tokio::sync::oneshot::channel();

            // Spawn GPU server in dedicated thread with its own runtime
            let thread_handle = thread::Builder::new()
                .name("gpu-server".to_string())
                .spawn(move || {
                    eprintln!("[GPU FFI] Starting GPU server thread on {}", addr_str);

                    // Create dedicated Tokio runtime for GPU server
                    let runtime = match tokio::runtime::Builder::new_multi_thread()
                        .worker_threads(2) // 2 threads sufficient for GPU server
                        .thread_name("gpu-worker")
                        .enable_all()
                        .build()
                    {
                        Ok(rt) => rt,
                        Err(e) => {
                            eprintln!("[GPU FFI ERROR] Failed to create runtime: {}", e);
                            return;
                        }
                    };

                    // Run GPU server
                    runtime.block_on(async move {
                        if let Err(e) = run_gpu_server(addr_str, vaapi_str, shutdown_rx).await {
                            eprintln!("[GPU FFI ERROR] Server failed: {}", e);
                        }
                    });

                    eprintln!("[GPU FFI] GPU server thread exiting");
                })
                .expect("Failed to spawn GPU server thread");

            // Store server handle
            let handle = GpuServerHandle {
                shutdown_tx: Some(shutdown_tx),
                thread_handle: Some(thread_handle),
            };

            unsafe {
                GPU_SERVER = Some(Arc::new(Mutex::new(handle)));
            }

            initialized = true;
        });

        if initialized {
            eprintln!("[GPU FFI] GPU server started successfully");
            0
        } else {
            eprintln!("[GPU FFI ERROR] GPU server already running");
            -1
        }
    });

    match result {
        Ok(code) => code,
        Err(e) => {
            eprintln!("[GPU FFI PANIC] Start panicked: {:?}", e);
            -1
        }
    }
}

/// Check if GPU server is running
///
/// # Returns
///
/// * 1 if running, 0 if not running
#[no_mangle]
pub extern "C" fn xg2g_gpu_server_is_running() -> c_int {
    unsafe {
        if let Some(server) = &GPU_SERVER {
            if let Ok(handle) = server.lock() {
                if handle.thread_handle.is_some() {
                    return 1;
                }
            }
        }
    }
    0
}

/// Stop the GPU transcoding server
///
/// # Returns
///
/// * 0 on success, -1 on error
///
/// # Safety
///
/// Blocks until server shuts down gracefully.
#[no_mangle]
pub extern "C" fn xg2g_gpu_server_stop() -> c_int {
    let result = catch_unwind(|| {
        unsafe {
            if let Some(server) = GPU_SERVER.take() {
                if let Ok(mut handle) = server.lock() {
                    eprintln!("[GPU FFI] Shutting down GPU server...");

                    // Send shutdown signal
                    if let Some(tx) = handle.shutdown_tx.take() {
                        let _ = tx.send(());
                    }

                    // Wait for thread to finish
                    if let Some(thread) = handle.thread_handle.take() {
                        if let Err(e) = thread.join() {
                            eprintln!("[GPU FFI ERROR] Thread join failed: {:?}", e);
                            return -1;
                        }
                    }

                    eprintln!("[GPU FFI] GPU server stopped");
                    return 0;
                }
            }
        }

        eprintln!("[GPU FFI ERROR] GPU server not running");
        -1
    });

    match result {
        Ok(code) => code,
        Err(e) => {
            eprintln!("[GPU FFI PANIC] Stop panicked: {:?}", e);
            -1
        }
    }
}

/// Run the GPU server (internal async function)
async fn run_gpu_server(
    listen_addr: String,
    vaapi_device: String,
    mut shutdown_rx: tokio::sync::oneshot::Receiver<()>,
) -> anyhow::Result<()> {
    use crate::transcoder::TranscoderConfig;
    use std::sync::Arc;

    // Set VAAPI device environment variable
    std::env::set_var("VAAPI_DEVICE", vaapi_device);

    // Initialize tracing (only if not already initialized by main binary)
    // When embedded via FFI, tracing may already be initialized by the Go daemon
    let _ = tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "xg2g_transcoder=info".into()),
        )
        .with(tracing_subscriber::fmt::layer().with_ansi(false))
        .try_init(); // Use try_init() instead of init() to avoid panic if already initialized

    tracing::info!("GPU server initializing...");

    // Check VAAPI availability
    let vaapi_available = crate::main::check_vaapi().await;
    if !vaapi_available {
        tracing::warn!("VAAPI not available - GPU transcoding will not work!");
    }

    // Load configuration
    let config = TranscoderConfig::from_env();
    let metrics_handle = crate::metrics::init_metrics();

    let app_state = Arc::new(crate::main::AppState {
        config,
        vaapi_available,
        metrics_handle,
    });

    // Build router (same as main.rs)
    use axum::{routing::get, Router};
    let app = Router::new()
        .route("/health", get(crate::main::health_handler))
        .route("/metrics", get(crate::main::metrics_handler))
        .route("/transcode", get(crate::main::transcode_handler))
        .with_state(app_state);

    // Parse listen address
    let addr: std::net::SocketAddr = listen_addr.parse()?;
    tracing::info!("GPU server listening on {}", addr);

    // Start server with graceful shutdown
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            let _ = shutdown_rx.await;
            tracing::info!("GPU server shutdown signal received");
        })
        .await?;

    Ok(())
}

// =============================================================================
// Tests
// =============================================================================

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_remuxer_init_and_free() {
        let handle = xg2g_audio_remux_init(48000, 2, 192000);
        assert!(!handle.is_null());
        xg2g_audio_remux_free(handle);
    }

    #[test]
    fn test_remuxer_process() {
        let handle = xg2g_audio_remux_init(48000, 2, 192000);
        assert!(!handle.is_null());

        let input = vec![0u8; 1024];
        let mut output = vec![0u8; 2048];

        let result = xg2g_audio_remux_process(
            handle,
            input.as_ptr(),
            input.len(),
            output.as_mut_ptr(),
            output.len(),
        );

        assert!(result >= 0);
        xg2g_audio_remux_free(handle);
    }

    #[test]
    fn test_version() {
        let version_ptr = xg2g_transcoder_version();
        assert!(!version_ptr.is_null());

        let version = unsafe { CStr::from_ptr(version_ptr) };
        let version_str = version.to_str().unwrap();
        assert!(!version_str.is_empty());
    }

    #[test]
    fn test_null_handle() {
        // Should not crash with null handle
        xg2g_audio_remux_free(ptr::null_mut());

        let result = xg2g_audio_remux_process(
            ptr::null_mut(),
            ptr::null(),
            0,
            ptr::null_mut(),
            0,
        );
        assert_eq!(result, -1);
    }
}
