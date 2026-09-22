// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Canonical PCR extraction, deterministic 27 MHz integer timing, and bitrate estimation.

use super::types::{BitrateBps, ByteOffset, Pid, RawPcr27m};
use crate::transport::{PacketView, Pcr};

/// The modulus of the 27 MHz PCR counter: 2^33 * 300 = 2,576,980,377,600 ticks.
pub const PCR_MODULUS: u64 = (1 << 33) * 300;

/// Maximum plausible PCR delta ticks between consecutive PCRs without an announced discontinuity.
/// Corresponds to 2.0 seconds at 27 MHz (54,000,000 ticks).
pub const MAX_PLAUSIBLE_DELTA_TICKS: u64 = 2 * 27_000_000;

/// A recorded PCR sample and its position in the stream.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PcrSample {
    /// The transport byte offset where this PCR was observed.
    pub packet_offset: ByteOffset,
    /// The canonical PCR value.
    pub pcr: Pcr,
    /// The raw 27 MHz PCR timestamp.
    pub raw: RawPcr27m,
}

/// A point-in-time snapshot of the timing tracker's state.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct TimingSnapshot {
    /// The PID designated for PCR, if any.
    pub pcr_pid: Option<Pid>,
    /// The last observed PCR value.
    pub last_pcr: Option<Pcr>,
    /// The last observed raw PCR timestamp.
    pub last_raw_pcr: Option<RawPcr27m>,
    /// The transport byte offset where the last PCR was observed.
    pub last_pcr_offset: ByteOffset,
    /// Estimated bitrate in bits per second (via integer EMA).
    pub bitrate_bps: BitrateBps,
    /// Total number of valid PCRs observed.
    pub pcr_count: u64,
    /// Total number of discontinuity events observed on the PCR PID.
    pub discontinuity_count: u64,
}

/// Tracks Program Clock Reference (PCR) timestamps, detects discontinuities,
/// and maintains an integer EMA of transport stream bitrate.
///
/// This implementation is strictly deterministic, relying entirely on transport
/// stream packet offsets and 27 MHz PCR ticks without any wall-clock or `Instant`
/// dependencies.
#[derive(Debug, Default)]
pub struct PcrTracker {
    pcr_pid: Option<Pid>,
    last_pcr: Option<Pcr>,
    last_offset: ByteOffset,
    pending_discontinuity: bool,
    bitrate_bps: BitrateBps,
    pcr_count: u64,
    discontinuity_count: u64,
}

/// An authoritative observation of PCR state emitted by [`PcrTracker`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PcrObservation {
    /// No PCR or discontinuity event on this packet.
    None,
    /// Discontinuity indicator was observed on the PCR PID without a PCR timestamp.
    Discontinuity {
        /// Byte offset of the packet where the discontinuity indicator was seen.
        offset: ByteOffset,
    },
    /// A syntactically valid PCR timestamp was observed on the PCR PID.
    Sample {
        /// Byte offset of the packet carrying this PCR.
        offset: ByteOffset,
        /// The raw 27 MHz PCR timestamp.
        raw: RawPcr27m,
    },
    /// A syntactically valid PCR timestamp was observed on the PCR PID with an announced discontinuity.
    DiscontinuitySample {
        /// Byte offset of the packet carrying this PCR and discontinuity indicator.
        offset: ByteOffset,
        /// The raw 27 MHz PCR timestamp.
        raw: RawPcr27m,
    },
}

impl PcrTracker {
    /// Constructs a new PCR tracker with no assigned PID or baseline.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Rebinds the tracker to a new PCR PID, resetting baseline and rate estimates.
    pub fn rebind(&mut self, pcr_pid: Option<Pid>) {
        self.pcr_pid = pcr_pid;
        self.reset();
    }

    /// Clears baseline, rate estimates, and pending discontinuity flags.
    pub fn reset(&mut self) {
        self.last_pcr = None;
        self.last_offset = ByteOffset::default();
        self.pending_discontinuity = false;
        self.bitrate_bps = BitrateBps::default();
        self.pcr_count = 0;
        self.discontinuity_count = 0;
    }

    /// The PID currently being followed for PCR timing, if any.
    #[must_use]
    pub fn pcr_pid(&self) -> Option<Pid> {
        self.pcr_pid
    }

    /// The most recently observed PCR value, if any.
    #[must_use]
    pub fn last_pcr(&self) -> Option<Pcr> {
        self.last_pcr
    }

    /// The estimated transport stream bitrate in bits per second.
    #[must_use]
    pub fn bitrate_bps(&self) -> BitrateBps {
        self.bitrate_bps
    }

