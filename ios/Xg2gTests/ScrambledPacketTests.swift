// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// A scrambled payload is not elementary stream yet.
///
/// On an encrypted service the receiver needs a moment before the CA module has
/// the key, and the packets that arrive meanwhile are marked scrambled. Parsing
/// them as PES cannot work, and the failures it produces were being counted as
/// parser errors — 262 in one measured session against 3 continuity errors,
/// which is what said the transport itself was intact.
@Suite struct ScrambledPacketTests {

    /// One 188-byte transport packet carrying `payload`, optionally marked
    /// scrambled.
    private func packet(pid: UInt16, scrambled: Bool, payload: [UInt8]) -> Data {
        var p = [UInt8](repeating: 0xFF, count: 188)
        p[0] = 0x47
        p[1] = 0x40 | UInt8((pid >> 8) & 0x1F)        // payload_unit_start_indicator
        p[2] = UInt8(pid & 0xFF)
        // transport_scrambling_control in bits 7..6, adaptation_field_control = 01
        p[3] = (scrambled ? 0xC0 : 0x00) | 0x10
        for (i, b) in payload.enumerated() where 4 + i < 188 {
            p[4 + i] = b
        }
        return Data(p)
    }

    /// A video PES header, which is what the parser's auto-detection looks for.
    private var videoPESHeader: [UInt8] {
        [0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
         0x21, 0x00, 0x01, 0x00, 0x01]
    }

    @Test func scrambledPacketsOnAnUnknownPidAreSkippedButNotCounted() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        parser.feed(data: packet(pid: 0x0100, scrambled: true, payload: videoPESHeader))

