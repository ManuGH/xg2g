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

/// The largest a section can be, from `section_length` being twelve bits: three
/// header bytes ahead of the field plus the 4093 it can name.
pub(crate) const MAX_SECTION_LEN: usize = 3 + 0x0FFF;

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
    /// - its bytes are intact,
    /// - it is in force rather than announced for later.
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
        if crc::mpeg2(section) != 0 {
            return None;
        }
        // current_next_indicator. A table announced for later describes a
        // programme that is not on air, and acting on it would describe the
        // stream as it is going to be rather than as it is.
        if section[5] & 0x01 != 1 {
            return None;
        }
        Some(Self {
            id_extension: (u16::from(section[3]) << 8) | u16::from(section[4]),
            version: (section[5] >> 1) & 0x1F,
            section_number: section[6],
            last_section_number: section[7],
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
