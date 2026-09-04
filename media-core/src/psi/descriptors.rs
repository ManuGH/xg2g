// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What the descriptors of one elementary stream say about it.
//!
//! The block handed in here has already been bounded by the elementary stream
//! loop, so nothing in this file can reach a section's CRC. What it still has to
//! survive is a descriptor whose own declared length runs past the block: the
//! walk stops there rather than reading what follows, because a length that is
//! wrong is not a reason to guess where the next descriptor starts.

/// Descriptor tags this parser assigns meaning to.
mod tag {
    /// ISO 639 language descriptor.
    pub(super) const LANGUAGE: u8 = 0x0A;
    /// AC-3 descriptor, ETSI EN 300 468 Annex D.
    pub(super) const AC3: u8 = 0x6A;
    /// Enhanced AC-3 descriptor.
    pub(super) const ENHANCED_AC3: u8 = 0x7A;
    /// DTS descriptor.
    pub(super) const DTS: u8 = 0x7B;
    /// AAC descriptor.
    pub(super) const AAC: u8 = 0x7C;
    /// DTS-HD descriptor.
    pub(super) const DTS_HD: u8 = 0x7D;
    /// Registration descriptor, whose format identifier names some codecs.
    pub(super) const REGISTRATION: u8 = 0x05;
}

/// The format identifiers a registration descriptor can use to mean audio.
const AUDIO_FORMAT_IDS: [&[u8; 4]; 5] = [b"AC-3", b"EAC3", b"DTS1", b"DTS2", b"DTS3"];

/// Walks a descriptor block, yielding `(tag, body)` for each descriptor that
/// fits inside it.
///
/// Stops at the first descriptor whose declared length runs past the block, and
/// at a trailing byte too short to be a header. Both are the same decision: the
/// length field is the only thing that says where the next descriptor begins, so
/// a length that does not fit ends the walk instead of starting a guess.
fn walk(block: &[u8]) -> impl Iterator<Item = (u8, &[u8])> {
    let mut at = 0usize;
    core::iter::from_fn(move || {
        let header_end = at.checked_add(2)?;
        if header_end > block.len() {
            return None;
        }
        let tag = block[at];
        let length = usize::from(block[at + 1]);
        let body_end = header_end.checked_add(length)?;
        if body_end > block.len() {
            return None;
        }
        let body = &block[header_end..body_end];
        at = body_end;
        Some((tag, body))
    })
}

/// Whether a descriptor block says its stream carries audio.
///
/// Only consulted for stream types that do not say so themselves. Stream type
/// 0x06 is "PES carrying private data", which is what European broadcasters use
/// for AC-3, E-AC-3 and DTS - and also what subtitles and teletext use, so
/// without a descriptor saying otherwise it is not audio.
pub(crate) fn declares_audio(block: &[u8]) -> bool {
    for (tag, body) in walk(block) {
        match tag {
            tag::AC3 | tag::ENHANCED_AC3 | tag::DTS | tag::AAC | tag::DTS_HD => return true,
            tag::REGISTRATION => {
                let head = &body[..body.len().min(4)];
                if AUDIO_FORMAT_IDS
                    .iter()
                    .any(|id| id.starts_with(head) && head.len() == 4)
                {
                    return true;
                }
            }
            _ => {}
        }
    }
    false
}

/// The codec a private-data stream's descriptors name, if any of them do.
pub(crate) fn private_stream_codec(block: &[u8]) -> Option<&'static str> {
    for (tag, body) in walk(block) {
        match tag {
            tag::AC3 => return Some("ac3"),
            tag::ENHANCED_AC3 => return Some("eac3"),
            tag::AAC => return Some("aac"),
            tag::DTS | tag::DTS_HD => return Some("dts"),
            tag::REGISTRATION if body.len() >= 4 => match &body[..4] {
                b"AC-3" => return Some("ac3"),
                b"EAC3" => return Some("eac3"),
                _ => {}
            },
            _ => {}
        }
    }
    None
}

/// The ISO 639-2 language a block names, or `und` when none does.
///
/// `und` is the code for "undetermined", so a stream that says nothing and a
/// stream that says it does not know are reported the same way - which is what
/// the code exists for.
pub(crate) fn language(block: &[u8]) -> String {
    for (tag, body) in walk(block) {
        if tag == tag::LANGUAGE
            && body.len() >= 3
            && let Ok(code) = core::str::from_utf8(&body[..3])
        {
            return code.to_owned();
        }
    }
    "und".to_owned()
}

/// What a descriptor block declares about an audio stream's channel layout.
///
/// Deliberately not a channel count. The descriptors carry a coarse class, and
/// above stereo they say "more than two" without naming a number - an AC-3
/// service at 5.1 and one at 7.1 declare the same value. Turning that into a
/// number is an inference, and it belongs to whoever knows what they will do
/// with the answer.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct ChannelDeclaration {
    /// The declared count where the declaration names one, otherwise 0.
    pub channels: u8,
    /// A declaration of more than two channels that carries no exact count.
    pub multichannel: bool,
    /// The raw component type byte this was read from, kept so a policy layer
    /// can reach conclusions this type deliberately does not.
    pub component_type: u8,
    /// Whether a component type was present at all, which is a different thing
    /// from one that happens to be zero.
    pub has_component_type: bool,
}

