// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Canonical stream timeline tracker and epoch coordinator.
//!
//! Manages `TimelineEpoch` transitions, establishes phase alignment across
//! PCR and elementary stream timestamps, and associates Random Access Points (RAP)
//! with continuous `ExtendedPts90k` timestamps via `subject_at`.

use std::collections::{HashMap, VecDeque};

use super::pcr::PcrObservation;
use super::phase::{align_90k_to_reference, align_dts_to_reference, align_pcr_to_90k_reference};
use super::types::{
    ByteOffset, ExtendedDts90k, ExtendedPcr27m, ExtendedPts90k, Pid, TimelineEpoch, TimingEvent,
    TimingField, TimingPoint,
};
use super::unwrap::{DtsUnwrapper33, PcrUnwrapper27m, PtsUnwrapper33};

const VIDEO_PTS_HISTORY_CAP: usize = 32;

/// Tracks stream timeline epochs, coordinates phase alignment across PCR, video,
/// and audio clocks, and binds Random Access Points to extended timestamps.
#[derive(Debug, Default)]
pub struct TimelineTracker {
    active_epoch: Option<TimelineEpoch>,
    next_epoch: u64,
    epoch_reference_90k: Option<ExtendedPts90k>,

    pcr_unwrapper: PcrUnwrapper27m,
    video_pts_unwrapper: PtsUnwrapper33,
    video_dts_unwrapper: DtsUnwrapper33,

    audio_pts_unwrappers: HashMap<Pid, PtsUnwrapper33>,
    audio_dts_unwrappers: HashMap<Pid, DtsUnwrapper33>,

    last_video_point: Option<TimingPoint>,
    video_pts_history: VecDeque<(i64, ExtendedPts90k)>,
    last_audio_points: HashMap<Pid, TimingPoint>,
    last_pcr: Option<(ExtendedPcr27m, ByteOffset)>,

    rap_points: HashMap<i64, ExtendedPts90k>,
}

impl TimelineTracker {
    /// Constructs a new timeline tracker with no active epoch.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// The currently active timeline epoch, if a program is active.
    #[must_use]
    pub const fn active_epoch(&self) -> Option<TimelineEpoch> {
        self.active_epoch
    }

    /// Activates the next timeline epoch.
    ///
    /// If no epoch was previously active, activates `TimelineEpoch(0)` (or current `next_epoch`).
    /// All clock unwrappers and anchors are reset for the new epoch.
    pub fn activate_next_epoch(&mut self) -> TimelineEpoch {
        let epoch = TimelineEpoch::new(self.next_epoch);
        self.next_epoch = self.next_epoch.saturating_add(1);
        self.active_epoch = Some(epoch);
        self.epoch_reference_90k = None;

        self.pcr_unwrapper.reset();
        self.video_pts_unwrapper.reset();
        self.video_dts_unwrapper.reset();
        self.audio_pts_unwrappers.clear();
        self.audio_dts_unwrappers.clear();

        self.last_video_point = None;
        self.video_pts_history.clear();
        self.last_audio_points.clear();
        self.last_pcr = None;
        self.rap_points.clear();

        epoch
    }

    /// Deactivates the current program epoch (e.g. when PMT is lost).
    ///
    /// Leaves `next_epoch` unchanged so that the next program activation receives a monotonic epoch.
    pub fn deactivate_program(&mut self) {
        self.active_epoch = None;
        self.epoch_reference_90k = None;

        self.pcr_unwrapper.reset();
        self.video_pts_unwrapper.reset();
        self.video_dts_unwrapper.reset();
        self.audio_pts_unwrappers.clear();
        self.audio_dts_unwrappers.clear();

        self.last_video_point = None;
        self.video_pts_history.clear();
        self.last_audio_points.clear();
        self.last_pcr = None;
        self.rap_points.clear();
    }

