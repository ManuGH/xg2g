// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Where a PES packet begins, and where its elementary stream does.
//!
//! This reads syntax and answers structural questions. It does not decide
//! whether a stream id belongs to video or to audio, because those two callers
//! do not agree and should not be made to: the reference accepts `0xE0..0xEF`
//! for video and `0xBD`, `0xFD`, `0xC0..0xDF` for audio, and it treats a
//! payload that fails the check differently in each case. Folding that into one
//! parser would mean choosing one caller's policy for both.
//!
//! So the reader says what the bytes are and the caller says what to do about
//! it. [`is_video_stream_id`] and [`is_audio_stream_id`] are offered for the
//! callers that want them; nothing here applies them.
//!
//! What this module does not do: interpret PTS, DTS, ESCR or PCR. Their bytes
//! live inside the optional header and are stepped over as opaque, because
//! reading them is a timing decision and timing is a later step. Nor does it
//! assemble a whole PES packet - `PES_packet_length` is read as a field, not
//! honoured as a buffering instruction.
//!
//! Offsets are the caller's. A PES start is reported as a fact about a payload;
//! which byte coordinate that payload had is something only the caller knows,
//! and inventing a second coordinate system here is how two of them end up
//! disagreeing.

use crate::transport::PacketView;

/// The three bytes every PES packet starts with.
const START_CODE: [u8; 3] = [0x00, 0x00, 0x01];

/// The fixed part of a PES header for the stream ids that carry an optional
/// header: start code, stream id, packet length, two flag bytes and the length
/// of the optional header itself.
const FIXED_HEADER_LEN: usize = 9;

/// Where the optional header's own length is written.
const HEADER_DATA_LENGTH_AT: usize = 8;

/// The bytes before the optional header: start code, stream id, packet length.
const MINIMUM_HEADER_LEN: usize = 6;

/// What one transport payload said about a PES packet starting in it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PesStart<'a> {
    /// The payload does not begin a PES packet: the start code is not there.
    ///
    /// This is a statement about these bytes and not about the stream. A
    /// continuation payload is not a start either, and the caller knows which
    /// it is holding because the caller knows the payload unit start flag.
    NotAStart,

    /// The start code is there, or cannot be ruled out, but the fixed header is
    /// not complete in this payload.
    ///
    /// Distinct from a header that merely extends past the payload: here not
    /// even the length of the optional part could be read, so nothing is known
    /// about where the elementary stream would begin.
    Truncated {
        /// How many bytes the payload actually held.
        available: usize,
    },

    /// A PES packet starts here whose stream id carries no optional header at
    /// all, so its data begins immediately after the six fixed bytes.
    ///
    /// Padding, ECM, EMM and the stream-map ids are laid out this way. Reading
    /// byte eight of one of them as a header length invents a header: a padding
    /// stream stuffed with `0xFF` would declare 255 bytes of optional header
    /// that do not exist, and every byte after would be attributed wrongly.
    NoOptionalHeader {
        /// Which stream this packet belongs to.
        stream_id: u8,
        /// The `PES_packet_length` field, as written.
        packet_length: u16,
        /// The packet's data bytes in this payload.
        data: &'a [u8],
    },

    /// A PES packet starts here and its optional header ends inside this
    /// payload.
    ///
    /// `es` may be empty, which is not the same as the header being incomplete:
    /// the header finished exactly at the end of the payload and the elementary
    /// stream begins in the next one.
    Complete {
        /// Which elementary stream this packet belongs to.
        stream_id: u8,
        /// The `PES_packet_length` field, as written. Zero is legal and means
        /// unbounded for video.
        packet_length: u16,
        /// The length of the optional header after the fixed nine bytes.
        header_data_length: u8,
        /// The elementary stream bytes in this payload, after the header.
        es: &'a [u8],
    },

    /// A PES packet starts here and its optional header reaches past the end of
    /// this payload.
    ///
    /// The elementary stream has not begun. Saying so is the point: the bytes
    /// at the start of the next payload are the rest of this header, and a
    /// consumer told nothing would read them as elementary stream.
    HeaderIncomplete {
        /// Which elementary stream this packet belongs to.
        stream_id: u8,
        /// The `PES_packet_length` field, as written.
        packet_length: u16,
        /// The length of the optional header after the fixed nine bytes.
        header_data_length: u8,
        /// How many header bytes are still to come in later payloads.
        remaining_header: usize,
    },
}

