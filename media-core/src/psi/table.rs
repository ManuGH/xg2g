// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What a completed section has to be before anything is read out of it, and
//! how the sections of one table are collected into a generation.

use super::crc;
use std::collections::BTreeMap;

/// The shortest section that can hold every field this parser reads: the
/// eight-byte header, four bytes of CRC, and nothing in between.
pub(crate) const MIN_SECTION_LEN: usize = 12;

/// The most a PAT or PMT section may declare after its length field.
///
/// `section_length` is a twelve bit field, but ISO/IEC 13818-1 does not let
/// either of these tables use all of it: for both, the first two bits of the
/// field shall be `00` and the value shall not exceed 1021. The field width is
/// not the bound, and treating it as one would have this parser collect four
/// kilobytes for a section that cannot legally be longer than one - on nothing
/// better than the say-so of a stream that has already declared something
/// impossible.
pub(crate) const MAX_SECTION_LENGTH: usize = 1021;

/// The most a PAT or PMT section can be in total.
pub(crate) const MAX_SECTION_BYTES: usize = MAX_SECTION_LENGTH + 3;

/// Reads the three bytes that open a section and reports the total length they
/// declare.
///
/// `None` for a declaration a PAT or PMT cannot make. Three things make one
/// impossible, and they are asked together because they are read from the same
/// two bytes and all knowable the moment those bytes arrive:
///
/// - `section_syntax_indicator` must be 1, since both tables use the long form
/// - the bit after it must be 0, fixed by the syntax rather than reserved
/// - `section_length` must not exceed 1021
///
/// One helper for one question, asked everywhere it comes up: by the scan that
/// meets a header whole inside a payload, by the assembler deciding how many
/// bytes to collect, and by the completed-section check. A second definition of
/// "how long may this be" is how two paths come to disagree about it.
pub(crate) fn declaration(prefix: &[u8]) -> Option<usize> {
    if prefix.len() < 3 {
        return None;
    }
    if prefix[1] & 0x80 == 0 || prefix[1] & 0x40 != 0 {
        return None;
    }
    let length = (usize::from(prefix[1] & 0x0F) << 8) | usize::from(prefix[2]);
    if length > MAX_SECTION_LENGTH {
        return None;
    }
    let total = length + 3;
    debug_assert!(total <= MAX_SECTION_BYTES);
    Some(total)
}

/// The fields of a section header this parser reads.
pub(crate) struct SectionHeader {
    /// The `transport_stream_id` of a PAT, or the `program_number` of a PMT.
    pub(crate) id_extension: u16,
    /// The version of the table this section is part of.
    pub(crate) version: u8,
    /// Where this section sits in the table.
    pub(crate) section_number: u8,
    /// The last section number the table has.
    pub(crate) last_section_number: u8,
}

impl SectionHeader {
    /// Reads a section that has arrived in full, refusing the ones that cannot
    /// be interpreted at this point in the stream.
    ///
    /// Four things are asked, in this order:
    ///
    /// - it is long enough to hold the fields that will be read,
    /// - it is the table this PID is being read for,
    /// - it declares a length and a syntax those tables may declare,
    /// - its bytes are intact,
    /// - it is in force rather than announced for later,
    /// - it numbers itself inside the table it names.
    ///
    /// The table check is here, on the completed section, and not only where a
    /// header happens to be scanned inside one payload: a section whose first
    /// bytes land at the end of a packet is assembled without that scan ever
    /// seeing its `table_id`, and a CRC does not discriminate between tables. It
    /// only says the bytes arrived as they were sent.
    pub(crate) fn read(section: &[u8], expected_table_id: u8) -> Option<Self> {
        if section.len() < MIN_SECTION_LEN {
            return None;
        }
        if section[0] != expected_table_id {
            return None;
        }
        // Defence in depth. Both paths that reach here refused an impossible
        // declaration before acting on it, and asking again costs nothing
        // against a section that arrived some way nobody has thought of yet.
        declaration(section)?;
        if crc::mpeg2(section) != 0 {
            return None;
        }
        // current_next_indicator. A table announced for later describes a
        // programme that is not on air, and acting on it would describe the
        // stream as it is going to be rather than as it is.
        if section[5] & 0x01 != 1 {
            return None;
        }
        let section_number = section[6];
        let last_section_number = section[7];
        // A section numbers itself within its own table: 13818-1 has
        // section_number count from zero and last_section_number name the
        // highest, so a section claiming to be the sixth of two is not a member
        // of the table it names. Refused here rather than collected and found
        // wanting later - the tracker keys sections by number, so an impossible
        // one is an entry the generation can never account for, and a table
        // arriving correctly would stop completing because of a section that
        // was never part of it.
        if section_number > last_section_number {
            return None;
        }
        Some(Self {
            id_extension: (u16::from(section[3]) << 8) | u16::from(section[4]),
            version: (section[5] >> 1) & 0x1F,
            section_number,
            last_section_number,
        })
    }

