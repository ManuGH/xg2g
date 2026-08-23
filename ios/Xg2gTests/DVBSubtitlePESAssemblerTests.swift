// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct DVBSubtitlePESAssemblerTests {

    @Test("DVB Subtitle PES: Full single-packet assembly with PTS and 0xFF end marker")
    func dvbSubtitleSinglePacketAssemblyWithPTS() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData = Data([0x0F, 0x10, 0x00, 0x01, 0x00, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05]) // 11-byte segment
        let pesData = makeDVBSubtitlePES(pts90k: 90000 * 10, segments: segmentData, includeEndMarker: true)

        assembler.feed(payload: pesData, unitStart: true)

        #expect(sink.packets.count == 1)
        #expect(sink.errors.isEmpty)

        let packet = sink.packets[0]
        #expect(packet.pts90k == 900000)
        #expect(packet.pts?.seconds == 10.0)
        #expect(packet.subtitleStreamID == 0x00)
        #expect(packet.payload == segmentData)
    }

    @Test("DVB Subtitle PES: Reassembly across 3 fragmented TS chunks")
    func dvbSubtitleFragmentedAcrossMultipleTSPackets() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData = Data(repeating: 0x42, count: 250) // 250 bytes
        let fullPES = makeDVBSubtitlePES(pts90k: 180000, segments: segmentData, includeEndMarker: true)

        // Split into 3 chunks of 100, 100, remaining
        let chunk1 = fullPES.prefix(100)
        let chunk2 = fullPES.dropFirst(100).prefix(100)
        let chunk3 = fullPES.dropFirst(200)

        assembler.feed(payload: chunk1, unitStart: true)
        #expect(sink.packets.isEmpty, "Should not emit before full length is satisfied")

        assembler.feed(payload: chunk2, unitStart: false)
        #expect(sink.packets.isEmpty, "Should not emit before final chunk")

        assembler.feed(payload: chunk3, unitStart: false)
        #expect(sink.packets.count == 1, "Should emit immediately upon satisfying PES_packet_length")
        #expect(sink.errors.isEmpty)

        let packet = sink.packets[0]
        #expect(packet.pts90k == 180000)
        #expect(packet.pts?.seconds == 2.0)
        #expect(packet.payload == segmentData)
    }

    @Test("DVB Subtitle PES: Packet without PTS flag emits nil PTS")
    func dvbSubtitleWithoutPTS() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData = Data([0x0F, 0x80, 0x00, 0x01, 0x00, 0x00]) // EoDS
        let pesData = makeDVBSubtitlePES(pts90k: nil, segments: segmentData, includeEndMarker: true)

        assembler.feed(payload: pesData, unitStart: true)

        #expect(sink.packets.count == 1)
        #expect(sink.packets[0].pts90k == nil)
        #expect(sink.packets[0].pts == nil)
        #expect(sink.packets[0].payload == segmentData)
    }

    @Test("DVB Subtitle PES Validation: Rejects invalid start code prefix and wrong stream ID")
    func dvbSubtitleInvalidStartCodeAndStreamID() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        // 1. Invalid start code prefix (0x00 0x00 0x02)
        var invalidStartCode = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F]), includeEndMarker: true)
        invalidStartCode[2] = 0x02
        assembler.feed(payload: invalidStartCode, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.count == 1)

        sink.errors.removeAll()

        // 2. Invalid stream ID (0xE0 instead of 0xBD private_stream_1)
        var invalidStreamID = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F]), includeEndMarker: true)
        invalidStreamID[3] = 0xE0
        assembler.feed(payload: invalidStreamID, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.count == 1)
    }

    @Test("DVB Subtitle PES Validation: Rejects invalid data_identifier")
    func dvbSubtitleInvalidDataIdentifier() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        // data_identifier 0x10 (Teletext) instead of 0x20 (DVB Subtitling)
        var invalidDataID = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F]), includeEndMarker: true)
        // Locate data_identifier byte (after 9-byte PES header + 5-byte PTS) -> offset 14
        invalidDataID[14] = 0x10

        assembler.feed(payload: invalidDataID, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.count == 1)
        #expect(sink.errors[0].contains("data_identifier"))
    }

    @Test("DVB Subtitle PES Validation: Truncated header data is discarded")
    func dvbSubtitleTruncatedHeader() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        // Feed only 5 bytes (incomplete start code & length)
        let truncatedBytes = Data([0x00, 0x00, 0x01, 0xBD, 0x00])
        assembler.feed(payload: truncatedBytes, unitStart: true)
        assembler.flush()

        #expect(sink.packets.isEmpty)
    }

    @Test("DVB Subtitle PES: Continuity error drops in-flight buffer until next PUSI")
    func dvbSubtitleContinuityErrorRecovery() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData1 = Data(repeating: 0x11, count: 100)
        let pesData1 = makeDVBSubtitlePES(pts90k: 90000, segments: segmentData1, includeEndMarker: true)

        // Feed first chunk of packet 1
        assembler.feed(payload: pesData1.prefix(50), unitStart: true)
        #expect(sink.packets.isEmpty)

        // Continuity error occurs mid-stream
        assembler.handleContinuityError()
        #expect(sink.errors.count == 1)

        // Continuation chunk of packet 1 arrives -> must be ignored
        assembler.feed(payload: pesData1.dropFirst(50), unitStart: false)
        #expect(sink.packets.isEmpty, "Continuation after continuity error must be ignored")

        // New PUSI arrives with packet 2 -> must re-synchronize cleanly
        let segmentData2 = Data(repeating: 0x22, count: 50)
        let pesData2 = makeDVBSubtitlePES(pts90k: 180000, segments: segmentData2, includeEndMarker: true)
        assembler.feed(payload: pesData2, unitStart: true)

        #expect(sink.packets.count == 1, "Next PUSI after continuity loss must assemble cleanly")
        #expect(sink.packets[0].pts90k == 180000)
        #expect(sink.packets[0].payload == segmentData2)
    }

    @Test("DVB Subtitle PES: New PUSI arriving prematurely discards truncated previous buffer")
    func dvbSubtitlePrematurePUSIDiscardsIncompleteBuffer() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData1 = Data(repeating: 0xAA, count: 200)
        let pesData1 = makeDVBSubtitlePES(pts90k: 90000, segments: segmentData1, includeEndMarker: true)

        // Feed only partial chunk of packet 1 (expecting 200+ bytes)
        assembler.feed(payload: pesData1.prefix(60), unitStart: true)
        #expect(sink.packets.isEmpty)

        // Packet 2 arrives with unitStart == true before packet 1 finished
        let segmentData2 = Data(repeating: 0xBB, count: 40)
        let pesData2 = makeDVBSubtitlePES(pts90k: 270000, segments: segmentData2, includeEndMarker: true)
        assembler.feed(payload: pesData2, unitStart: true)

        #expect(sink.packets.count == 1, "Only packet 2 should emit")
        #expect(sink.packets[0].pts90k == 270000)
        #expect(sink.packets[0].payload == segmentData2)
        #expect(sink.errors.contains(where: { $0.contains("Incomplete PES packet truncated") }))
    }

    @Test("DVB Subtitle PES: Reset clears all in-flight buffers and normalizers")
    func dvbSubtitleResetClearsState() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData = Data(repeating: 0xCC, count: 100)
        let pesData = makeDVBSubtitlePES(pts90k: 90000, segments: segmentData, includeEndMarker: true)

        assembler.feed(payload: pesData.prefix(40), unitStart: true)
        assembler.reset()

        // Feed continuation chunk after reset without PUSI -> must be ignored
        assembler.feed(payload: pesData.dropFirst(40), unitStart: false)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.isEmpty)
    }
}

