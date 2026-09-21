// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Deterministic transport stream timing and clock estimation.

pub mod pcr;
pub mod types;

pub use pcr::{MAX_PLAUSIBLE_DELTA_TICKS, PCR_MODULUS, PcrSample, PcrTracker, TimingSnapshot};
pub use types::{
    BitrateBps, ByteOffset, ExtendedDts90k, ExtendedPcr27m, ExtendedPts90k, PesTiming, Pid,
    RawDts33, RawPcr27m, RawPts33, TimelineEpoch, TimingEvent, TimingField,
};