        // Nothing may reach the PES assembler, and nothing may be inferred from
        // bytes that have not been decrypted.
        #expect(sink.payloads.isEmpty)
        #expect(parser.videoPID == nil)
        // Nor counted: until something names this PID as the programme's, there
        // is nothing to say the packet belongs to what is being watched. In a
        // real stream the PMT settles that within tens of milliseconds, and PSI
        // is never scrambled.
        #expect(parser.scrambledPackets == 0)
        #expect(sink.scrambledPIDs.isEmpty)
    }

    @Test func scrambledPacketsOnThePlayedPidAreCounted() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        parser.feed(data: packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        parser.feed(data: packet(pid: 0x0100, scrambled: true, payload: videoPESHeader))

        #expect(parser.videoPID == 0x0100)
        #expect(parser.scrambledPackets == 1)
        #expect(sink.scrambledPIDs == [0x0100])
        #expect(sink.payloads.count == 1)
    }

    /// The count has to mean "the programme is not coming through" and nothing
    /// else, or it cannot be read at all.
    @Test func scrambledPacketsOnOtherServicesAreSkippedButNotCounted() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        parser.feed(data: packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        var data = Data()
        for _ in 0..<50 {
            data.append(packet(pid: 0x0200, scrambled: true, payload: videoPESHeader))
        }
        parser.feed(data: data)

        // A receiver hands over the whole transport, and the services nobody
        // asked for stay encrypted for their own reasons. Counting those
        // reached 24512 on a measured tune whose picture and sound were
        // flawless.
        #expect(parser.scrambledPackets == 0)
        #expect(sink.scrambledPIDs.isEmpty)
        #expect(sink.payloads.count == 1)
    }

    @Test func clearPacketsStillFlowThrough() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        parser.feed(data: packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))

        #expect(parser.scrambledPackets == 0)
        #expect(parser.videoPID == 0x0100)
        #expect(sink.payloads.count == 1)
    }

    /// The realistic shape of a tune on an encrypted service: scrambled first,
    /// then clear. The scrambled run must not prevent the stream from starting
    /// once the key arrives.
    @Test func aScrambledRunDoesNotPoisonTheStreamThatFollows() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        // One clear packet first, standing in for the PMT that names the PID
        // on a real service before any of this arrives.
        var data = Data()
        data.append(packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        for _ in 0..<20 {
            data.append(packet(pid: 0x0100, scrambled: true, payload: videoPESHeader))
        }
        data.append(packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        parser.feed(data: data)

        #expect(parser.scrambledPackets == 20)
        #expect(parser.videoPID == 0x0100)
        #expect(sink.payloads.count == 2)
    }

    /// Continuity is only defined over packets that carry payload we accepted;
    /// counting a skipped packet would report a fault on every tune-in.
    @Test func skippedPacketsDoNotRegisterAsContinuityErrors() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        var data = Data()
        data.append(packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        for _ in 0..<5 {
            data.append(packet(pid: 0x0100, scrambled: true, payload: videoPESHeader))
        }
        data.append(packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        parser.feed(data: data)

        #expect(sink.continuityErrors == 0)
    }

    // MARK: - Hard Invariant & Edge Case Tests

    /// 1. scrambling_control = 10 / 11 + payload coincidentally starting with 00 00 01 MUST be discarded.
    @Test func scrambledPacketCoincidentallyStartingWithPESHeaderIsDiscarded() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        // Discover video PID via a clear packet first
        parser.feed(data: packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))
        #expect(parser.videoPID == 0x0100)
        #expect(sink.payloads.count == 1)

        // Even key scrambling (0b10 -> 0x80 in TS header byte 3) with PES prefix
        var pEven = [UInt8](repeating: 0x77, count: 188)
        pEven[0] = 0x47
        pEven[1] = 0x40 | 0x01 // PUSI = 1, PID = 0x0100
        pEven[2] = 0x00
        pEven[3] = 0x80 | 0x10 // scramblingControl = 0b10, adaptation = 01
        pEven[4] = 0x00 // Coincidental PES start code in ciphertext
        pEven[5] = 0x00
        pEven[6] = 0x01
        pEven[7] = 0xE0
        parser.feed(data: Data(pEven))

        // Odd key scrambling (0b11 -> 0xC0 in TS header byte 3) with PES prefix
        var pOdd = [UInt8](repeating: 0x88, count: 188)
        pOdd[0] = 0x47
        pOdd[1] = 0x40 | 0x01 // PUSI = 1, PID = 0x0100
        pOdd[2] = 0x00
        pOdd[3] = 0xC0 | 0x10 // scramblingControl = 0b11, adaptation = 01
        pOdd[4] = 0x00
        pOdd[5] = 0x00
        pOdd[6] = 0x01
        pOdd[7] = 0xE0
        parser.feed(data: Data(pOdd))

        #expect(sink.payloads.count == 1) // Only the initial clear packet was emitted
        #expect(parser.scrambledPackets == 2)
        #expect(sink.scrambledPIDs == [0x0100, 0x0100])
    }

    /// 2. Scrambled PUSI packet followed by continuation packets on the same PID
    /// must NEVER mark the PID as plaintext or emit payloads.
    @Test func scrambledPUSIFollowedByContinuationsNeverEmitsOrMarksPIDAsPlaintext() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        // Announce video PID
        parser.feed(data: packet(pid: 0x0100, scrambled: false, payload: videoPESHeader))

        // 1 Scrambled PUSI packet with fake PES header
        var pusi = [UInt8](repeating: 0xAA, count: 188)
        pusi[0] = 0x47
        pusi[1] = 0x41 // PUSI = 1, PID = 0x0100
        pusi[2] = 0x00
        pusi[3] = 0xC0 | 0x10 // Scrambled 0b11
        pusi[4] = 0x00
        pusi[5] = 0x00
        pusi[6] = 0x01
        pusi[7] = 0xE0
        parser.feed(data: Data(pusi))

        // 5 Scrambled continuation packets (PUSI = 0)
        for i in 0..<5 {
            var cont = [UInt8](repeating: UInt8(0x10 + i), count: 188)
            cont[0] = 0x47
            cont[1] = 0x01 // PUSI = 0, PID = 0x0100
            cont[2] = 0x00
            cont[3] = 0xC0 | 0x10 // Scrambled 0b11
            parser.feed(data: Data(cont))
        }

        #expect(sink.payloads.count == 1) // Only initial clear packet
        #expect(parser.scrambledPackets == 6)
    }

    /// 3. Scrambled run followed by real clear packets with scrambling_control = 00
    /// verifies that processing resumes cleanly without packet loss.
    @Test func scrambledRunFollowedByClearStreamResumesProcessingNormally() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        // Clear header to establish PID
        parser.feed(data: packet(pid: 0x0200, scrambled: false, payload: videoPESHeader))

        // 10 Scrambled packets (simulating tuning delay before CW arrives)
        for _ in 0..<10 {
            parser.feed(data: packet(pid: 0x0200, scrambled: true, payload: [0xDE, 0xAD, 0xBE, 0xEF]))
        }
        #expect(parser.scrambledPackets == 10)
        #expect(sink.payloads.count == 1)

        // Stream becomes clear (CW decrypted stream)
        for _ in 0..<5 {
            parser.feed(data: packet(pid: 0x0200, scrambled: false, payload: videoPESHeader))
        }

        #expect(parser.scrambledPackets == 10)
        #expect(sink.payloads.count == 6)
    }

    /// 4. Scrambled PSI/PAT/PMT-like bytes must not alter parser state.
    @Test func scrambledPSIPacketsNeverMutatePATOrPMTTables() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        // Scrambled PAT packet (PID = 0, scramblingControl = 0b11)
        var scrambledPAT = [UInt8](repeating: 0xFF, count: 188)
        scrambledPAT[0] = 0x47
        scrambledPAT[1] = 0x40 // PUSI = 1, PID = 0
        scrambledPAT[2] = 0x00
        scrambledPAT[3] = 0xC0 | 0x10 // Scrambled
        scrambledPAT[4] = 0x00 // pointer_field = 0
        scrambledPAT[5] = 0x00 // table_id = 0x00 (PAT)
        scrambledPAT[6] = 0xB0
        scrambledPAT[7] = 0x0D
        scrambledPAT[8] = 0x00
        scrambledPAT[9] = 0x01 // transport_stream_id
        scrambledPAT[10] = 0xC1
        scrambledPAT[11] = 0x00
        scrambledPAT[12] = 0x00
        scrambledPAT[13] = 0x00
        scrambledPAT[14] = 0x01 // program_number = 1
        scrambledPAT[15] = 0xE1
        scrambledPAT[16] = 0x00 // PMT PID = 0x0100
        parser.feed(data: Data(scrambledPAT))

        // Now send clear PMT packet on PID 0x0100 - because scrambled PAT was rejected,
        // PID 0x0100 is NOT known as PMT PID and should not be parsed as PMT.
        var pmtPacket = [UInt8](repeating: 0xFF, count: 188)
        pmtPacket[0] = 0x47
        pmtPacket[1] = 0x41 // PUSI = 1, PID = 0x0100
        pmtPacket[2] = 0x00
        pmtPacket[3] = 0x10 // Clear
        pmtPacket[4] = 0x00
        pmtPacket[5] = 0x02 // table_id = PMT
        pmtPacket[6] = 0xB0
        pmtPacket[7] = 0x12
        pmtPacket[8] = 0x00
        pmtPacket[9] = 0x01
        pmtPacket[10] = 0xC1
        pmtPacket[11] = 0x00
        pmtPacket[12] = 0x00
        pmtPacket[13] = 0xE2
        pmtPacket[14] = 0x00 // PCR PID
        pmtPacket[15] = 0xF0
        pmtPacket[16] = 0x00
        pmtPacket[17] = 0x1B // H.264 video stream
        pmtPacket[18] = 0xE2
        pmtPacket[19] = 0x00 // elementary PID = 0x0200
        pmtPacket[20] = 0xF0
        pmtPacket[21] = 0x00
        parser.feed(data: Data(pmtPacket))

        // videoPID should still be nil because the scrambled PAT was ignored
        #expect(parser.videoPID == nil)
        #expect(parser.audioTracks.isEmpty)
    }

    /// 5. Real broadcast FTA capture (PULS 24 HD) parses cleanly with zero scrambled packet reports.
    @Test func realBroadcastFTACapturePULS24ParsesWithoutScrambledFlags() {
        let parser = TSPacketParser()
        let sink = ScrambledProbe()
        parser.delegate = sink

        let fixtureData = PULS24CaptureFixture.data
        parser.feed(data: fixtureData)

        #expect(parser.scrambledPackets == 0)
        #expect(sink.scrambledPIDs.isEmpty)
        #expect(parser.videoPID == 1279)
        #expect(parser.audioPIDs.contains(1283))
        #expect(!sink.payloads.isEmpty)
    }
}

private final class ScrambledProbe: TSPacketParserDelegate, @unchecked Sendable {
    var payloads: [Data] = []
    var scrambledPIDs: [UInt16] = []
    var continuityErrors = 0

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        payloads.append(data)
    }

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {
        continuityErrors += 1
    }

    func tsParser(_ parser: TSPacketParser, didEncounterScrambledPacketOnPID pid: UInt16) {
        scrambledPIDs.append(pid)
    }
}
