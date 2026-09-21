// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Phase alignment of cyclic timestamps to a shared epoch timeline.
//!
//! While unwrapping provides continuity for an individual clock, phase alignment
//! ensures that multiple clock domains (PCR, Video PTS/DTS, Audio PTS/DTS) share
//! the same modulo period relative to an epoch reference anchor.

use super::types::{ExtendedDts90k, ExtendedPcr27m, ExtendedPts90k, RawDts33, RawPcr27m, RawPts33};

/// Aligns a raw 33-bit Presentation Time Stamp (90 kHz) to the nearest 2^33 period
/// relative to an established 90 kHz reference timestamp.
#[must_use]
pub fn align_90k_to_reference(raw: RawPts33, reference: ExtendedPts90k) -> Option<ExtendedPts90k> {
    let modulus = i128::from(RawPts33::MODULUS);
    let half = modulus / 2;

    let raw = i128::from(raw.get());
    let reference = i128::from(reference.get());

    let reference_phase = reference.rem_euclid(modulus);
    let forward = (raw - reference_phase).rem_euclid(modulus);

    let delta = if forward >= half {
        forward - modulus
    } else {
        forward
    };

    let aligned = reference.checked_add(delta)?;

    i64::try_from(aligned).ok().map(ExtendedPts90k::new)
}

/// Aligns a raw 33-bit Decode Time Stamp (90 kHz) to the nearest 2^33 period
/// relative to an established 90 kHz reference timestamp.
#[must_use]
pub fn align_dts_to_reference(raw: RawDts33, reference: ExtendedPts90k) -> Option<ExtendedDts90k> {
    let modulus = i128::from(RawDts33::MODULUS);
    let half = modulus / 2;

    let raw = i128::from(raw.get());
    let reference = i128::from(reference.get());

    let reference_phase = reference.rem_euclid(modulus);
    let forward = (raw - reference_phase).rem_euclid(modulus);

    let delta = if forward >= half {
        forward - modulus
    } else {
        forward
    };

    let aligned = reference.checked_add(delta)?;

    i64::try_from(aligned).ok().map(ExtendedDts90k::new)
}

/// Aligns a raw 27 MHz PCR timestamp to the nearest `2^33 * 300` period
/// relative to an established 27 MHz reference timestamp.
#[must_use]
pub fn align_27m_to_reference(raw: RawPcr27m, reference: ExtendedPcr27m) -> Option<ExtendedPcr27m> {
    let modulus = i128::from(RawPcr27m::MODULUS);
    let half = modulus / 2;

    let raw = i128::from(raw.get());
    let reference = i128::from(reference.get());

    let reference_phase = reference.rem_euclid(modulus);
    let forward = (raw - reference_phase).rem_euclid(modulus);

    let delta = if forward >= half {
        forward - modulus
    } else {
        forward
    };

    let aligned = reference.checked_add(delta)?;

    i64::try_from(aligned).ok().map(ExtendedPcr27m::new)
}

/// Aligns a raw 27 MHz PCR timestamp to an established 90 kHz epoch reference.
#[must_use]
pub fn align_pcr_to_90k_reference(
    raw: RawPcr27m,
    reference_90k: ExtendedPts90k,
) -> Option<ExtendedPcr27m> {
    let ref_27m_i128 = i128::from(reference_90k.get()).checked_mul(300)?;
    let ref_27m = i64::try_from(ref_27m_i128).ok().map(ExtendedPcr27m::new)?;
    align_27m_to_reference(raw, ref_27m)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn align_90k_near_wrap_boundary_avoids_26_hour_offset() {
        // PCR90k is near 2^33 - 100
        let pcr_ref_val = i64::try_from(RawPts33::MODULUS - 100).expect("fits in i64");
        let pcr_ref = ExtendedPts90k::new(pcr_ref_val);
        // PTS is 50
        let raw_pts = RawPts33::new(50).unwrap();

        let aligned = align_90k_to_reference(raw_pts, pcr_ref).unwrap();
        // True difference must be +150 ticks, NOT -26.5 hours!
        assert_eq!(aligned.checked_diff(pcr_ref), Some(150));
        let expected = i64::try_from(RawPts33::MODULUS + 50).expect("fits in i64");
        assert_eq!(aligned.get(), expected);
    }

    #[test]
    fn align_90k_with_dts_and_pts_at_boundary() {
        // DTS = 2^33 - 10
        let dts_ref_val = i64::try_from(RawPts33::MODULUS - 10).expect("fits in i64");
        let dts_ref = ExtendedPts90k::new(dts_ref_val);
        // PTS = 10
        let raw_pts = RawPts33::new(10).unwrap();

        let aligned_pts = align_90k_to_reference(raw_pts, dts_ref).unwrap();
        assert_eq!(aligned_pts.checked_diff(dts_ref), Some(20));
    }

    #[test]
    fn align_90k_with_negative_reference() {
        // Negative reference coordinate (e.g. -100)
        let ref_neg = ExtendedPts90k::new(-100);
        let raw_pts = RawPts33::new(RawPts33::MODULUS - 80).unwrap();

        let aligned = align_90k_to_reference(raw_pts, ref_neg).unwrap();
        // Distance from -100 to -80 is +20
        assert_eq!(aligned.checked_diff(ref_neg), Some(20));
        assert_eq!(aligned.get(), -80);
    }

    #[test]
    fn align_27m_pcr_to_90k_reference() {
        let ref_90k = ExtendedPts90k::new(1000);
        // PCR raw is 300 * 1050 = 315,000 ticks
        let raw_pcr = RawPcr27m::new(315_000).unwrap();

        let aligned = align_pcr_to_90k_reference(raw_pcr, ref_90k).unwrap();
        assert_eq!(aligned.get(), 315_000);
        assert_eq!(aligned.to_90k().checked_diff(ref_90k), Some(50));
    }
}
