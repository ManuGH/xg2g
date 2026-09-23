// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Strict newtypes for media transport coordinates and timing units.

use super::pcr::PCR_MODULUS;

/// A 13-bit transport stream packet identifier (0..=0x1FFF).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Pid(u16);

impl Pid {
    /// Null packet PID (0x1FFF).
    pub const NULL: Self = Self(0x1FFF);
    /// Program Association Table PID (0x0000).
    pub const PAT: Self = Self(0x0000);

    /// Constructs a PID, validating that it fits within the 13-bit MPEG-2 TS range.
    #[must_use]
    pub const fn new(value: u16) -> Option<Self> {
        if value <= 0x1FFF {
            Some(Self(value))
        } else {
            None
        }
    }

    /// Returns the raw 16-bit PID value.
    #[must_use]
    pub const fn get(self) -> u16 {
        self.0
    }
}

/// A transport stream byte offset in the caller's monotonic coordinate system.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ByteOffset(i64);

impl ByteOffset {
    /// Constructs a byte offset.
    #[must_use]
    pub const fn new(value: i64) -> Self {
        Self(value)
    }

    /// Returns the raw 64-bit byte offset value.
    #[must_use]
    pub const fn get(self) -> i64 {
        self.0
    }

    /// Safe checked subtraction between offsets.
    #[must_use]
    pub fn checked_sub(self, other: Self) -> Option<u64> {
        self.0
            .checked_sub(other.0)
            .and_then(|v| u64::try_from(v).ok())
    }
}

/// A raw 33-bit Presentation Time Stamp at 90 kHz (strictly < 2^33).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct RawPts33(u64);

impl RawPts33 {
    /// Modulus for 33-bit timestamps: 2^33.
    pub const MODULUS: u64 = 1u64 << 33;
    /// Maximum legal 33-bit timestamp value.
    pub const MAX_VALUE: u64 = Self::MODULUS - 1;

    /// Constructs a raw PTS, enforcing that `value < 2^33`.
    #[must_use]
    pub const fn new(value: u64) -> Option<Self> {
        if value < Self::MODULUS {
            Some(Self(value))
        } else {
            None
        }
    }

    /// Returns the raw 33-bit timestamp in 90 kHz ticks.
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
}

/// A raw 33-bit Decode Time Stamp at 90 kHz (strictly < 2^33).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct RawDts33(u64);

impl RawDts33 {
    /// Modulus for 33-bit timestamps: 2^33.
    pub const MODULUS: u64 = 1u64 << 33;
    /// Maximum legal 33-bit timestamp value.
    pub const MAX_VALUE: u64 = Self::MODULUS - 1;

    /// Constructs a raw DTS, enforcing that `value < 2^33`.
    #[must_use]
    pub const fn new(value: u64) -> Option<Self> {
        if value < Self::MODULUS {
            Some(Self(value))
        } else {
            None
        }
    }

    /// Returns the raw 33-bit timestamp in 90 kHz ticks.
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
}

/// A raw 27 MHz Program Clock Reference timestamp (strictly < 2^33 * 300).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct RawPcr27m(u64);

impl RawPcr27m {
    /// Modulus for 27 MHz PCR: 2^33 * 300.
    pub const MODULUS: u64 = PCR_MODULUS;

    /// Constructs a raw PCR, enforcing that `value < PCR_MODULUS`.
    #[must_use]
    pub const fn new(value: u64) -> Option<Self> {
        if value < Self::MODULUS {
            Some(Self(value))
        } else {
            None
        }
    }

    /// Returns the raw 27 MHz PCR value.
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
}

/// Extended 64-bit Presentation Time Stamp in 90 kHz ticks (unwrapped, Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ExtendedPts90k(pub i64);

impl ExtendedPts90k {
    /// Constructs an extended PTS.
    #[must_use]
    pub const fn new(value: i64) -> Self {
        Self(value)
    }

    /// Returns the extended PTS value in 90 kHz ticks.
    #[must_use]
    pub const fn get(self) -> i64 {
        self.0
    }

    /// Checked addition of 90 kHz ticks.
    #[must_use]
    pub fn checked_add_ticks(self, ticks: i64) -> Option<Self> {
        self.0.checked_add(ticks).map(Self)
    }