    /// Resets the unwrappers for a specific track (PID) due to track-local timing loss
    /// (such as continuity error or TEI on an elementary stream).
    ///
    /// Does **NOT** advance `TimelineEpoch` or modify `epoch_reference_90k`.
    pub fn reset_track(&mut self, pid: Pid) {
        if self
            .last_video_point
            .as_ref()
            .is_some_and(|pt| pt.pid == pid)
        {
            self.video_pts_unwrapper.reset();
            self.video_dts_unwrapper.reset();
            self.last_video_point = None;
            self.video_pts_history.clear();
        }
        if let Some(u) = self.audio_pts_unwrappers.get_mut(&pid) {
            u.reset();
        }
        if let Some(u) = self.audio_dts_unwrappers.get_mut(&pid) {
            u.reset();
        }
        self.last_audio_points.remove(&pid);
    }

    /// Ingests an authoritative PCR observation from [`PcrTracker`](super::pcr::PcrTracker).
    ///
    /// If the observation indicates a discontinuity on the PCR PID, the program epoch
    /// is advanced before ingesting the sample.
    ///
    /// # Panics
    ///
    /// Panics if a valid PCR timestamp exceeds `i64::MAX`.
    pub fn observe_pcr(&mut self, obs: PcrObservation) -> Option<ExtendedPcr27m> {
        match obs {
            PcrObservation::None => None,
            PcrObservation::Discontinuity { .. } => {
                if self.active_epoch.is_some() {
                    self.activate_next_epoch();
                }
                None
            }
            PcrObservation::Sample {
                offset,
                raw,
                discontinuity,
            } => {
                if discontinuity && self.active_epoch.is_some() {
                    self.activate_next_epoch();
                }

                self.active_epoch?;

                let ext_pcr = match self.epoch_reference_90k {
                    None => {
                        // First clock sample in epoch: establish epoch reference
                        let pcr_90k = raw.get() / 300;
                        let pcr_90k_i64 = i64::try_from(pcr_90k).expect("pcr_90k fits in i64");
                        let ref_90k = ExtendedPts90k::new(pcr_90k_i64);
                        self.epoch_reference_90k = Some(ref_90k);

                        let ext_val = i64::try_from(raw.get()).expect("pcr fits in i64");
                        let ext = ExtendedPcr27m::new(ext_val);
                        self.pcr_unwrapper.init(raw, ext);
                        ext
                    }
                    Some(ref_90k) => {
                        if self.pcr_unwrapper.is_initialized() {
                            self.pcr_unwrapper.unwrap(raw)?
                        } else {
                            let ext = align_pcr_to_90k_reference(raw, ref_90k)?;
                            self.pcr_unwrapper.init(raw, ext);
                            ext
                        }
                    }
                };

                self.last_pcr = Some((ext_pcr, offset));
                Some(ext_pcr)
            }
        }
    }