impl ChannelDeclaration {
    /// Whether the declaration says anything about the channel count.
    #[must_use]
    pub const fn known(&self) -> bool {
        self.channels > 0 || self.multichannel
    }
}

/// AC-3 `component_type`, ETSI EN 300 468 Annex D. The low nibble is the declared
/// number of channels; from `MULTI_FIRST` upward it says only "more than two".
mod ac3_channels {
    /// `component_type_flag`, the first bit of the descriptor body.
    pub(super) const COMPONENT_TYPE_FLAG: u8 = 0x80;
    /// The bits of the component type that name the channel count.
    pub(super) const MASK: u8 = 0x0F;
    /// A single channel.
    pub(super) const MONO: u8 = 0x00;
    /// Two independent mono programmes.
    pub(super) const DUAL_MONO: u8 = 0x01;
    /// Two channels.
    pub(super) const STEREO: u8 = 0x02;
    /// Two channels, Dolby surround encoded.
    pub(super) const STEREO_SURROUND: u8 = 0x03;
    /// The first value meaning multichannel with no count declared.
    pub(super) const MULTI_FIRST: u8 = 0x04;
    /// The last such value.
    pub(super) const MULTI_LAST: u8 = 0x06;
}

/// `AAC_type`, ETSI EN 300 468 Table 26. Only the values whose channel meaning
/// is unambiguous appear; the table also names audio description, hard of
/// hearing and mixed supplementary variants whose count the value does not fix.
mod aac_type {
    /// `AAC_type_flag`, the bit after `profile_and_level`.
    pub(super) const FLAG: u8 = 0x80;
    /// Mono.
    pub(super) const MONO: u8 = 0x01;
    /// Stereo.
    pub(super) const STEREO: u8 = 0x03;
    /// Surround.
    pub(super) const SURROUND: u8 = 0x05;
    /// HE-AAC mono.
    pub(super) const HE_MONO: u8 = 0x43;
    /// HE-AAC stereo.
    pub(super) const HE_STEREO: u8 = 0x45;
    /// HE-AAC surround.
    pub(super) const HE_SURROUND: u8 = 0x47;
}

/// Reads the declared channel information out of a descriptor block.
///
/// Only AC-3, E-AC-3 and AAC declare channels in a descriptor, and only when the
/// optional component type is present. MPEG-1/2 layer II carries its channel
/// mode in the audio frame header, which is elementary stream payload this file
/// never sees, so it declares nothing here. DTS likewise.
pub(crate) fn channel_declaration(block: &[u8]) -> ChannelDeclaration {
    for (tag, body) in walk(block) {
        let declared = match tag {
            tag::AC3 | tag::ENHANCED_AC3 => ac3_declaration(body),
            tag::AAC => aac_declaration(body),
            _ => None,
        };
        if let Some(declared) = declared {
            return declared;
        }
    }
    ChannelDeclaration::default()
}

/// Reads an AC-3 or E-AC-3 descriptor body. Its first byte is a set of presence
/// flags; the component type, when the flag says it is there, follows it.
fn ac3_declaration(body: &[u8]) -> Option<ChannelDeclaration> {
    if body.len() < 2 || body[0] & ac3_channels::COMPONENT_TYPE_FLAG == 0 {
        return None;
    }
    let component_type = body[1];
    let mut declared = ChannelDeclaration {
        component_type,
        has_component_type: true,
        ..ChannelDeclaration::default()
    };
    match component_type & ac3_channels::MASK {
        ac3_channels::MONO => declared.channels = 1,
        ac3_channels::DUAL_MONO | ac3_channels::STEREO | ac3_channels::STEREO_SURROUND => {
            declared.channels = 2;
        }
        ac3_channels::MULTI_FIRST..=ac3_channels::MULTI_LAST => declared.multichannel = true,
        // Reserved. The component type is still worth carrying, but it names no
        // channel count this code is willing to claim.
        _ => {}
    }
    Some(declared)
}

/// Reads an AAC descriptor body: `profile_and_level`, a flag bit, then the AAC
/// type when that flag is set.
fn aac_declaration(body: &[u8]) -> Option<ChannelDeclaration> {
    if body.len() < 3 || body[1] & aac_type::FLAG == 0 {
        return None;
    }
    let value = body[2];
    let mut declared = ChannelDeclaration {
        component_type: value,
        has_component_type: true,
        ..ChannelDeclaration::default()
    };
    match value {
        aac_type::MONO | aac_type::HE_MONO => declared.channels = 1,
        aac_type::STEREO | aac_type::HE_STEREO => declared.channels = 2,
        aac_type::SURROUND | aac_type::HE_SURROUND => declared.multichannel = true,
        _ => {}
    }
    Some(declared)
}