    /// Checked difference between two extended PTS values in 90 kHz ticks.
    #[must_use]
    pub fn checked_diff(self, other: Self) -> Option<i64> {
        self.0.checked_sub(other.0)
    }
}

/// Extended 64-bit Decode Time Stamp in 90 kHz ticks (unwrapped, Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ExtendedDts90k(pub i64);

impl ExtendedDts90k {
    /// Constructs an extended DTS.
    #[must_use]
    pub const fn new(value: i64) -> Self {
        Self(value)
    }

    /// Returns the extended DTS value in 90 kHz ticks.
    #[must_use]
    pub const fn get(self) -> i64 {
        self.0
    }

    /// Checked addition of 90 kHz ticks.
    #[must_use]
    pub fn checked_add_ticks(self, ticks: i64) -> Option<Self> {
        self.0.checked_add(ticks).map(Self)
    }

    /// Checked difference between two extended DTS values in 90 kHz ticks.
    #[must_use]
    pub fn checked_diff(self, other: Self) -> Option<i64> {
        self.0.checked_sub(other.0)
    }
}

/// Extended 64-bit Program Clock Reference in 27 MHz ticks (unwrapped, Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ExtendedPcr27m(pub i64);

impl ExtendedPcr27m {
    /// Constructs an extended PCR.
    #[must_use]
    pub const fn new(value: i64) -> Self {
        Self(value)
    }

    /// Returns the extended PCR value in 27 MHz ticks.
    #[must_use]
    pub const fn get(self) -> i64 {
        self.0
    }

    /// Checked addition of 27 MHz ticks.
    #[must_use]
    pub fn checked_add_ticks(self, ticks: i64) -> Option<Self> {
        self.0.checked_add(ticks).map(Self)
    }

    /// Checked difference between two extended PCR values in 27 MHz ticks.
    #[must_use]
    pub fn checked_diff(self, other: Self) -> Option<i64> {
        self.0.checked_sub(other.0)
    }

    /// Converts 27 MHz PCR ticks to 90 kHz ticks (`ticks / 300`).
    #[must_use]
    pub fn to_90k(self) -> ExtendedPts90k {
        ExtendedPts90k(self.0 / 300)
    }
}

/// A monotonic epoch counter for stream timeline segments (Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct TimelineEpoch(pub u64);

impl TimelineEpoch {
    /// Constructs a timeline epoch.
    #[must_use]
    pub const fn new(value: u64) -> Self {
        Self(value)
    }

    /// Returns the raw epoch number.
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }

    /// Increments the epoch.
    ///
    /// # Panics
    ///
    /// Panics if the 64-bit epoch counter overflows.
    #[must_use]
    pub fn next(self) -> Self {
        Self(
            self.0
                .checked_add(1)
                .expect("timeline epoch counter exhausted"),
        )
    }
}

/// Scope of a timing reset event.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TimingResetScope {
    /// Program-level discontinuity: affects entire program timeline (epoch transition).
    Program,
    /// Track-local timing loss: affects only the specified PID without advancing epoch.
    Track(Pid),
}

/// Explicit causal reason for a timeline discontinuity.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DiscontinuityReason {
    /// Program identity changed (PMT update or new program selection).
    ProgramIdentityChanged,
    /// PCR PID changed without full program replacement.
    PcrPidChanged,
    /// Transport packet discontinuity indicator asserted on the active PCR PID.
    PcrDiscontinuityIndicator,
    /// Unrecoverable loss of transport timing continuity.
    TransportTimingLoss,
}

/// A correlated timing point binding stream coordinates, epoch, and timestamps.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct TimingPoint {
    /// The timeline epoch this point belongs to.
    pub epoch: TimelineEpoch,
    /// The transport PID.
    pub pid: Pid,
    /// Transport byte offset where the timing was recognized.
    pub observed_at: ByteOffset,
    /// Transport byte offset of the PES start packet (PUSI).
    pub subject_at: ByteOffset,
    /// Presentation Time Stamp, unwrapped and phase-aligned.
    pub pts: Option<ExtendedPts90k>,
    /// Decode Time Stamp, unwrapped and phase-aligned.
    pub dts: Option<ExtendedDts90k>,
}

