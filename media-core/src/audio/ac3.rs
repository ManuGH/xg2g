// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! AC-3 and E-AC-3 syncframe headers.
//!
//! The two syntaxes share a syncword and are told apart by bsid, which both
//! happen to place in the top five bits of byte 5. Everything else differs.

/// First byte of the syncword.
pub const SYNC_HI: u8 = 0x0B;
/// Second byte of the syncword.
pub const SYNC_LO: u8 = 0x77;

/// What both syntaxes need to answer the channel question.
///
/// E-AC-3 has lfeon in byte 4; AC-3 places it after a run of mix level fields
/// whose presence acmod decides, but even the longest of those runs still ends
/// inside byte 6.
pub const HEADER_BYTES: usize = 7;

/// Full-bandwidth channels per audio coding mode, ATSC A/52 Table 5.8.
///
/// acmod 0 is 1+1 - two independent mono programmes, which is two channels of
/// audio however it is used.
const ACMOD_CHANNELS: [u8; 8] = [2, 1, 2, 3, 3, 4, 4, 5];

/// A/52 Table 5.18 in kbit/s. frmsizecod indexes it in pairs; the low bit
/// distinguishes the two 44.1 kHz frame lengths that share a bitrate.
const AC3_BITRATES: [usize; 19] = [
    32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 448, 512, 576, 640,
];

/// What one syncframe says about its own channel layout.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FrameHeader {
    /// Full-bandwidth channels plus LFE, or 0 when the frame does not describe
    /// the programme's own layout - a dependent substream, or an independent
    /// substream belonging to another programme.
    pub channels: u8,
    /// The raw audio coding mode the count was derived from.
    pub acmod: u8,
    /// The low frequency effects channel, already included in `channels`.
    pub lfe: bool,
    /// An E-AC-3 dependent substream. Its channels extend the independent
    /// substream rather than replacing it, and reconstructing the total needs a
    /// channel map this parser does not read - so it is reported as a fact about
    /// the stream's shape, never as a count.
    pub dependent: bool,
    /// The whole frame, used to step to the next syncword instead of rescanning
    /// frame payload that can contain the sync pattern by chance.
    pub size_bytes: usize,
}

/// Reads the header of a syncframe starting at `b[0]`.
///
/// Returns `None` for anything the syntax does not define. A byte pair that
/// happened to look like a syncword is far more likely than a bitstream this
/// parser has never heard of, and acting on one would put a layout into the
/// facts that no stream ever carried.
#[must_use]
pub fn parse_sync_frame(b: &[u8]) -> Option<FrameHeader> {
    if b.len() < HEADER_BYTES || b[0] != SYNC_HI || b[1] != SYNC_LO {
        return None;
    }
    let bsid = b[5] >> 3;
    if bsid <= 10 {
        // bsid 9 and 10 are the alternate bit rate variants; they carry the same
        // bit stream information syntax as 8.
        parse_ac3(b)
    } else if bsid <= 16 {
        parse_eac3(b)
    } else {
        None
    }
}

/// Reads the A/52 syncinfo and bsi fields up to lfeon.
fn parse_ac3(b: &[u8]) -> Option<FrameHeader> {
    let fscod = b[4] >> 6;
    let frmsizecod = b[4] & 0x3F;
    if fscod == 0x03 || frmsizecod > 37 {
        // Reserved sample rate or frame size code. Treated as "not a frame"
        // rather than as a frame with unknown fields.
        return None;
    }

    let acmod = b[6] >> 5;

    // lfeon sits behind the mix level fields, and which of those are present is
    // decided by acmod itself.
    let mut offset = 3u32;
    if acmod & 0x01 != 0 && acmod != 0x01 {
        offset += 2; // cmixlev, present for the modes with a centre channel
    }
    if acmod & 0x04 != 0 {
        offset += 2; // surmixlev, present for the modes with surround channels
    }
    if acmod == 0x02 {
        offset += 2; // dsurmod, only 2/0 declares Dolby Surround encoding
    }
    let lfe = (b[6] >> (7 - offset)) & 0x01 == 1;

    let mut channels = ACMOD_CHANNELS[usize::from(acmod)];
    if lfe {
        channels += 1;
    }
    Some(FrameHeader {
        channels,
        acmod,
        lfe,
        dependent: false,
        size_bytes: ac3_frame_bytes(fscod, frmsizecod),
    })
}

/// A/52 Table 5.18. At 48 and 32 kHz the frame is a whole number of words per
/// kbit/s; at 44.1 kHz it is not, which is why the table lists two lengths per
/// bitrate and frmsizecod's low bit picks between them.
fn ac3_frame_bytes(fscod: u8, frmsizecod: u8) -> usize {
    let bitrate = AC3_BITRATES[usize::from(frmsizecod >> 1)];

    let words = match fscod {
        0x00 => bitrate * 2, // 48 kHz
        0x01 => (bitrate * 1536 * 1000) / (44100 * 16) + usize::from(frmsizecod & 0x01), // 44.1 kHz
        _ => bitrate * 3,    // 32 kHz
    };
    words * 2
}

/// Reads the A/52 Annex E bit stream information.
fn parse_eac3(b: &[u8]) -> Option<FrameHeader> {
    let strmtyp = b[2] >> 6;
    if strmtyp == 0x03 {
        return None; // reserved
    }
    let substream_id = (b[2] >> 3) & 0x07;
    let frmsiz = (u16::from(b[2] & 0x07) << 8) | u16::from(b[3]);

    let acmod = (b[4] >> 1) & 0x07;
    let lfe = b[4] & 0x01 == 1;

    let mut h = FrameHeader {
        channels: 0,
        acmod,
        lfe,
        dependent: false,
        size_bytes: (usize::from(frmsiz) + 1) * 2,
    };

    if strmtyp == 0x01 {
        // Dependent substream: its acmod describes the channels it adds, not the
        // programme's total.
        h.dependent = true;
    } else if substream_id == 0 {
        h.channels = ACMOD_CHANNELS[usize::from(acmod)];
        if lfe {
            h.channels += 1;
        }
    }
    // An independent substream other than 0 is a different programme in the same
    // elementary stream. Not this track's layout, and not a count.
    Some(h)
}
