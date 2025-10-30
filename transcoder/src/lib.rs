//! xg2g Rust Audio Transcoder Library
//!
//! This library provides native audio remuxing capabilities for the xg2g daemon.
//! It can be used as a standalone binary or embedded in Go via FFI.

pub mod audio_remux;
pub mod decoder;
pub mod demux;
pub mod encoder;
pub mod ffi;
pub mod main;
pub mod muxer;
pub mod transcoder;
pub mod metrics;

// Re-export main types for convenience
pub use transcoder::{TranscoderConfig, VaapiTranscoder};
pub use audio_remux::{AudioRemuxConfig, AudioRemuxer};
pub use metrics::{MetricsGuard, init_metrics};

#[cfg(test)]
mod ffi_test;
