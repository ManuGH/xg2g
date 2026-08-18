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