    /// Observes a video timing event from [`PesHeaderAssembler`](crate::pes::PesHeaderAssembler).
    ///
    /// # Panics
    ///
    /// Panics if a 33-bit timestamp exceeds `i64::MAX`.
    pub fn observe_video_timing(&mut self, event: &TimingEvent) -> Option<TimingPoint> {
        let epoch = self.active_epoch?;

        let (pts, dts) = match self.epoch_reference_90k {
            None => {
                // No clock sample has established epoch reference yet.
                // Prioritize DTS if present, else PTS.
                if let TimingField::Valid(raw_dts) = event.timing.dts {
                    let base_dts_val = i64::try_from(raw_dts.get()).expect("raw_dts fits in i64");
                    let base_dts = ExtendedDts90k::new(base_dts_val);
                    let ref_90k = ExtendedPts90k::new(base_dts.get());
                    self.epoch_reference_90k = Some(ref_90k);
                    self.video_dts_unwrapper.init(raw_dts, base_dts);

                    let pts = if let TimingField::Valid(raw_pts) = event.timing.pts {
                        let aligned = align_90k_to_reference(raw_pts, ref_90k);
                        if let Some(p) = aligned {
                            self.video_pts_unwrapper.init(raw_pts, p);
                        }
                        aligned
                    } else {
                        None
                    };
                    (pts, Some(base_dts))
                } else if let TimingField::Valid(raw_pts) = event.timing.pts {
                    let base_pts_val = i64::try_from(raw_pts.get()).expect("raw_pts fits in i64");
                    let base_pts = ExtendedPts90k::new(base_pts_val);
                    self.epoch_reference_90k = Some(base_pts);
                    self.video_pts_unwrapper.init(raw_pts, base_pts);
                    (Some(base_pts), None)
                } else {
                    (None, None)
                }
            }
            Some(ref_90k) => {
                let dts = if let TimingField::Valid(raw_dts) = event.timing.dts {
                    if self.video_dts_unwrapper.is_initialized() {
                        self.video_dts_unwrapper.unwrap(raw_dts)
                    } else {
                        let aligned = align_dts_to_reference(raw_dts, ref_90k);
                        if let Some(d) = aligned {
                            self.video_dts_unwrapper.init(raw_dts, d);
                        }
                        aligned
                    }
                } else {
                    None
                };

                let pts = if let TimingField::Valid(raw_pts) = event.timing.pts {
                    if self.video_pts_unwrapper.is_initialized() {
                        self.video_pts_unwrapper.unwrap(raw_pts)
                    } else {
                        let aligned = align_90k_to_reference(raw_pts, ref_90k);
                        if let Some(p) = aligned {
                            self.video_pts_unwrapper.init(raw_pts, p);
                        }
                        aligned
                    }
                } else {
                    None
                };

                (pts, dts)
            }
        };

        let point = TimingPoint {
            epoch,
            pid: event.pid,
            observed_at: event.observed_at,
            subject_at: event.subject_at,
            pts,
            dts,
        };

        if let Some(p) = point.pts {
            if self.video_pts_history.len() >= VIDEO_PTS_HISTORY_CAP {
                self.video_pts_history.pop_front();
            }
            self.video_pts_history
                .push_back((point.subject_at.get(), p));
        }

        self.last_video_point = Some(point);
        Some(point)
    }

    /// Observes an audio timing event from [`PesHeaderAssembler`](crate::pes::PesHeaderAssembler).
    ///
    /// # Panics
    ///
    /// Panics if a 33-bit timestamp exceeds `i64::MAX`.
    pub fn observe_audio_timing(&mut self, event: &TimingEvent) -> Option<TimingPoint> {
        let epoch = self.active_epoch?;

        let (pts, dts) = match self.epoch_reference_90k {
            None => {
                // Audio is the first clock sample in the epoch.
                if let TimingField::Valid(raw_dts) = event.timing.dts {
                    let base_dts_val = i64::try_from(raw_dts.get()).expect("raw_dts fits in i64");
                    let base_dts = ExtendedDts90k::new(base_dts_val);
                    let ref_90k = ExtendedPts90k::new(base_dts.get());
                    self.epoch_reference_90k = Some(ref_90k);
                    self.audio_dts_unwrappers
                        .entry(event.pid)
                        .or_default()
                        .init(raw_dts, base_dts);

                    let pts = if let TimingField::Valid(raw_pts) = event.timing.pts {
                        let aligned = align_90k_to_reference(raw_pts, ref_90k);
                        if let Some(p) = aligned {
                            self.audio_pts_unwrappers
                                .entry(event.pid)
                                .or_default()
                                .init(raw_pts, p);
                        }
                        aligned
                    } else {
                        None
                    };
                    (pts, Some(base_dts))
                } else if let TimingField::Valid(raw_pts) = event.timing.pts {
                    let base_pts_val = i64::try_from(raw_pts.get()).expect("raw_pts fits in i64");
                    let base_pts = ExtendedPts90k::new(base_pts_val);
                    self.epoch_reference_90k = Some(base_pts);
                    self.audio_pts_unwrappers
                        .entry(event.pid)
                        .or_default()
                        .init(raw_pts, base_pts);
                    (Some(base_pts), None)
                } else {
                    (None, None)
                }
            }
            Some(ref_90k) => {
                let dts = if let TimingField::Valid(raw_dts) = event.timing.dts {
                    let u = self.audio_dts_unwrappers.entry(event.pid).or_default();
                    if u.is_initialized() {
                        u.unwrap(raw_dts)
                    } else {
                        let aligned = align_dts_to_reference(raw_dts, ref_90k);
                        if let Some(d) = aligned {
                            u.init(raw_dts, d);
                        }
                        aligned
                    }
                } else {
                    None
                };

                let pts = if let TimingField::Valid(raw_pts) = event.timing.pts {
                    let u = self.audio_pts_unwrappers.entry(event.pid).or_default();
                    if u.is_initialized() {
                        u.unwrap(raw_pts)
                    } else {
                        let aligned = align_90k_to_reference(raw_pts, ref_90k);
                        if let Some(p) = aligned {
                            u.init(raw_pts, p);
                        }
                        aligned
                    }
                } else {
                    None
                };

                (pts, dts)
            }
        };

        let point = TimingPoint {
            epoch,
            pid: event.pid,
            observed_at: event.observed_at,
            subject_at: event.subject_at,
            pts,
            dts,
        };

        self.last_audio_points.insert(event.pid, point);
        Some(point)
    }