    /// Captures a point-in-time snapshot of the tracker state.
    #[must_use]
    pub fn snapshot(&self) -> TimingSnapshot {
        TimingSnapshot {
            pcr_pid: self.pcr_pid,
            last_pcr: self.last_pcr,
            last_raw_pcr: self.last_pcr.and_then(|p| RawPcr27m::new(p.ticks_27mhz)),
            last_pcr_offset: self.last_offset,
            bitrate_bps: self.bitrate_bps,
            pcr_count: self.pcr_count,
            discontinuity_count: self.discontinuity_count,
        }
    }

    /// Observes a single transport stream packet at `packet_offset`.
    ///
    /// # Panics
    ///
    /// Panics if a valid PCR timestamp from `view.pcr()` exceeds `RawPcr27m::MODULUS`.
    pub fn observe(&mut self, packet_offset: i64, view: &PacketView<'_>) -> PcrObservation {
        let Some(target_pid) = self.pcr_pid else {
            return PcrObservation::None;
        };
        if view.pid() != target_pid.get() {
            return PcrObservation::None;
        }

        // TEI packet on PCR PID: invalidate rate baseline; do not parse PCR.
        if view.transport_error_indicator() {
            self.last_pcr = None;
            return PcrObservation::None;
        }

        let offset = ByteOffset::new(packet_offset);

        // Latch discontinuity indicator even if this packet carries no PCR.
        let has_di = view.discontinuity_indicator();
        if has_di {
            self.pending_discontinuity = true;
            self.discontinuity_count = self.discontinuity_count.saturating_add(1);
        }

        let Some(current_pcr) = view.pcr() else {
            if has_di {
                return PcrObservation::Discontinuity { offset };
            }
            return PcrObservation::None;
        };

        self.pcr_count = self.pcr_count.saturating_add(1);
        let raw = RawPcr27m::new(current_pcr.ticks_27mhz).expect("valid PCR ticks < MODULUS");

        // Discontinuity was latched: re-anchor baseline without updating rate estimation.
        if self.pending_discontinuity {
            self.last_pcr = Some(current_pcr);
            self.last_offset = offset;
            self.pending_discontinuity = false;
            if has_di {
                return PcrObservation::DiscontinuitySample { offset, raw };
            }
            return PcrObservation::Sample { offset, raw };
        }

        let Some(last_pcr) = self.last_pcr else {
            // First clean PCR: anchor initial baseline.
            self.last_pcr = Some(current_pcr);
            self.last_offset = offset;
            return PcrObservation::Sample { offset, raw };
        };

        // Checked byte advancement in stream coordinate system.
        let Some(delta_bytes) = offset.checked_sub(self.last_offset) else {
            // Negative offset or invalid subtraction: re-anchor baseline.
            self.last_pcr = Some(current_pcr);
            self.last_offset = offset;
            return PcrObservation::Sample { offset, raw };
        };

        if delta_bytes == 0 {
            // Byte offset did not advance; skip update.
            return PcrObservation::Sample { offset, raw };
        }

        // Delta ticks with 2^33 * 300 rollover handling.
        let delta_ticks = if current_pcr.ticks_27mhz >= last_pcr.ticks_27mhz {
            current_pcr.ticks_27mhz - last_pcr.ticks_27mhz
        } else {
            (current_pcr.ticks_27mhz + PCR_MODULUS) - last_pcr.ticks_27mhz
        };

        // Divide-by-zero guard or implausible jump (> 2.0s without discontinuity indicator).
        if delta_ticks == 0 || delta_ticks >= MAX_PLAUSIBLE_DELTA_TICKS {
            self.last_pcr = Some(current_pcr);
            self.last_offset = offset;
            return PcrObservation::Sample { offset, raw };
        }

        // Pure integer instant bitrate: (delta_bytes * 8 * 27_000_000) / delta_ticks
        let instant_bps = (u128::from(delta_bytes) * 8 * 27_000_000) / u128::from(delta_ticks);
        let instant_bps_u64 = u64::try_from(instant_bps).unwrap_or(u64::MAX);

        if self.bitrate_bps.get() == 0 {
            self.bitrate_bps = BitrateBps::new(instant_bps_u64);
        } else {
            // Integer EMA: 80% previous estimate + 20% instant sample.
            let filtered = (u128::from(self.bitrate_bps.get()) * 4 + instant_bps) / 5;
            self.bitrate_bps = BitrateBps::new(u64::try_from(filtered).unwrap_or(u64::MAX));
        }

        self.last_pcr = Some(current_pcr);
        self.last_offset = offset;
        PcrObservation::Sample { offset, raw }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::transport::TS_PACKET_LEN;

    /// Helper to create a 188-byte packet with given PID, adaptation flags, and PCR.
    fn make_pcr_packet(
        pid: u16,
        tei: bool,
        di: bool,
        pcr_val: Option<(u64, u16)>,
    ) -> [u8; TS_PACKET_LEN] {
        let mut pkt = [0xFFu8; TS_PACKET_LEN];
        pkt[0] = 0x47;
        pkt[1] = u8::try_from((pid >> 8) & 0x1F).unwrap() | if tei { 0x80 } else { 0 };
        pkt[2] = u8::try_from(pid & 0xFF).unwrap();

        if let Some((base, ext)) = pcr_val {
            pkt[3] = 0x20; // adaptation field only (afc = 0b10)
            pkt[4] = 7; // adaptation_field_length: 1 flag byte + 6 PCR bytes
            let mut flags = 0x10; // PCR flag set
            if di {
                flags |= 0x80;
            }
            pkt[5] = flags;

            pkt[6] = u8::try_from((base >> 25) & 0xFF).unwrap();
            pkt[7] = u8::try_from((base >> 17) & 0xFF).unwrap();
            pkt[8] = u8::try_from((base >> 9) & 0xFF).unwrap();
            pkt[9] = u8::try_from((base >> 1) & 0xFF).unwrap();
            pkt[10] = (u8::try_from((base & 0x01) << 7).unwrap())
                | 0x7E
                | (u8::try_from((ext >> 8) & 0x01).unwrap());
            pkt[11] = u8::try_from(ext & 0xFF).unwrap();
        } else if di {
            pkt[3] = 0x20;
            pkt[4] = 1;
            pkt[5] = 0x80; // discontinuity indicator set, no PCR
        } else {
            pkt[3] = 0x10; // payload only, no adaptation
        }
        pkt
    }

    #[test]
    fn pcr_syntax_and_ext_bounds() {
        // ext = 299: valid
        let pkt = make_pcr_packet(100, false, false, Some((1000, 299)));
        let view = PacketView::parse(&pkt).expect("valid packet");
        let pcr = view.pcr().expect("valid PCR");
        assert_eq!(pcr.base, 1000);
        assert_eq!(pcr.ext, 299);
        assert_eq!(pcr.ticks_27mhz, 1000 * 300 + 299);

        // ext = 300: invalid per ISO/IEC 13818-1
        let pkt_bad = make_pcr_packet(100, false, false, Some((1000, 300)));
        let view_bad = PacketView::parse(&pkt_bad).expect("valid packet");
        assert_eq!(view_bad.pcr(), None);

        // ext = 511: invalid
        let pkt_bad2 = make_pcr_packet(100, false, false, Some((1000, 511)));
        let view_bad2 = PacketView::parse(&pkt_bad2).expect("valid packet");
        assert_eq!(view_bad2.pcr(), None);
    }

    #[test]
    fn deterministic_rate_estimation_and_ema() {
        let mut tracker = PcrTracker::new();
        tracker.rebind(Pid::new(100));

        // 40ms interval at 4 Mbps:
        // 40ms = 0.04s * 27_000_000 = 1,080_000 ticks.
        // 4 Mbps = 500,000 B/s -> in 0.04s = 20,000 bytes.
        let delta_bytes: i64 = 20_000;
        let delta_ticks: u64 = 1_080_000;

        let pkt1 = make_pcr_packet(100, false, false, Some((0, 0)));
        let view1 = PacketView::parse(&pkt1).unwrap();
        tracker.observe(0, &view1);

        assert_eq!(tracker.bitrate_bps().get(), 0);
        assert_eq!(tracker.snapshot().pcr_count, 1);

        // Second packet establishes initial instant bitrate (4,000,000 bps)
        let pkt2 = make_pcr_packet(
            100,
            false,
            false,
            Some((delta_ticks / 300, (delta_ticks % 300) as u16)),
        );
        let view2 = PacketView::parse(&pkt2).unwrap();
        tracker.observe(delta_bytes, &view2);

        assert_eq!(tracker.bitrate_bps().get(), 4_000_000);
        assert_eq!(tracker.snapshot().pcr_count, 2);

        // Third packet: higher instant rate (5 Mbps: 25,000 bytes over same 40ms)
        let delta_bytes_3: i64 = 45_000;
        let total_ticks_3 = delta_ticks * 2;
        let pkt3 = make_pcr_packet(
            100,
            false,
            false,
            Some((total_ticks_3 / 300, (total_ticks_3 % 300) as u16)),
        );
        let view3 = PacketView::parse(&pkt3).unwrap();
        tracker.observe(delta_bytes_3, &view3);

        // Instant rate was 5,000,000.
        // EMA: (4_000_000 * 4 + 5_000_000) / 5 = 21_000_000 / 5 = 4_200_000 bps.
        assert_eq!(tracker.bitrate_bps().get(), 4_200_000);
    }

    #[test]
    fn pcr_rollover_handling() {
        let mut tracker = PcrTracker::new();
        tracker.rebind(Pid::new(100));

        // PCR near modulus: (PCR_MODULUS - 540_000) (20ms before wrap)
        let ticks1 = PCR_MODULUS - 540_000;
        let pkt1 = make_pcr_packet(
            100,
            false,
            false,
            Some((ticks1 / 300, (ticks1 % 300) as u16)),
        );
        let view1 = PacketView::parse(&pkt1).unwrap();
        tracker.observe(0, &view1);

        // PCR wrapped past zero: 540_000 ticks (20ms after wrap)
        // Total delta ticks: 1,080,000 ticks (40ms)
        // Delta bytes: 20,000 -> 4 Mbps
        let ticks2 = 540_000;
        let pkt2 = make_pcr_packet(
            100,
            false,
            false,
            Some((ticks2 / 300, (ticks2 % 300) as u16)),
        );
        let view2 = PacketView::parse(&pkt2).unwrap();
        tracker.observe(20_000, &view2);

        assert_eq!(tracker.bitrate_bps().get(), 4_000_000);
    }

    #[test]
    fn pending_discontinuity_latched_and_cleared() {
        let mut tracker = PcrTracker::new();
        tracker.rebind(Pid::new(100));

        // Initial sample
        let pkt1 = make_pcr_packet(100, false, false, Some((10_000, 0)));
        let view1 = PacketView::parse(&pkt1).unwrap();
        tracker.observe(0, &view1);

        // Next packet on PID 100 has discontinuity_indicator, but NO PCR
        let pkt_di = make_pcr_packet(100, false, true, None);
        let view_di = PacketView::parse(&pkt_di).unwrap();
        tracker.observe(188, &view_di);

        assert_eq!(tracker.snapshot().discontinuity_count, 1);

        // Next packet on PID 100 has a PCR jumping backwards to 5,000
        let pkt2 = make_pcr_packet(100, false, false, Some((5_000, 0)));
        let view2 = PacketView::parse(&pkt2).unwrap();
        tracker.observe(376, &view2);

        // Because discontinuity was latched, baseline re-anchored without updating rate
        assert_eq!(tracker.bitrate_bps().get(), 0);
        assert_eq!(tracker.snapshot().pcr_count, 2);
        assert_eq!(tracker.snapshot().last_pcr_offset, ByteOffset::new(376));

        // Subsequent normal packet updates rate normally
        let delta_ticks: u64 = 1_080_000;
        let ticks3 = 5_000 * 300 + delta_ticks;
        let pkt3 = make_pcr_packet(
            100,
            false,
            false,
            Some((ticks3 / 300, (ticks3 % 300) as u16)),
        );
        let view3 = PacketView::parse(&pkt3).unwrap();
        tracker.observe(20_376, &view3);

        assert_eq!(tracker.bitrate_bps().get(), 4_000_000);
    }

    #[test]
    fn tei_invalidates_baseline() {
        let mut tracker = PcrTracker::new();
        tracker.rebind(Pid::new(100));

        // Initial sample
        let pkt1 = make_pcr_packet(100, false, false, Some((10_000, 0)));
        let view1 = PacketView::parse(&pkt1).unwrap();
        tracker.observe(0, &view1);

        // Second packet has TEI set
        let pkt_tei = make_pcr_packet(100, true, false, Some((20_000, 0)));
        let view_tei = PacketView::parse(&pkt_tei).unwrap();
        tracker.observe(20_000, &view_tei);

        // Baseline invalidated
        assert_eq!(tracker.snapshot().last_pcr, None);

        // Third clean packet establishes new baseline, no rate update
        let pkt3 = make_pcr_packet(100, false, false, Some((30_000, 0)));
        let view3 = PacketView::parse(&pkt3).unwrap();
        tracker.observe(40_000, &view3);

        assert_eq!(tracker.bitrate_bps().get(), 0);
        assert_eq!(tracker.snapshot().last_pcr_offset, ByteOffset::new(40_000));
    }

    #[test]
    fn unannounced_jump_exceeding_threshold_reanchors() {
        let mut tracker = PcrTracker::new();
        tracker.rebind(Pid::new(100));

        let pkt1 = make_pcr_packet(100, false, false, Some((10_000, 0)));
        let view1 = PacketView::parse(&pkt1).unwrap();
        tracker.observe(0, &view1);

        // Delta > 2.0s (54,000,000 ticks) without discontinuity indicator
        let delta_ticks = MAX_PLAUSIBLE_DELTA_TICKS + 300;
        let ticks2 = 10_000 * 300 + delta_ticks;
        let pkt2 = make_pcr_packet(
            100,
            false,
            false,
            Some((ticks2 / 300, (ticks2 % 300) as u16)),
        );
        let view2 = PacketView::parse(&pkt2).unwrap();
        tracker.observe(20_000, &view2);

        // Re-anchored without updating rate
        assert_eq!(tracker.bitrate_bps().get(), 0);
        assert_eq!(tracker.snapshot().last_pcr_offset, ByteOffset::new(20_000));
    }
}
