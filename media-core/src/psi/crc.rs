// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The CRC a PSI section carries about itself.

/// The ISO/IEC 13818-1 Annex B polynomial.
const POLYNOMIAL: u32 = 0x04C1_1DB7;

/// Computes the MPEG-2 systems CRC over `data`.
///
/// Written bit by bit rather than with a lookup table. The table form is faster
/// and the sections this runs over are a few hundred bytes at most, so the
/// difference is not worth the thing the table costs: a second place where the
/// polynomial is encoded, and one that cannot be read against the standard
/// without expanding it back out again.
///
/// A section carries its own CRC in the last four bytes, so running this over
/// the whole section - the CRC included - yields zero when the bytes are
/// intact. That is how the caller uses it, and it is why nothing here strips or
/// appends anything.
pub(crate) fn mpeg2(data: &[u8]) -> u32 {
    let mut crc: u32 = 0xFFFF_FFFF;
    for &byte in data {
        for bit in (0..8).rev() {
            let incoming = u32::from((byte >> bit) & 1);
            let outgoing = crc >> 31;
            crc <<= 1;
            if outgoing != incoming {
                crc ^= POLYNOMIAL;
            }
        }
    }
    crc
}

#[cfg(test)]
mod tests {
    use super::mpeg2;

    /// Appends the CRC of `body`, which is what a well-formed section does.
    fn sealed(body: &[u8]) -> Vec<u8> {
        let mut out = body.to_vec();
        out.extend_from_slice(&mpeg2(body).to_be_bytes());
        out
    }

    #[test]
    fn a_sealed_section_checks_to_zero() {
        for body in [
            &b""[..],
            &b"\x00"[..],
            &b"\x02\xb0\x17\x00\x01\xc1\x00\x00"[..],
            &[0xFF; 183][..],
        ] {
            assert_eq!(mpeg2(&sealed(body)), 0, "body {body:02x?}");
        }
    }

    #[test]
    fn a_single_flipped_bit_anywhere_is_caught() {
        let body = b"\x02\xb0\x17\x00\x01\xc1\x00\x00\xe1\x01\xf0\x00";
        let section = sealed(body);
        for byte in 0..section.len() {
            for bit in 0..8u8 {
                let mut broken = section.clone();
                broken[byte] ^= 1 << bit;
                assert_ne!(
                    mpeg2(&broken),
                    0,
                    "flipping byte {byte} bit {bit} went unnoticed"
                );
            }
        }
    }

    #[test]
    fn a_truncated_section_does_not_check_out() {
        let section = sealed(b"\x00\xb0\x0d\x00\x01\xc1\x00\x00\x00\x01\xe1\x00");
        for cut in 1..section.len() {
            assert_ne!(
                mpeg2(&section[..cut]),
                0,
                "a section cut to {cut} bytes checked out"
            );
        }
    }

    /// The empty input is the initial register, which is what makes the
    /// four-byte all-ones section the only one that trivially checks out.
    #[test]
    fn the_empty_input_is_the_initial_register() {
        assert_eq!(mpeg2(&[]), 0xFFFF_FFFF);
    }
}
