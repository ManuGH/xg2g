// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import "github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"

// patPID is the PID the PAT is always carried on, ISO/IEC 13818-1 Table 2-3.
const patPID = 0x0000

// psiSectionHeaderBytes is table_id and the two bytes that declare the section
// length: the least a run of bytes must have to be a section at all.
const psiSectionHeaderBytes = 3

// packetizePSISections wraps accepted PSI sections in transport packets.
//
// This is transport, not interpretation. The core has already read these
// sections and accepted them; what it hands back is their exact bytes, from
// table_id through CRC_32, and this function's whole job is to put those bytes
// into 188-byte packets on the right PID. It does not parse them, does not
// re-encode them and does not recompute a CRC - any of which would be a second
// PSI implementation with its own answer.
//
// The packetization is the ring's own and is deliberately not the sender's. The
// sender's choice of how finely to fragment a table is not something the core
// keeps, precisely so that a sender cannot make xg2g hold more memory by
// fragmenting harder. What comes out here is therefore canonical rather than
// reproduced: each section starts a packet, with pointer_field zero, and the
// continuity counter counts this PID's packets from zero.
//
// Nothing is lost by that. The preamble is delivered ahead of ring data, where
// the sender's counters would not have lined up with the ring's offset anyway,
// and a decoder reads the tables, not the packets that carried them.
//
// Each section costs at most six packets - 183 payload bytes in the first, 184
// in each continuation, against a section of at most 1024 - so a table of 256
// sections is at most 288,768 bytes and a PAT and PMT preamble at most 564 KiB.
// That ceiling is derived from the syntax rather than chosen, and it is what an
// adversary can reach, not what a broadcast costs.
//
// Sections it could not have been given are skipped rather than emitted: a
// length outside what a section may be means the state upstream is damaged, and
// a packet built from it would claim to carry a section that cannot be read.
func packetizePSISections(pid uint16, sections [][]byte) []byte {
	var out []byte
	cc := uint8(0)
	for _, section := range sections {
		if len(section) < psiSectionHeaderBytes || len(section) > mediafacts.MaxSectionBytes {
			continue
		}
		rest := section
		pusi := true
		for {
			pkt := make([]byte, TSPacketSize)
			pkt[0] = mediafacts.SyncByte
			pkt[1] = byte((pid >> 8) & 0x1F)
			if pusi {
				pkt[1] |= 0x40 // payload_unit_start_indicator
			}
			pkt[2] = byte(pid & 0xFF)
			// Not scrambled, payload only, then this PID's own counter.
			pkt[3] = 0x10 | (cc & 0x0F)
			cc = (cc + 1) & 0x0F

			body := pkt[4:]
			if pusi {
				// A section that starts a packet starts at the packet's first
				// payload byte, which is what a pointer_field of zero says.
				body[0] = 0x00
				body = body[1:]
				pusi = false
			}
			n := copy(body, rest)
			rest = rest[n:]
			for i := n; i < len(body); i++ {
				body[i] = 0xFF
			}

			out = append(out, pkt...)
			if len(rest) == 0 {
				break
			}
		}
	}
	return out
}
