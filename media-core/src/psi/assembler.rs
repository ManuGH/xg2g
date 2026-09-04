// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Getting whole PSI sections back out of 188-byte packets.
//!
//! One of these follows one PSI PID. It owns the continuity counter for that
//! PID, the bytes of a section that has not finished arriving, and the packets
//! those bytes came from - the last because a subscriber is handed the original
//! packets rather than a re-encoded table, so the transport it joins is the one
//! that was broadcast.

/// A section that has arrived in full, with the packets it came from.
pub(crate) struct Completed {
    /// The section, from its `table_id` through its CRC.
    pub(crate) bytes: Vec<u8>,
    /// Every packet that carried part of it, in arrival order.
    pub(crate) packets: Vec<Vec<u8>>,
}

/// Section assembly for one PSI PID.
#[derive(Default)]
pub(crate) struct SectionAssembler {
    /// Bytes of the section still arriving.
    buf: Vec<u8>,
    /// Its total length once the header has been read, otherwise 0.
    section_len: usize,
    /// The continuity counter of the last packet accepted on this PID.
    last_cc: u8,
    /// Whether `last_cc` means anything yet.
    has_cc: bool,
    /// The last packet accepted, kept whole so an exact repeat can be told from
    /// a different packet that reuses the counter.
    last_packet: Vec<u8>,
    /// The packets the bytes in `buf` came from.
    raw_packets: Vec<Vec<u8>>,
}

impl SectionAssembler {
    /// What this assembler is holding: buffered bytes, the section length it is
    /// waiting for, and packets kept for a section not yet emitted.
    ///
    /// Exposed for the one test that has to see that an impossible declaration
    /// left nothing behind. That defect never moved a fact, so a test comparing
    /// facts would have watched it happen and reported nothing.
    #[cfg(test)]
    pub(super) fn retained(&self) -> (usize, usize, usize) {
        (self.buf.len(), self.section_len, self.raw_packets.len())
    }

    /// Forgets everything, the continuity counter included.
    ///
    /// Used where the stream itself has become untrustworthy: after a
    /// discontinuity, or when the PID stops being one this core follows. The
    /// next packet is then treated as the first, which is what stops a stale
    /// counter from rejecting a stream that has legitimately restarted.
    pub(crate) fn reset(&mut self) {
        self.buf.clear();
        self.section_len = 0;
        self.has_cc = false;
        self.last_packet.clear();
        self.raw_packets.clear();
    }

    /// Drops the section in flight but keeps following the PID.
    ///
    /// Separate from [`Self::reset`] on purpose: a pointer field saying "a new
    /// section starts here" makes whatever was half-collected unusable, but says
    /// nothing about the continuity counter, and forgetting that too would make
    /// the next packet look like the start of a fresh stream.
    fn discard_section(&mut self) {
        self.buf.clear();
        self.section_len = 0;
        self.raw_packets.clear();
    }

