// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! One reading of a transport stream packet, for everything that needs one.
//!
//! Before this module the PSI code decoded bytes 1 to 3 itself, and anything
//! else that came to need a PID or a payload boundary would have decoded them
//! again. Two decoders of the same four bytes are two chances to disagree about
//! what a packet says, and the disagreement would not show up as a compile
//! error - it would show up as one consumer following a stream the other had
//! given up on.
//!
//! So the packet header is read once, here, and the things built on top of it
//! keep only their own state: PSI keeps sections and table versions, and a
//! later stage keeps whatever it needs. What a packet *is* stops being any of
//! their business.
//!
//! This module deliberately stops at transport syntax. It says a packet is
//! scrambled; it does not say the channel is broken. It says the counter did
//! not advance; it does not say the viewer should be dropped. Those are
//! decisions, and decisions are made where the product is, not here.

/// The size of a transport stream packet.
pub const TS_PACKET_LEN: usize = 188;

/// The byte every transport stream packet starts with.
pub const SYNC_BYTE: u8 = 0x47;

/// The fixed header, before any adaptation field.
const HEADER_LEN: usize = 4;

/// Why a packet could not be read.
///
/// Each of these is a packet that says something impossible about itself. None
/// of them is repaired: a length that runs past the end of the packet is not
/// clamped to fit, because a clamped length is an invented one, and inventing
/// one here would hand the next stage bytes that were never a payload.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TransportError {
    /// The slice was not exactly one packet long.
    WrongLength(usize),
    /// The packet did not begin with the sync byte.
    NoSyncByte(u8),
    /// `adaptation_field_control` 0 is reserved and carries neither payload nor
    /// adaptation field, so a packet using it describes nothing.
    ReservedAdaptationControl,
    /// The adaptation field length reaches past the end of the packet.
    AdaptationOverrun {
        /// The length the packet declared.
        declared: usize,
    },
}

/// What one packet says about itself.
///
/// Borrowed rather than copied: a packet is 188 bytes and a stream is millions
/// of them, so this is a view onto bytes the caller already has.
#[derive(Debug, Clone, Copy)]
pub struct PacketView<'a> {
    packet: &'a [u8],
    /// Where the payload begins, when there is one.
    payload_at: Option<usize>,
    /// The adaptation field's own bytes, after its length byte.
    adaptation: Option<(usize, usize)>,
}

impl<'a> PacketView<'a> {
    /// Reads one complete transport packet.
    ///
    /// Complete is required rather than assumed. The ingest contract already
    /// guarantees whole packets, so a short slice here is a caller bug, and
    /// returning an error for it is how that bug is found rather than
    /// misinterpreted.
    ///
    /// # Errors
    ///
    /// Returns [`TransportError`] when the slice is not exactly one packet, does
    /// not start with the sync byte, uses the reserved `adaptation_field_control`
    /// value, or declares an adaptation field reaching past the end of the
    /// packet.
    pub fn parse(packet: &'a [u8]) -> Result<Self, TransportError> {
        if packet.len() != TS_PACKET_LEN {
            return Err(TransportError::WrongLength(packet.len()));
        }
        if packet[0] != SYNC_BYTE {
            return Err(TransportError::NoSyncByte(packet[0]));
        }

        let afc = (packet[3] >> 4) & 0x03;
        let (adaptation, payload_at) = match afc {
            // Reserved. Not a packet with no payload - a packet that means
            // nothing at all.
            0b00 => return Err(TransportError::ReservedAdaptationControl),

            // Payload only: it starts straight after the header.
            0b01 => (None, Some(HEADER_LEN)),

            // Adaptation field only, filling the rest of the packet.
            0b10 => {
                let declared = usize::from(packet[HEADER_LEN]);
                // The length byte counts the bytes after itself, and there are
                // 183 of them.
                if declared > TS_PACKET_LEN - HEADER_LEN - 1 {
                    return Err(TransportError::AdaptationOverrun { declared });
                }
                let start = HEADER_LEN + 1;
                (Some((start, start + declared)), None)
            }

            // Adaptation field, then payload.
            _ => {
                let declared = usize::from(packet[HEADER_LEN]);
                let start = HEADER_LEN + 1;
                let payload_at = start + declared;
                if payload_at > TS_PACKET_LEN {
                    return Err(TransportError::AdaptationOverrun { declared });
                }
                // A field that reaches exactly the end leaves no payload behind
                // it, whatever the control bits promised. That is malformed but
                // readable: the packet is reported as carrying no payload
                // rather than refused, because nothing about it is ambiguous.
                let payload = if payload_at == TS_PACKET_LEN {
                    None
                } else {
                    Some(payload_at)
                };
                (Some((start, payload_at)), payload)
            }
        };

        Ok(Self {
            packet,
            payload_at,
            adaptation,
        })
    }

