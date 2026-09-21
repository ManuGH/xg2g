// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Deterministic modular unwrappers for cyclic media timestamps.
//!
//! Provides modular signed delta calculation in range `[-M/2, +M/2)`
//! and typed unwrappers for PTS (33-bit, 90 kHz), DTS (33-bit, 90 kHz),
//! and PCR (27 MHz, modulus `2^33 * 300`).

use super::types::{ExtendedDts90k, ExtendedPcr27m, ExtendedPts90k, RawDts33, RawPcr27m, RawPts33};

/// Computes the signed modular delta from `previous` to `current` under `modulus`.
///
/// Returns a signed delta in range `[-modulus/2, +modulus/2)`.
/// At exact half-modulus (`modulus / 2`), deterministically resolves to `-modulus / 2`.
///
/// Calculations are performed in `i128` to guarantee arithmetic safety.
///
/// # Panics
///
/// Panics if the signed modular delta exceeds the representable range of `i64`.
#[must_use]
pub fn modular_delta(current: u64, previous: u64, modulus: u64) -> i64 {
    let current_m = current % modulus;
    let previous_m = previous % modulus;
    let forward = (current_m + modulus - previous_m) % modulus;
    let half = modulus / 2;

    let delta = if forward >= half {
        i128::from(forward) - i128::from(modulus)
    } else {
        i128::from(forward)
    };

    i64::try_from(delta).expect("modular delta fits in i64")
}

/// Unwrapper for raw 33-bit Presentation Time Stamps (90 kHz).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct PtsUnwrapper33 {
    anchor: Option<(RawPts33, ExtendedPts90k)>,
}

impl PtsUnwrapper33 {
    /// Constructs a new, uninitialized PTS unwrapper.
    #[must_use]
    pub const fn new() -> Self {
        Self { anchor: None }
    }

    /// Initializes or re-anchors the unwrapper with a phase-aligned base.
    pub fn init(&mut self, raw: RawPts33, base: ExtendedPts90k) {
        self.anchor = Some((raw, base));
    }

    /// Resets the unwrapper, discarding the current anchor.
    pub fn reset(&mut self) {
        self.anchor = None;
    }

    /// Returns `true` if an anchor is currently established.
    #[must_use]
    pub const fn is_initialized(&self) -> bool {
        self.anchor.is_some()
    }

    /// Returns the last unwrapped timestamp, if any.
    #[must_use]
    pub const fn last_unwrapped(&self) -> Option<ExtendedPts90k> {
        match self.anchor {
            Some((_, ext)) => Some(ext),
            None => None,
        }
    }

    /// Unwraps the next raw PTS sample relative to the established anchor.
    ///
    /// If uninitialized, returns `None`.
    pub fn unwrap(&mut self, current: RawPts33) -> Option<ExtendedPts90k> {
        let (prev_raw, prev_ext) = self.anchor?;
        let delta = modular_delta(current.get(), prev_raw.get(), RawPts33::MODULUS);
        let new_ext = prev_ext.checked_add_ticks(delta)?;
        self.anchor = Some((current, new_ext));
        Some(new_ext)
    }
}

/// Unwrapper for raw 33-bit Decode Time Stamps (90 kHz).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct DtsUnwrapper33 {
    anchor: Option<(RawDts33, ExtendedDts90k)>,
}

impl DtsUnwrapper33 {
    /// Constructs a new, uninitialized DTS unwrapper.
    #[must_use]
    pub const fn new() -> Self {
        Self { anchor: None }
    }

    /// Initializes or re-anchors the unwrapper with a phase-aligned base.
    pub fn init(&mut self, raw: RawDts33, base: ExtendedDts90k) {
        self.anchor = Some((raw, base));
    }

    /// Resets the unwrapper, discarding the current anchor.
    pub fn reset(&mut self) {
        self.anchor = None;
    }

    /// Returns `true` if an anchor is currently established.
    #[must_use]
    pub const fn is_initialized(&self) -> bool {
        self.anchor.is_some()
    }

    /// Returns the last unwrapped timestamp, if any.
    #[must_use]
    pub const fn last_unwrapped(&self) -> Option<ExtendedDts90k> {
        match self.anchor {
            Some((_, ext)) => Some(ext),
            None => None,
        }
    }

    /// Unwraps the next raw DTS sample relative to the established anchor.
    ///
    /// If uninitialized, returns `None`.
    pub fn unwrap(&mut self, current: RawDts33) -> Option<ExtendedDts90k> {
        let (prev_raw, prev_ext) = self.anchor?;
        let delta = modular_delta(current.get(), prev_raw.get(), RawDts33::MODULUS);
        let new_ext = prev_ext.checked_add_ticks(delta)?;
        self.anchor = Some((current, new_ext));
        Some(new_ext)
    }
}

/// Unwrapper for raw 27 MHz Program Clock Reference timestamps.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct PcrUnwrapper27m {
    anchor: Option<(RawPcr27m, ExtendedPcr27m)>,
}

