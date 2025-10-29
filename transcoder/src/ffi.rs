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
            Err(_) => return ptr::null_mut(),
        };
        let handle = Box::new(RemuxerHandle { remuxer });

        Box::into_raw(handle) as *mut c_void
    });

    match result {
        Ok(ptr) => ptr,
        Err(_) => ptr::null_mut(),
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