/// Reads what a transport payload says about a PES packet starting in it.
///
/// The caller decides whether to ask: a payload whose packet did not set the
/// payload unit start flag continues a PES packet rather than beginning one,
/// and asking this about it would be asking the wrong question.
///
/// Arithmetic is bounds-checked rather than clamped. A header longer than the
/// payload is reported as reaching past it, not shortened until it fits: a
/// shortened length is an invented one, and inventing it here is how header
/// bytes reach a consumer as elementary stream.
#[must_use]
pub fn read_start(payload: &[u8]) -> PesStart<'_> {
    // Fewer than three bytes cannot even be compared against the start code, so
    // whether a packet starts here is unknown rather than answered.
    if payload.len() < START_CODE.len() {
        return PesStart::Truncated {
            available: payload.len(),
        };
    }
    if payload[..START_CODE.len()] != START_CODE {
        return PesStart::NotAStart;
    }
    // The stream id and the packet length come before the optional header, and
    // whether there is an optional header at all depends on the stream id - so
    // those six bytes are read first and the rest is decided from them.
    if payload.len() < MINIMUM_HEADER_LEN {
        return PesStart::Truncated {
            available: payload.len(),
        };
    }

    let stream_id = payload[3];
    let packet_length = u16::from_be_bytes([payload[4], payload[5]]);

    if !has_optional_header(stream_id) {
        return PesStart::NoOptionalHeader {
            stream_id,
            packet_length,
            data: &payload[MINIMUM_HEADER_LEN..],
        };
    }

    if payload.len() < FIXED_HEADER_LEN {
        return PesStart::Truncated {
            available: payload.len(),
        };
    }

    let header_data_length = payload[HEADER_DATA_LENGTH_AT];
    let es_start = FIXED_HEADER_LEN + usize::from(header_data_length);

    if es_start <= payload.len() {
        PesStart::Complete {
            stream_id,
            packet_length,
            header_data_length,
            es: &payload[es_start..],
        }
    } else {
        PesStart::HeaderIncomplete {
            stream_id,
            packet_length,
            header_data_length,
            remaining_header: es_start - payload.len(),
        }
    }
}

/// Reads a PES start from a transport packet, when that packet begins one.
///
/// A convenience over [`read_start`] that takes the payload unit start flag and
/// the payload from the packet rather than from the caller, so the two cannot
/// be paired up wrongly. Returns `None` for a packet with no payload or one
/// that does not start a payload unit.
///
/// Scrambled payloads are refused. Searching encrypted bytes for a start code
/// finds one eventually and it means nothing. That the packet is scrambled is a
/// transport fact this returns to the caller by declining to read it; what it
/// means for a viewer is not decided here.
#[must_use]
pub fn read_packet_start<'a>(view: &PacketView<'a>) -> Option<PesStart<'a>> {
    if !view.payload_unit_start() || view.scrambling_control() != 0 {
        return None;
    }
    view.payload().map(read_start)
}

/// Whether a stream id carries the optional PES header.
///
/// Most do. These do not, and their data begins immediately after the six fixed
/// bytes: `program_stream_map` (0xBC), `padding_stream` (0xBE),
/// `private_stream_2` (0xBF), ECM (0xF0), EMM (0xF1), DSM-CC (0xF2), H.222.1
/// type E (0xF8) and `program_stream_directory` (0xFF).
///
/// Reading byte eight of one of those as a header length invents a header that
/// is not there. A padding stream stuffed with `0xFF` would declare 255 bytes
/// of it.
#[must_use]
pub fn has_optional_header(stream_id: u8) -> bool {
    !matches!(
        stream_id,
        0xBC | 0xBE | 0xBF | 0xF0 | 0xF1 | 0xF2 | 0xF8 | 0xFF
    )
}

/// Whether a stream id is one the video reference accepts.
///
/// Offered for callers, applied by none: video and audio disagree about which
/// ids are theirs and about what to do with a payload that fails, so the
/// decision stays with whoever is asking.
#[must_use]
pub fn is_video_stream_id(stream_id: u8) -> bool {
    (0xE0..=0xEF).contains(&stream_id)
}

/// Whether a stream id is one the audio reference accepts.
///
/// `0xBD` is `private_stream_1`, which is where AC-3 lives in DVB, and `0xFD`
/// is `extended_stream_id`. Neither is an audio stream id in the numbering; both are
/// audio in this product's streams.
#[must_use]
pub fn is_audio_stream_id(stream_id: u8) -> bool {
    stream_id == 0xBD || stream_id == 0xFD || (0xC0..=0xDF).contains(&stream_id)
}

#[cfg(test)]
mod corpus_test;
