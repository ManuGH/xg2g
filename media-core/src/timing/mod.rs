// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Deterministic transport stream timing and clock estimation.

pub mod pcr;

pub use pcr::{MAX_PLAUSIBLE_DELTA_TICKS, PCR_MODULUS, PcrSample, PcrTracker, TimingSnapshot};