    /// The whole packet, as it arrived.
    #[must_use]
    pub fn bytes(&self) -> &'a [u8] {
        self.packet
    }

    /// The stream this packet belongs to.
    #[must_use]
    pub fn pid(&self) -> u16 {
        (u16::from(self.packet[1] & 0x1F) << 8) | u16::from(self.packet[2])
    }

    /// Whether the sender marked this packet as damaged in transit.
    ///
    /// Read and reported, not acted on. Neither the reference nor this core
    /// currently refuses a packet for it, and starting to here would change
    /// what a proven stream parses to.
    #[must_use]
    pub fn transport_error_indicator(&self) -> bool {
        self.packet[1] & 0x80 != 0
    }

    /// Whether a new payload unit starts in this packet.
    #[must_use]
    pub fn payload_unit_start(&self) -> bool {
        self.packet[1] & 0x40 != 0
    }

    /// The transport priority bit.
    #[must_use]
    pub fn transport_priority(&self) -> bool {
        self.packet[1] & 0x20 != 0
    }

    /// Whether, and with which key, the payload is scrambled.
    ///
    /// Zero means clear. What a nonzero value means for a viewer is not decided
    /// here; this is the bit, not the verdict.
    #[must_use]
    pub fn scrambling_control(&self) -> u8 {
        (self.packet[3] >> 6) & 0x03
    }

    /// The raw `adaptation_field_control` field.
    #[must_use]
    pub fn adaptation_field_control(&self) -> u8 {
        (self.packet[3] >> 4) & 0x03
    }

    /// The counter that should advance by one per payload-carrying packet on a PID.
    #[must_use]
    pub fn continuity_counter(&self) -> u8 {
        self.packet[3] & 0x0F
    }

    /// Whether this packet carries payload bytes.
    #[must_use]
    pub fn has_payload(&self) -> bool {
        self.payload_at.is_some()
    }

    /// Whether this packet carries an adaptation field.
    #[must_use]
    pub fn has_adaptation(&self) -> bool {
        self.adaptation.is_some()
    }

    /// The payload, when there is one.
    #[must_use]
    pub fn payload(&self) -> Option<&'a [u8]> {
        self.payload_at.map(|at| &self.packet[at..])
    }

    /// The adaptation field's bytes, after its length byte.
    #[must_use]
    pub fn adaptation_field(&self) -> Option<&'a [u8]> {
        self.adaptation.map(|(a, b)| &self.packet[a..b])
    }

    /// Whether the sender announced a discontinuity.
    ///
    /// An announced break and an unexplained one are not the same event, and
    /// telling them apart needs this flag - which is why it is exposed. Nothing
    /// in this step acts on it: PSI's proven behaviour treats every counter
    /// break alike, and quietly changing that here would be a semantic change
    /// wearing a refactor's clothes.
    #[must_use]
    pub fn discontinuity_indicator(&self) -> bool {
        match self.adaptation_field() {
            // The flags live in the first byte after the length, so a
            // zero-length field announces nothing.
            Some(field) => field.first().is_some_and(|flags| flags & 0x80 != 0),
            None => false,
        }
    }
}

/// What the continuity counter said about one packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Continuity {
    /// Nothing had been seen on this PID yet, so there was nothing to compare.
    First,
    /// The counter advanced the way it should have.
    Continuous,
    /// The same counter and the same bytes: the transport said it twice.
    Duplicate,
    /// The counter did not advance the way it should have, so bytes are missing
    /// or the sender contradicted itself.
    Broken,
}

/// Follows the continuity counter of one PID.
///
/// Kept apart from the packet reader because it is the one part of transport
/// interpretation that has memory, and apart from the PSI assembler because a
/// later stage will need the same answer about its own PIDs. Nothing here knows
/// what the payload is for.
///
/// Feed it only packets that carry payload. A packet with an adaptation field
/// and no payload does not advance the counter, and handing it to a tracker
/// that expects an increment would turn a legal stream into a broken one.
#[derive(Debug, Default)]
pub struct ContinuityTracker {
    last_cc: u8,
    has_cc: bool,
    /// The previous packet, kept whole. Telling a duplicate from a loss needs
    /// the bytes: the counter alone cannot say which of the two happened.
    last_packet: Vec<u8>,
}

impl ContinuityTracker {
    /// A tracker that has seen nothing.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Forgets the counter, so the next packet is treated as the first.
    pub fn reset(&mut self) {
        self.has_cc = false;
        self.last_packet.clear();
    }

    /// How many bytes this tracker is holding.
    #[must_use]
    pub fn retained_bytes(&self) -> usize {
        self.last_packet.len()
    }

    /// Compares one payload-carrying packet against the one before it.
    ///
    /// A duplicate leaves the tracker untouched, because the packet it repeats
    /// is still the last one that meant anything. Every other outcome records
    /// this packet as the new reference - including a break, so that one lost
    /// packet does not make every packet after it look lost too.
    pub fn observe(&mut self, packet: &[u8], continuity_counter: u8) -> Continuity {
        let cc = continuity_counter & 0x0F;

        let verdict = if !self.has_cc {
            Continuity::First
        } else if cc == self.last_cc {
            if packet == self.last_packet.as_slice() {
                return Continuity::Duplicate;
            }
            // One counter value cannot describe two different packets.
            Continuity::Broken
        } else if cc != (self.last_cc.wrapping_add(1)) & 0x0F {
            Continuity::Broken
        } else {
            Continuity::Continuous
        };

        self.last_cc = cc;
        self.has_cc = true;
        self.last_packet.clear();
        self.last_packet.extend_from_slice(packet);
        verdict
    }
}

#[cfg(test)]
mod corpus_test;