    /// Binds a Random Access Point (RAP) at `subject_at` to an extended PTS timestamp.
    pub fn bind_rap(&mut self, subject_at: ByteOffset, pts: ExtendedPts90k) {
        self.rap_points.insert(subject_at.get(), pts);
    }

    /// Binds a Random Access Point (RAP) at `subject_at` to an extended PTS timestamp
    /// if available from recent video timing observations or the last video point.
    pub fn bind_rap_if_available(&mut self, subject_at: ByteOffset) -> Option<ExtendedPts90k> {
        if let Some(&(_, pts)) = self
            .video_pts_history
            .iter()
            .rev()
            .find(|&&(off, _)| off == subject_at.get())
        {
            self.rap_points.insert(subject_at.get(), pts);
            return Some(pts);
        }
        if let Some(pts) = self
            .last_video_point
            .as_ref()
            .filter(|pt| pt.subject_at == subject_at)
            .and_then(|pt| pt.pts)
        {
            self.rap_points.insert(subject_at.get(), pts);
            return Some(pts);
        }
        None
    }

    /// Invalidates and removes a previously bound Random Access Point (RAP) at `subject_at`.
    pub fn invalidate_rap(&mut self, subject_at: ByteOffset) {
        self.rap_points.remove(&subject_at.get());
    }

    /// Returns the extended PTS bound to the Random Access Point at `subject_at`, if present and valid.
    #[must_use]
    pub fn get_rap_pts(&self, subject_at: ByteOffset) -> Option<ExtendedPts90k> {
        self.rap_points.get(&subject_at.get()).copied()
    }

    /// Returns the most recently observed video timing point, if any.
    #[must_use]
    pub const fn last_video_point(&self) -> Option<&TimingPoint> {
        self.last_video_point.as_ref()
    }

    /// Returns the most recently observed audio timing point for `pid`, if any.
    #[must_use]
    pub fn last_audio_point(&self, pid: Pid) -> Option<&TimingPoint> {
        self.last_audio_points.get(&pid)
    }