    /// Takes one packet's payload and returns the sections it completed, in the
    /// order they finished.
    ///
    /// `expected_table_id` ends the scan of a payload rather than filtering it:
    /// a section of another table means the rest of this payload is not laid out
    /// the way this PID's tables are, and its length field is not something to
    /// navigate by. Whether a completed section is *accepted* is decided
    /// elsewhere, on the finished section, so that one arriving in pieces cannot
    /// reach interpretation without the same check.
    pub(crate) fn accept(
        &mut self,
        packet: &[u8],
        pusi: bool,
        payload: &[u8],
        expected_table_id: u8,
    ) -> Vec<Completed> {
        let mut completed = Vec::new();
        let cc = packet[3] & 0x0F;

        if self.has_cc {
            if cc == self.last_cc {
                if packet == self.last_packet.as_slice() {
                    // The transport saying the same thing twice, which is what a
                    // repeated counter with identical bytes means.
                    return completed;
                }
                // One counter value cannot describe two different packets, so
                // whatever was in flight cannot be trusted.
                self.reset();
                if !pusi {
                    return completed;
                }
            } else if cc != (self.last_cc.wrapping_add(1)) & 0x0F {
                // A packet went missing, and it may have carried section bytes.
                self.reset();
                if !pusi {
                    return completed;
                }
            }
        }
        self.last_cc = cc;
        self.has_cc = true;
        self.last_packet.clear();
        self.last_packet.extend_from_slice(packet);

        let mut at;
        if pusi {
            let Some(&pointer) = payload.first() else {
                return completed;
            };
            let pointer = usize::from(pointer);
            if pointer > 0 {
                if !self.buf.is_empty() {
                    let Some(end) = 1usize.checked_add(pointer).filter(|e| *e <= payload.len())
                    else {
                        self.reset();
                        return completed;
                    };
                    self.feed(expected_table_id, &payload[1..end], packet, &mut completed);
                    if !self.buf.is_empty() {
                        // The bytes before the pointer were supposed to finish
                        // the section in flight and did not, so it never will.
                        self.discard_section();
                    }
                }
                at = 1 + pointer;
            } else {
                self.discard_section();
                at = 1;
            }
        } else if self.buf.is_empty() {
            // Continuation bytes with nothing to continue.
            return completed;
        } else {
            at = self.feed(expected_table_id, payload, packet, &mut completed);
        }

        while at < payload.len() {
            if payload[at] == 0xFF {
                // PSI stuffing: the rest of the payload is padding.
                break;
            }
            let available = payload.len() - at;
            if available < 3 {
                // The section header itself is split across the packet boundary.
                // Its length is in the bytes that have not arrived, so there is
                // nothing to do but keep these and wait.
                self.buf.extend_from_slice(&payload[at..]);
                self.section_len = 0;
                self.raw_packets.push(packet.to_vec());
                break;
            }
            if payload[at] != expected_table_id {
                break;
            }
            let Some(full) = super::table::declaration(expected_table_id, &payload[at..]) else {
                // An impossible declaration ends the scan of this payload. What
                // follows it cannot be located: the length that would say where
                // is the one that has just been refused.
                break;
            };
            if available >= full {
                completed.push(Completed {
                    bytes: payload[at..at + full].to_vec(),
                    packets: vec![packet.to_vec()],
                });
                at += full;
                continue;
            }
            self.buf.extend_from_slice(&payload[at..]);
            self.section_len = full;
            self.raw_packets.push(packet.to_vec());
            break;
        }

        completed
    }

    /// Adds `chunk` to the section in flight, completing it if it fits, and
    /// returns how many bytes were taken - or, when the header completes into a
    /// declaration the expected table cannot make, the whole input.
    ///
    /// What the assembler holds is given up entirely in that case. The defect
    /// this closes was not a wrong fact: a section declaring nothing after its
    /// length field left three bytes here that neither phase could act on, and
    /// every later packet on the PID was then retained.
    fn feed(
        &mut self,
        expected_table_id: u8,
        chunk: &[u8],
        packet: &[u8],
        completed: &mut Vec<Completed>,
    ) -> usize {
        if chunk.is_empty() {
            return 0;
        }
        let mut chunk = chunk;
        let mut consumed = 0usize;

        // The three header bytes first: until they are all here, the section's
        // length is unknown and there is nothing to fill.
        if self.buf.len() < 3 {
            let take = chunk.len().min(3 - self.buf.len());
            self.buf.extend_from_slice(&chunk[..take]);
            self.raw_packets.push(packet.to_vec());
            chunk = &chunk[take..];
            consumed += take;
            if self.buf.len() < 3 {
                return consumed;
            }
            let Some(full) = super::table::declaration(expected_table_id, &self.buf) else {
                // The header is complete and says something a PAT or PMT cannot
                // say. It is the only thing that could tell this assembler how
                // much to collect, so there is nothing to wait for: the section
                // is dropped rather than reserving space on its word.
                self.discard_section();
                // Everything handed in is given up, not only the header bytes.
                // The caller resumes its scan at what this returns, and there is
                // nowhere in the rest of this payload it could resume: the
                // length that would say where the next section starts is the one
                // just refused. Returning the header bytes alone would put the
                // scan a byte or two into the body of the section that was
                // refused, and let those bytes be read as a table_id and a
                // length of their own.
                return consumed + chunk.len();
            };
            self.section_len = full;
        }

        if self.section_len > 0 && self.buf.len() < self.section_len {
            let take = chunk.len().min(self.section_len - self.buf.len());
            self.buf.extend_from_slice(&chunk[..take]);
            if consumed == 0 {
                // Not already recorded by the header phase for this packet.
                self.raw_packets.push(packet.to_vec());
            }
            consumed += take;
            if self.buf.len() >= self.section_len {
                completed.push(Completed {
                    bytes: self.buf[..self.section_len].to_vec(),
                    packets: self.raw_packets.clone(),
                });
                self.discard_section();
            }
        }

        consumed
    }
}
