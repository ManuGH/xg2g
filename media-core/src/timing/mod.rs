// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Deterministic transport stream timing and clock estimation.

#[cfg(test)]
mod corpus_test;
pub mod pcr;
pub mod phase;
pub mod timeline;
pub mod types;
pub mod unwrap;

pub use pcr::{
    MAX_PLAUSIBLE_DELTA_TICKS, PCR_MODULUS, PcrObservation, PcrSample, PcrTracker, TimingSnapshot,
};
pub use phase::{
    align_27m_to_reference, align_90k_to_reference, align_dts_to_reference,
    align_pcr_to_90k_reference,
};
pub use timeline::TimelineTracker;
pub use types::{
    BitrateBps, ByteOffset, DiscontinuityReason, ExtendedDts90k, ExtendedPcr27m, ExtendedPts90k,
    PesTiming, Pid, RawDts33, RawPcr27m, RawPts33, TimelineEpoch, TimingEvent, TimingField,
    TimingPoint, TimingRecord, TimingResetScope,
};
pub use unwrap::{DtsUnwrapper33, PcrUnwrapper27m, PtsUnwrapper33, modular_delta};
