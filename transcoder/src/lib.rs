//! xg2g Rust Audio Transcoder Library
//!
//! This library provides native audio remuxing capabilities for the xg2g daemon.
//! It can be used as a standalone binary or embedded in Go via FFI.

pub mod audio_remux;
pub mod ffi;
pub mod transcoder;
pub mod metrics;