impl TimingPoint {
    /// Composition buffer delay (`PTS - DTS`) in 90 kHz ticks, if both are present.
    #[must_use]
    pub fn composition_delay_90k(&self) -> Option<i64> {
        match (self.pts, self.dts) {
            (Some(p), Some(d)) => p.checked_diff(ExtendedPts90k(d.0)),
            _ => None,
        }
    }
}

/// A canonical timing record emitted during chunk processing in transport order.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TimingRecord {
    /// A correlated PES presentation/decode timing point.
    Pes(TimingPoint),
    /// A Random Access Point bound to the canonical timing of the PES that carries it.
    ///
    /// Emitted in the same chunk, at the same packet, as the
    /// `VideoEvent::RandomAccessPoint` it binds, whichever chunk the PES header
    /// arrived in. `subject_at` is the access point's offset (the start of its PES),
    /// `observed_at` is the packet at which the access point was established, and
    /// `epoch`, `pid`, `pts` and `dts` are that PES's canonical timing. A RAP whose
    /// PES has no canonical timing point gets no record: unbound is stated by
    /// absence, never by a zero.
    RandomAccessPoint(TimingPoint),
    /// A canonical PCR observation accepted by the timeline tracker.
    Pcr {
        /// Timeline epoch this PCR belongs to.
        epoch: TimelineEpoch,
        /// PCR PID.
        pid: Pid,
        /// Byte offset where the PCR packet was observed.
        observed_at: ByteOffset,
        /// 27 MHz PCR timestamp, unwrapped and signed.
        pcr_27m: ExtendedPcr27m,
    },
    /// A timeline discontinuity or track-local reset event.
    Discontinuity {
        /// Program or Track scope.
        scope: TimingResetScope,
        /// Causal reason for the discontinuity.
        reason: DiscontinuityReason,
        /// Byte offset where the discontinuity was observed.
        observed_at: ByteOffset,
        /// Timeline epoch before the discontinuity, if an epoch was active.
        epoch_before: Option<TimelineEpoch>,
        /// Timeline epoch after the discontinuity, if an epoch is active.
        epoch_after: Option<TimelineEpoch>,
    },
}

/// Estimated transport stream bitrate in bits per second.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct BitrateBps(pub u64);

impl BitrateBps {
    /// Constructs a bitrate value.
    #[must_use]
    pub const fn new(value: u64) -> Self {
        Self(value)
    }

    /// Returns the bitrate in bits per second.
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
}

/// The presence and syntactic validity of a timing field in a PES header.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum TimingField<T> {
    /// Sender declared no timestamp (e.g. `PTS_DTS_flags == 00`).
    #[default]
    Absent,
    /// Sender provided a syntactically valid timestamp.
    Valid(T),
    /// Syntax error (forbidden flags `01`, truncated header, bad marker bits, or invalid prefix).
    Invalid,
}

impl<T> TimingField<T> {
    /// Returns `true` if a valid timestamp is present.
    #[must_use]
    pub const fn is_valid(&self) -> bool {
        matches!(self, Self::Valid(_))
    }

    /// Returns `true` if the field is absent.
    #[must_use]
    pub const fn is_absent(&self) -> bool {
        matches!(self, Self::Absent)
    }

    /// Returns `true` if the field was declared but syntactically invalid.
    #[must_use]
    pub const fn is_invalid(&self) -> bool {
        matches!(self, Self::Invalid)
    }

    /// Returns the inner value if valid.
    #[must_use]
    pub const fn as_valid(&self) -> Option<&T> {
        match self {
            Self::Valid(val) => Some(val),
            _ => None,
        }
    }
}

/// Timing timestamps parsed from a PES header.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct PesTiming {
    /// Presentation Time Stamp.
    pub pts: TimingField<RawPts33>,
    /// Decode Time Stamp.
    pub dts: TimingField<RawDts33>,
}

/// A timing event emitted when a PES header's timing is determined.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct TimingEvent {
    /// Transport byte offset where sufficient header bytes were available to parse timing.
    pub observed_at: ByteOffset,
    /// Transport byte offset of the PES start packet (PUSI).
    pub subject_at: ByteOffset,
    /// The stream PID this timing event belongs to.
    pub pid: Pid,
    /// The parsed PES timing.
    pub timing: PesTiming,
}
