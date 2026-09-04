// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What a programme is made of, according to its own table.

use super::descriptors::{self, ChannelDeclaration};

/// Bytes of an elementary stream entry ahead of its descriptors.
const ENTRY_HEADER_BYTES: usize = 5;

/// Where `program_info_length` sits in a PMT section.
const PROGRAM_INFO_LENGTH_AT: usize = 10;

/// Where the elementary stream loop starts once `program_info` is skipped.
const ES_LOOP_BASE: usize = 12;

/// The PID reserved for stuffing. Nothing that carries an elementary stream ever
/// uses it.
const NULL_PID: u16 = 0x1FFF;

/// The video codec a programme's table declares.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum VideoCodec {
    /// No video stream has been named.
    #[default]
    Unknown,
    /// H.264 / AVC.
    H264,
    /// H.265 / HEVC.
    H265,
    /// MPEG-1 or MPEG-2 video.
    Mpeg2,
}

impl VideoCodec {
    /// The name this codec is written as.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Unknown => "unknown",
            Self::H264 => "h264",
            Self::H265 => "h265",
            Self::Mpeg2 => "mpeg2",
        }
    }
}

/// One audio elementary stream a programme's table declares.
///
/// Everything here is a declaration read from the table. Nothing in this crate's
/// PSI path listens to the audio itself, so nothing here is a measurement.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AudioTrack {
    /// The PID the stream is carried on.
    pub pid: u16,
    /// The stream type the table gave it.
    pub stream_type: u8,
    /// The codec named by that stream type, or by a descriptor when the type
    /// alone does not say.
    pub codec: String,
    /// The ISO 639-2 language, or `und`.
    pub language: String,
    /// What the descriptors declare about the channel layout.
    pub declared: ChannelDeclaration,
}

/// What one complete PMT says a programme is made of.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct Streams {
    /// The PID of the video stream, or 0 when the table names none.
    pub video_pid: u16,
    /// Its codec.
    pub video_codec: VideoCodec,
    /// The audio PIDs, in the order the table lists them.
    pub audio_pids: Vec<u16>,
    /// The audio tracks, in the same order.
    pub audio_tracks: Vec<AudioTrack>,
}

/// Whether a PID could name an elementary stream at all.
///
/// PID 0 is the PAT and 0x1FFF is the null packet. A table naming either is
/// malformed: no payload is ever routed to such a stream and nothing can watch
/// it, so a declaration on one describes something that cannot exist.
const fn can_carry_elementary_stream(pid: u16) -> bool {
    pid != 0 && pid != NULL_PID
}

/// Reads the elementary streams out of every section of a complete PMT.
pub(crate) fn interpret(sections: &[&[u8]]) -> Streams {
    let mut streams = Streams::default();
    for section in sections {
        if section.len() < super::table::MIN_SECTION_LEN {
            continue;
        }
        interpret_section(section, &mut streams);
    }
    streams
}

/// Walks one section's elementary stream loop.
fn interpret_section(section: &[u8], streams: &mut Streams) {
    let program_info_len = (usize::from(section[PROGRAM_INFO_LENGTH_AT] & 0x0F) << 8)
        | usize::from(section[PROGRAM_INFO_LENGTH_AT + 1]);
    let Some(start) = ES_LOOP_BASE.checked_add(program_info_len) else {
        return;
    };
    // The loop ends where the CRC begins. Everything read below is bounded by
    // this, which is why no descriptor can ever be the section's own CRC.
    let end = section.len() - 4;

    let mut at = start;
    while at + ENTRY_HEADER_BYTES <= end {
        let stream_type = section[at];
        let pid = (u16::from(section[at + 1] & 0x1F) << 8) | u16::from(section[at + 2]);
        let info_len = (usize::from(section[at + 3] & 0x0F) << 8) | usize::from(section[at + 4]);

        let Some(entry_end) = at
            .checked_add(ENTRY_HEADER_BYTES)
            .and_then(|s| s.checked_add(info_len))
        else {
            return;
        };
        if entry_end > end {
            // The entry declares more than the loop holds, so it is not an
            // entry. Its own length is the only thing naming where the next one
            // starts, so nothing after it can be located either: the walk stops
            // rather than reading past the bound or guessing a boundary.
            return;
        }
        let block = &section[at + ENTRY_HEADER_BYTES..entry_end];

        match stream_type {
            0x1B => set_video(streams, pid, VideoCodec::H264),
            0x24 | 0x27 => set_video(streams, pid, VideoCodec::H265),
            0x01 | 0x02 => set_video(streams, pid, VideoCodec::Mpeg2),
            _ => {
                // One decision for one stream: either this entry is an audio
                // elementary stream that could exist, or it is nothing. The PID
                // list and the track list are both built here so they cannot end
                // up describing different sets.
                if declares_audio(stream_type, block) && can_carry_elementary_stream(pid) {
                    add_audio(streams, pid, stream_type, block);
                }
            }
        }
        at = entry_end;
    }
}

/// Records the video stream, if none has been named yet.
///
/// The first video entry the table lists is the one followed. A programme with
/// two of them is followed on the first, rather than on whichever happened to be
/// read last.
fn set_video(streams: &mut Streams, pid: u16, codec: VideoCodec) {
    if streams.video_pid == 0 {
        streams.video_pid = pid;
        streams.video_codec = codec;
    }
}

/// Records an accepted audio stream in both lists, keeping table order and
/// listing each PID once.
fn add_audio(streams: &mut Streams, pid: u16, stream_type: u8, block: &[u8]) {
    if !streams.audio_pids.contains(&pid) {
        streams.audio_pids.push(pid);
    }
    let track = AudioTrack {
        pid,
        stream_type,
        codec: audio_codec(stream_type, block).to_owned(),
        language: descriptors::language(block),
        declared: descriptors::channel_declaration(block),
    };
    match streams.audio_tracks.iter_mut().find(|t| t.pid == pid) {
        Some(existing) => *existing = track,
        None => streams.audio_tracks.push(track),
    }
}

/// Whether a stream type carries audio.
///
/// The unambiguous types say so themselves. Type 0x06 is "PES carrying private
/// data", which European broadcasters use for AC-3, E-AC-3 and DTS - and which
/// subtitles and teletext also use, so it counts as audio only when a descriptor
/// says so.
fn declares_audio(stream_type: u8, block: &[u8]) -> bool {
    match stream_type {
        0x03 | 0x04 | 0x0F | 0x11 | 0x1C | 0x81 | 0x87 => true,
        0x06 => descriptors::declares_audio(block),
        _ => false,
    }
}

/// The codec a stream type names, falling back to its descriptors where the type
/// alone does not say.
///
/// ATSC A/52 registers 0x81 for AC-3 and 0x87 for Enhanced AC-3. They are
/// different codecs and are named differently.
fn audio_codec(stream_type: u8, block: &[u8]) -> &'static str {
    match stream_type {
        0x03 | 0x04 => "mp2",
        0x0F | 0x11 | 0x1C => "aac",
        0x81 => "ac3",
        0x87 => "eac3",
        0x06 => descriptors::private_stream_codec(block).unwrap_or("unknown"),
        _ => "unknown",
    }
}
