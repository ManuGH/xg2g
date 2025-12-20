// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! xg2g Rust Audio Transcoder Library
//!
//! This library provides native audio remuxing capabilities for the xg2g daemon.
//! It can be used as a standalone binary or embedded in Go via FFI.

pub mod audio_remux;
pub mod decoder;
pub mod demux;
pub mod encoder;
pub mod ffi;
pub mod muxer;
pub mod server;
pub mod transcoder;
pub mod metrics;

// Re-export main types for convenience
pub use transcoder::{TranscoderConfig, VaapiTranscoder};
pub use audio_remux::{AudioRemuxConfig, AudioRemuxer};
pub use metrics::{MetricsGuard, init_metrics};

#[cfg(test)]
mod ffi_test;