// MARK: - Test Helpers

private final class MockDVBSubtitlePESSink: DVBSubtitlePESAssemblerDelegate, @unchecked Sendable {
    var packets: [DVBSubtitlePESPacket] = []
    var errors: [String] = []

    func dvbSubtitleAssembler(_ assembler: DVBSubtitlePESAssembler, didEmitPacket packet: DVBSubtitlePESPacket) {
        packets.append(packet)
    }

    func dvbSubtitleAssembler(_ assembler: DVBSubtitlePESAssembler, didEncounterError reason: String) {
        errors.append(reason)
    }
}

private func makeDVBSubtitlePES(
    pts90k: UInt64?,
    subtitleStreamID: UInt8 = 0x00,
    segments: Data,
    includeEndMarker: Bool = true
) -> Data {
    var headerData = Data()
    let flags2: UInt8
    let headerDataLength: UInt8

    if let pts = pts90k {
        flags2 = 0x80 // PTS present
        headerDataLength = 5
        let pts32_30 = UInt8((pts >> 30) & 0x07)
        let pts29_15 = UInt16((pts >> 15) & 0x7FFF)
        let pts14_0  = UInt16(pts & 0x7FFF)

        let b0 = 0x20 | (pts32_30 << 1) | 0x01
        let b1 = UInt8((pts29_15 >> 7) & 0xFF)
        let b2 = UInt8((pts29_15 << 1) & 0xFE) | 0x01
        let b3 = UInt8((pts14_0 >> 7) & 0xFF)
        let b4 = UInt8((pts14_0 << 1) & 0xFE) | 0x01

        headerData.append(contentsOf: [b0, b1, b2, b3, b4])
    } else {
        flags2 = 0x00 // No PTS
        headerDataLength = 0
    }

    // DVB Subtitling payload: data_identifier (0x20) + subtitle_stream_id + segments + optional 0xFF end marker
    var dvbPayload = Data([0x20, subtitleStreamID])
    dvbPayload.append(segments)
    if includeEndMarker {
        dvbPayload.append(0xFF)
    }

    let pesPacketLength = 3 + Int(headerDataLength) + dvbPayload.count

    var pes = Data()
    // Start code prefix 0x000001
    pes.append(contentsOf: [0x00, 0x00, 0x01])
    // Stream ID: 0xBD (private_stream_1)
    pes.append(0xBD)
    // PES_packet_length
    pes.append(UInt8((pesPacketLength >> 8) & 0xFF))
    pes.append(UInt8(pesPacketLength & 0xFF))
    // Flags 1: '10' marker
    pes.append(0x80)
    // Flags 2: PTS_DTS_flags
    pes.append(flags2)
    // Header data length
    pes.append(headerDataLength)
    // Header data (PTS)
    pes.append(headerData)
    // DVB Payload
    pes.append(dvbPayload)

    return pes
}
