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

/// Extended 64-bit Decode Time Stamp in 90 kHz ticks (unwrapped, Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ExtendedDts90k(pub i64);

/// Extended 64-bit Program Clock Reference in 27 MHz ticks (unwrapped, Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ExtendedPcr27m(pub i64);

/// A monotonic epoch counter for stream timeline segments (Step 8c).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct TimelineEpoch(pub u64);

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