impl PcrUnwrapper27m {
    /// Constructs a new, uninitialized PCR unwrapper.
    #[must_use]
    pub const fn new() -> Self {
        Self { anchor: None }
    }

    /// Initializes or re-anchors the unwrapper with a phase-aligned base.
    pub fn init(&mut self, raw: RawPcr27m, base: ExtendedPcr27m) {
        self.anchor = Some((raw, base));
    }

    /// Resets the unwrapper, discarding the current anchor.
    pub fn reset(&mut self) {
        self.anchor = None;
    }

    /// Returns `true` if an anchor is currently established.
    #[must_use]
    pub const fn is_initialized(&self) -> bool {
        self.anchor.is_some()
    }

    /// Returns the last unwrapped timestamp, if any.
    #[must_use]
    pub const fn last_unwrapped(&self) -> Option<ExtendedPcr27m> {
        match self.anchor {
            Some((_, ext)) => Some(ext),
            None => None,
        }
    }

    /// Unwraps the next raw PCR sample relative to the established anchor.
    ///
    /// If uninitialized, returns `None`.
    pub fn unwrap(&mut self, current: RawPcr27m) -> Option<ExtendedPcr27m> {
        let (prev_raw, prev_ext) = self.anchor?;
        let delta = modular_delta(current.get(), prev_raw.get(), RawPcr27m::MODULUS);
        let new_ext = prev_ext.checked_add_ticks(delta)?;
        self.anchor = Some((current, new_ext));
        Some(new_ext)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn modular_delta_forward_steps() {
        let m = RawPts33::MODULUS;
        assert_eq!(modular_delta(100, 90, m), 10);
        assert_eq!(modular_delta(3000, 0, m), 3000);
        assert_eq!(modular_delta(100, 100, m), 0);
    }

    #[test]
    fn modular_delta_preserves_b_frame_negative_delta() {
        let m = RawPts33::MODULUS;
        // B-frame decode order: previous PTS was 100, current PTS is 90
        assert_eq!(modular_delta(90, 100, m), -10);
        assert_eq!(modular_delta(90_000, 93_000, m), -3000);
    }

    #[test]
    fn modular_delta_forward_wrap() {
        let m = RawPts33::MODULUS;
        // previous is near 2^33 - 1, current has wrapped past 0
        let prev = m - 10;
        let curr = 5;
        assert_eq!(modular_delta(curr, prev, m), 15);
    }

    #[test]
    fn modular_delta_backward_wrap() {
        let m = RawPts33::MODULUS;
        // previous is near 0, current stepped backwards across 0
        let prev = 5;
        let curr = m - 10;
        assert_eq!(modular_delta(curr, prev, m), -15);
    }

    #[test]
    fn modular_delta_exact_half_modulus_boundary() {
        let m = RawPts33::MODULUS;
        let half = m / 2;
        // Exactly half modulus forward: deterministically resolves to -half
        let delta = modular_delta(half, 0, m);
        let half_i64 = i64::try_from(half).expect("half fits in i64");
        assert_eq!(delta, -half_i64);
    }

    #[test]
    fn pts_unwrapper_monotonic_and_b_frames() {
        let mut u = PtsUnwrapper33::new();
        assert!(!u.is_initialized());
        assert_eq!(u.unwrap(RawPts33::new(100).unwrap()), None);

        // Initialize at PTS = 100, extended = 100
        u.init(RawPts33::new(100).unwrap(), ExtendedPts90k::new(100));
        assert!(u.is_initialized());

        // B-frame: PTS 90 (step backwards by 10)
        let ext1 = u.unwrap(RawPts33::new(90).unwrap()).unwrap();
        assert_eq!(ext1.get(), 90);

        // Forward: PTS 120 (step forwards by 30)
        let ext2 = u.unwrap(RawPts33::new(120).unwrap()).unwrap();
        assert_eq!(ext2.get(), 120);

        // Forward wrap across 2^33
        let wrap_prev = RawPts33::new(RawPts33::MAX_VALUE).unwrap();
        let prev_val = i64::try_from(RawPts33::MAX_VALUE).expect("fits in i64");
        u.init(wrap_prev, ExtendedPts90k::new(prev_val));
        let ext_wrap = u.unwrap(RawPts33::new(5).unwrap()).unwrap();
        let expected = i64::try_from(RawPts33::MODULUS + 5).expect("fits in i64");
        assert_eq!(ext_wrap.get(), expected);
    }

    #[test]
    fn pcr_unwrapper_27m_wrap() {
        let mut u = PcrUnwrapper27m::new();
        let m = RawPcr27m::MODULUS;
        let prev = RawPcr27m::new(m - 100).unwrap();
        let prev_val = i64::try_from(m - 100).expect("fits in i64");
        u.init(prev, ExtendedPcr27m::new(prev_val));

        let curr = RawPcr27m::new(200).unwrap();
        let ext = u.unwrap(curr).unwrap();
        let expected = i64::try_from(m + 200).expect("fits in i64");
        assert_eq!(ext.get(), expected);
    }
}
