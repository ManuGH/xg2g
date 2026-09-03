// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Which programme of a transport this core follows.

use super::table::SectionHeader;

/// A programme entry of a PAT: four bytes, a number and the PID its table is on.
const ENTRY_BYTES: usize = 4;

/// The programme number reserved for the network information table. It names a
/// table, not a service, and following it would tune to neither.
const NIT_PROGRAM_NUMBER: u16 = 0;

/// The programme a PAT selects, and the PID its table is carried on.
///
/// The two are one decision and are never apart: a PAT entry names them
/// together, and a PMT on that PID has to agree with that number before it can
/// describe this programme. Holding only the PID would make any table on it
/// this programme's, which is exactly what a shared PID makes untrue.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct Selection {
    /// The programme number chosen.
    pub(crate) program_number: u16,
    /// The PID its table is carried on.
    pub(crate) pmt_pid: u16,
}

/// Chooses a programme out of a complete PAT.
///
/// `target` names the programme to follow, or is 0 to follow whichever the table
/// offers first. "First" is the table's own order - sections by `section_number`
/// ascending, and within a section the order the entries are listed - and the
/// sections arrive here already in that order however they were transmitted.
///
/// The first match ends the search whether or not a target was named. A later
/// section does not get to re-choose: an unnamed target means "whichever comes
/// first", and something that came later is not first.
pub(crate) fn select(sections: &[&[u8]], target: u16) -> Option<Selection> {
    for section in sections {
        if section.len() < super::table::MIN_SECTION_LEN {
            continue;
        }
        for entry in SectionHeader::body(section).chunks_exact(ENTRY_BYTES) {
            let program_number = (u16::from(entry[0]) << 8) | u16::from(entry[1]);
            let pmt_pid = (u16::from(entry[2] & 0x1F) << 8) | u16::from(entry[3]);
            if program_number == NIT_PROGRAM_NUMBER {
                continue;
            }
            if target > 0 && program_number != target {
                continue;
            }
            return Some(Selection {
                program_number,
                pmt_pid,
            });
        }
    }
    None
}