    /// Returns the most recently observed PCR and its packet offset, if any.
    #[must_use]
    pub const fn last_pcr(&self) -> Option<(ExtendedPcr27m, ByteOffset)> {
        self.last_pcr
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::timing::types::{PesTiming, RawDts33, RawPcr27m, RawPts33};

    #[test]
    fn epoch_lifecycle_decoupled_from_pat() {
        let mut tracker = TimelineTracker::new();
        assert_eq!(tracker.active_epoch(), None);

        // First valid PMT arrives -> Epoch 0
        let ep0 = tracker.activate_next_epoch();
        assert_eq!(ep0.get(), 0);
        assert_eq!(tracker.active_epoch(), Some(TimelineEpoch::new(0)));

        // PMT lost / deactivated -> None, but next remains 1
        tracker.deactivate_program();
        assert_eq!(tracker.active_epoch(), None);

        // New PMT arrives -> Epoch 1
        let ep1 = tracker.activate_next_epoch();
        assert_eq!(ep1.get(), 1);
        assert_eq!(tracker.active_epoch(), Some(TimelineEpoch::new(1)));
    }

    #[test]
    fn track_reset_does_not_advance_epoch() {
        let mut tracker = TimelineTracker::new();
        tracker.activate_next_epoch();
        assert_eq!(tracker.active_epoch(), Some(TimelineEpoch::new(0)));

        let audio_pid = Pid::new(257).unwrap();
        tracker.reset_track(audio_pid);
        assert_eq!(tracker.active_epoch(), Some(TimelineEpoch::new(0)));
    }

    #[test]
    fn dts_first_establishes_phase_later_pcr_aligns() {
        let mut tracker = TimelineTracker::new();
        tracker.activate_next_epoch();

        let vpid = Pid::new(256).unwrap();
        // Video PES arrives with raw DTS = 2^33 - 100, PTS = 2^33 - 50
        let event = TimingEvent {
            observed_at: ByteOffset::new(188),
            subject_at: ByteOffset::new(0),
            pid: vpid,
            timing: PesTiming {
                pts: TimingField::Valid(RawPts33::new(RawPts33::MODULUS - 50).unwrap()),
                dts: TimingField::Valid(RawDts33::new(RawDts33::MODULUS - 100).unwrap()),
            },
        };

        let pt = tracker.observe_video_timing(&event).unwrap();
        let dts_bound = i64::try_from(RawDts33::MODULUS - 100).expect("fits in i64");
        assert_eq!(pt.dts.unwrap().get(), dts_bound);
        let pts_target = i64::try_from(RawPts33::MODULUS - 50).expect("fits in i64");
        assert_eq!(pt.pts.unwrap().get(), pts_target);
        assert_eq!(pt.composition_delay_90k(), Some(50));

        // PCR arrives LATER with raw ticks corresponding to 2^33 + 10 (wrapped over 2^33 in 90k)
        // In 27 MHz: (10) * 300 = 3000 ticks
        let pcr_obs = PcrObservation::Sample {
            offset: ByteOffset::new(376),
            raw: RawPcr27m::new(3000).unwrap(),
            discontinuity: false,
        };
        let ext_pcr = tracker.observe_pcr(pcr_obs).unwrap();
        // Must align to the existing phase established by DTS:
        // DTS was 2^33 - 100, PCR at 10 ticks past 0 should be 2^33 + 10 in 90k ticks
        let pcr_90k = ext_pcr.to_90k();
        let expected_pcr_90k = i64::try_from(RawPts33::MODULUS + 10).expect("fits in i64");
        assert_eq!(pcr_90k.get(), expected_pcr_90k);
        let dts_as_pts = ExtendedPts90k::new(pt.dts.unwrap().get());
        assert_eq!(pcr_90k.checked_diff(dts_as_pts), Some(110));
    }

    #[test]
    fn rap_binding_and_invalidation() {
        let mut tracker = TimelineTracker::new();
        tracker.activate_next_epoch();

        let offset = ByteOffset::new(1000);
        let pts = ExtendedPts90k::new(90_000);

        tracker.bind_rap(offset, pts);
        assert_eq!(tracker.get_rap_pts(offset), Some(pts));

        tracker.invalidate_rap(offset);
        assert_eq!(tracker.get_rap_pts(offset), None);
    }
}
