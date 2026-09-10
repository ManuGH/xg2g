// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Getting whole PSI sections back out of 188-byte packets.
//!
//! One of these follows one PSI PID. It owns the bytes of a section that has not
//! finished arriving, and nothing about how those bytes were carried: the
//! continuity counter belongs to the transport layer, which keeps it for this
//! PID on the assembler's behalf so a later stage following its own PIDs gets
//! the same answer from the same code.
//!
//! The packets are the transport's segmentation of a section. Once their bytes
//! are in the buffer they have said everything they had to say, and keeping
//! them would make this parser's memory a function of how finely a sender chose
//! to fragment: the same 1024-byte table can be sent in six packets or in a
//! thousand, and both mean the same table.

/// A section that has arrived in full.
use crate::transport::{Continuity, ContinuityTracker};

pub(crate) struct Completed {
    /// The section, from its `table_id` through its CRC.
    pub(crate) bytes: Vec<u8>,
}

/// Section assembly for one PSI PID.
#[derive(Debug, Default)]
pub(crate) struct SectionAssembler {
    /// Bytes of the section still arriving.
    buf: Vec<u8>,
    /// Its total length once the header has been read, otherwise 0.
    section_len: usize,
    /// The continuity counter of this PID.
    ///
    /// The counter is transport, not PSI, so it is kept by the transport layer's
    /// tracker rather than reimplemented here. A later stage following its own
    /// PIDs gets the same answer from the same code instead of a second one that
    /// happens to agree today.
    continuity: ContinuityTracker,
}

impl SectionAssembler {
    /// Every byte this assembler is holding, and the section length it is
    /// waiting for.
    ///
    /// Exposed for the tests that have to see the memory rather than the facts.
    /// Neither defect this pins ever moved a fact: an impossible declaration
    /// wedged bytes here that nothing could emit, and retaining carrier packets
    /// let a sender decide how much was held. A test comparing facts would have
    /// watched both happen and reported nothing.
    #[cfg(test)]
    pub(super) fn retained(&self) -> (usize, usize) {
        (self.buf.len(), self.section_len)
    }

    /// Every byte this assembler holds, the duplicate-detection packet included.
    ///
    /// Kept apart from [`Self::retained`] because the two answer different
    /// questions: that one asks whether a section was left in flight and must be
    /// able to read zero, this one accounts for memory and never can - one
    /// packet is always held once a packet has been seen, and that is the bound.
    #[cfg(test)]
    pub(super) fn retained_bytes(&self) -> usize {
        self.buf.len() + self.continuity.retained_bytes()
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
        self.continuity.reset();
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
        continuity_counter: u8,
        pusi: bool,
        payload: &[u8],
        expected_table_id: u8,
    ) -> Vec<Completed> {
        let mut completed = Vec::new();

        // The tracker has already recorded this packet by the time it answers,
        // except for a duplicate, which leaves it untouched. That is why the
        // break arm discards only the section: forgetting the counter as well
        // would make the very next packet look like the start of a new stream,
        // and the old code immediately undid exactly that by re-recording the
        // counter it had just cleared.
        match self.continuity.observe(packet, continuity_counter) {
            // The transport saying the same thing twice.
            Continuity::Duplicate => return completed,
            // Either a packet went missing, or one counter value described two
            // different packets. Whatever was in flight cannot be trusted.
            Continuity::Broken => {
                self.discard_section();
                if !pusi {
                    return completed;
                }
            }
            Continuity::First | Continuity::Continuous => {}
        }

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
                    self.feed(expected_table_id, &payload[1..end], &mut completed);
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
            at = self.feed(expected_table_id, payload, &mut completed);
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
                });
                at += full;
                continue;
            }
            self.buf.extend_from_slice(&payload[at..]);
            self.section_len = full;
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
    /// every later packet on the PID then added to them.
    fn feed(
        &mut self,
        expected_table_id: u8,
        chunk: &[u8],
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
            consumed += take;
            if self.buf.len() >= self.section_len {
                completed.push(Completed {
                    bytes: self.buf[..self.section_len].to_vec(),
                });
                self.discard_section();
            }
        }

        consumed
    }
}