    /// The bytes between the header and the CRC, which is where a table's own
    /// content lives.
    pub(crate) fn body(section: &[u8]) -> &[u8] {
        // Every caller has been through `read`, so the length is at least
        // MIN_SECTION_LEN and both ends are inside the slice.
        &section[8..section.len() - 4]
    }
}

/// The sections of one table, collected until every one of them is here.
///
/// A multi-section table is not a table until it is complete. Reading a partial
/// one would describe a programme by whichever half arrived first, and the half
/// that describes the video is not more authoritative than the half that
/// describes the audio.
#[derive(Default)]
pub(crate) struct TableTracker {
    /// The generation being collected, if any.
    in_flight: Option<Generation>,
}

/// One version of one table, part-collected.
struct Generation {
    /// The version every section of this generation carries.
    version: u8,
    /// The last section number every section of it declares.
    last_section_number: u8,
    /// Sections by section number.
    sections: BTreeMap<u8, Vec<u8>>,
    /// The packets each section arrived in.
    packets: BTreeMap<u8, Vec<Vec<u8>>>,
}

impl TableTracker {
    /// Forgets whatever was being collected.
    pub(crate) fn reset(&mut self) {
        self.in_flight = None;
    }

    /// Adds one section and reports whether the table is now complete.
    ///
    /// A section whose version or `last_section_number` differs from the one being
    /// collected starts a new generation and discards the old one: the two are
    /// describing different tables, and mixing their sections would produce a
    /// programme that was never broadcast.
    pub(crate) fn add(
        &mut self,
        version: u8,
        section_number: u8,
        last_section_number: u8,
        section: &[u8],
        packets: &[Vec<u8>],
    ) -> bool {
        let restart = match &self.in_flight {
            Some(generation) => {
                generation.version != version
                    || generation.last_section_number != last_section_number
            }
            None => true,
        };
        if restart {
            self.in_flight = Some(Generation {
                version,
                last_section_number,
                sections: BTreeMap::new(),
                packets: BTreeMap::new(),
            });
        }

        let generation = self
            .in_flight
            .as_mut()
            .expect("a generation was just put in place");
        generation.sections.insert(section_number, section.to_vec());
        generation.packets.insert(section_number, packets.to_vec());

        // Two questions, and the count is asked first because it is the one a
        // section numbered outside the table fails: such a section is collected
        // like any other, so a table that was complete stops being complete
        // while a stray number is held. Only once the count matches is each slot
        // actually looked for.
        let wanted = usize::from(last_section_number) + 1;
        if generation.sections.len() != wanted {
            return false;
        }
        (0..=last_section_number).all(|n| generation.sections.contains_key(&n))
    }

    /// The sections of the completed table, in `section_number` order.
    ///
    /// Order is the table's, never arrival's: the sections of a table say where
    /// they belong, and a transport that sends them out of order is describing
    /// the same programme. Only the numbers the table declares are walked, so a
    /// section numbered outside it is never read even while it is held.
    pub(crate) fn sections(&self) -> Vec<&[u8]> {
        let mut out = Vec::new();
        if let Some(generation) = &self.in_flight {
            for number in 0..=generation.last_section_number {
                if let Some(section) = generation.sections.get(&number) {
                    out.push(section.as_slice());
                }
            }
        }
        out
    }

    /// Every packet of the completed table, in `section_number` order, each listed
    /// once however many sections it carried.
    pub(crate) fn packets(&self) -> Vec<Vec<u8>> {
        let mut out: Vec<Vec<u8>> = Vec::new();
        if let Some(generation) = &self.in_flight {
            for number in 0..=generation.last_section_number {
                let Some(packets) = generation.packets.get(&number) else {
                    continue;
                };
                for packet in packets {
                    if !out.contains(packet) {
                        out.push(packet.clone());
                    }
                }
            }
        }
        out
    }
}
