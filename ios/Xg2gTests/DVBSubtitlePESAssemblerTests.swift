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

    @Test("DVB Subtitle PES Validation: Rejects PES_packet_length == 0 for private_stream_1 (0xBD)")
    func dvbSubtitleRejectsZeroPacketLength() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        var pesData = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: true)
        pesData[4] = 0x00
        pesData[5] = 0x00 // Force PES_packet_length = 0

        assembler.feed(payload: pesData, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Invalid PES_packet_length 0") }))
    }

    @Test("DVB Subtitle PES Validation: Rejects missing or invalid 0xFF end marker")
    func dvbSubtitleRejectsMissingEndMarker() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        // 1. Missing end marker
        let pesWithoutEndMarker = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: false)
        assembler.feed(payload: pesWithoutEndMarker, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Missing or invalid DVB end_of_PES_data_field_marker 0xFF") }))

        sink.errors.removeAll()

        // 2. Corrupt end marker (0xFE instead of 0xFF)
        var pesCorruptEndMarker = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: true)
        pesCorruptEndMarker[pesCorruptEndMarker.count - 1] = 0xFE
        assembler.feed(payload: pesCorruptEndMarker, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Missing or invalid DVB end_of_PES_data_field_marker 0xFF") }))
    }

    @Test("DVB Subtitle PES Validation: Rejects forbidden PTS_DTS_flags == 01 ('01')")
    func dvbSubtitleRejectsForbiddenPTSDTSFlag() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        var pesData = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: true)
        // flags2 is at byte 7: set top 2 bits to '01' (0x40)
        pesData[7] = 0x40

        assembler.feed(payload: pesData, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Forbidden PTS_DTS_flags") }))
    }

    @Test("DVB Subtitle PES Validation: Rejects PTS flag with truncated header data length")
    func dvbSubtitleRejectsTruncatedHeaderData() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        var pesData = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: true)
        // headerDataLength is at byte 8: set to 4 (< 5 required for PTS)
        pesData[8] = 0x04

        assembler.feed(payload: pesData, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("headerDataLength") }))
    }

    @Test("DVB Subtitle PES Validation: Rejects malformed PTS marker bits or prefix")
    func dvbSubtitleRejectsMalformedPTSMarkerBits() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        // 1. Corrupt marker bit at byte 9 (first PTS byte)
        var pesCorruptMarker = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: true)
        pesCorruptMarker[9] &= ~0x01 // clear marker bit 0
        assembler.feed(payload: pesCorruptMarker, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Malformed PTS timestamp") }))

        sink.errors.removeAll()

        // 2. Corrupt PTS prefix (0x10 instead of 0x20)
        var pesCorruptPrefix = makeDVBSubtitlePES(pts90k: 90000, segments: Data([0x0F, 0x80]), includeEndMarker: true)
        pesCorruptPrefix[9] = (pesCorruptPrefix[9] & 0x0F) | 0x10
        assembler.feed(payload: pesCorruptPrefix, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Malformed PTS timestamp") }))
    }

    @Test("DVB Subtitle PES Validation: PTS+DTS flags ('11') with valid DTS vs corrupt DTS")
    func dvbSubtitlePTSAndDTSValidation() {
        let assembler = DVBSubtitlePESAssembler()
        let sink = MockDVBSubtitlePESSink()
        assembler.delegate = sink

        let segmentData = Data([0x0F, 0x10, 0x00, 0x01, 0x00, 0x02, 0x11, 0x22])

        // 1. Valid PTS (90k = 90000) + Valid DTS (90k = 45000)
        let validPTSDTS = makeDVBSubtitlePES(pts90k: 90000, dts90k: 45000, segments: segmentData, includeEndMarker: true)
        assembler.feed(payload: validPTSDTS, unitStart: true)
        #expect(sink.packets.count == 1)
        #expect(sink.packets[0].pts90k == 90000)
        #expect(sink.errors.isEmpty)

        sink.packets.removeAll()
        sink.errors.removeAll()

        // 2. Valid PTS + Corrupt DTS prefix (0x20 instead of 0x10 / '0001')
        var corruptDTSPrefix = makeDVBSubtitlePES(pts90k: 90000, dts90k: 45000, segments: segmentData, includeEndMarker: true)
        // DTS starts at byte 14: set top nibble to 0x20 ('0010') instead of 0x10 ('0001')
        corruptDTSPrefix[14] = (corruptDTSPrefix[14] & 0x0F) | 0x20
        assembler.feed(payload: corruptDTSPrefix, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Malformed DTS timestamp") }))

        sink.errors.removeAll()

        // 3. Valid PTS + Corrupt DTS marker bit
        var corruptDTSMarker = makeDVBSubtitlePES(pts90k: 90000, dts90k: 45000, segments: segmentData, includeEndMarker: true)
        corruptDTSMarker[14] &= ~0x01 // clear marker bit
        assembler.feed(payload: corruptDTSMarker, unitStart: true)
        #expect(sink.packets.isEmpty)
        #expect(sink.errors.contains(where: { $0.contains("Malformed DTS timestamp") }))
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

    @Test("Pipeline Wiring: Selective routing of DVB subtitle PID, format-isolated continuity errors, and zap reset")
    func pipelineDVBSubtitleRoutingAndIsolation() async {
        let pipeline = NativeTSVideoPipeline()
        let sink = MockDVBSubtitlePESSink()
        pipeline.dvbSubtitleAssembler.delegate = sink

        let dvbTrack = SubtitleTrackInfo(pid: 258, format: .dvb(compositionPageID: 1, ancillaryPageID: 1), language: "deu", isHearingImpaired: true)
        let ttxTrack = SubtitleTrackInfo(pid: 259, format: .teletext(page: 777), language: "deu", isHearingImpaired: true)

        let parser = TSPacketParser()
        parser.delegate = pipeline

        // 1. Select DVB Subtitle Track (PID 258)
        pipeline.selectSubtitleTrack(dvbTrack)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == dvbTrack)

        let segmentData = Data([0x0F, 0x10, 0x00, 0x01, 0x00, 0x02, 0xAA, 0xBB])
        let pesData = makeDVBSubtitlePES(pts90k: 90000, segments: segmentData, includeEndMarker: true)

        // 2. Feed on Video PID 256 and Teletext PID 259 -> Must NOT reach DVB subtitle assembler
        pipeline.tsParser(parser, didEmitPayload: pesData, pid: 256, unitStart: true)
        pipeline.tsParser(parser, didEmitPayload: pesData, pid: 259, unitStart: true)
        #expect(sink.packets.isEmpty, "Unrelated PIDs must not feed DVB subtitle assembler")

        // 3. Continuity error on Video PID 256 or Teletext PID 259 when DVB is selected -> Must NOT trigger subtitle continuity error
        pipeline.tsParser(parser, didEncounterContinuityErrorOnPID: 256, expected: 1, actual: 3)
        pipeline.tsParser(parser, didEncounterContinuityErrorOnPID: 259, expected: 2, actual: 5)
        #expect(sink.errors.isEmpty, "Foreign PID continuity errors must not affect DVB subtitle assembler")

        // 4. Feed on selected DVB PID 258 -> Emits packet cleanly
        pipeline.tsParser(parser, didEmitPayload: pesData, pid: 258, unitStart: true)
        #expect(sink.packets.count == 1)
        #expect(sink.packets[0].pts90k == 90000)
        #expect(sink.packets[0].payload == segmentData)

        // 5. Continuity error on selected DVB PID 258 -> Triggers subtitle continuity error
        pipeline.tsParser(parser, didEncounterContinuityErrorOnPID: 258, expected: 4, actual: 6)
        #expect(sink.errors.count == 1)
        #expect(sink.errors[0].contains("Continuity error on subtitle PID"))

        // 6. Track Switch to Teletext
        sink.errors.removeAll()
        pipeline.selectSubtitleTrack(ttxTrack)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == ttxTrack)

        // 7. Continuity error on Teletext PID 259 while Teletext is selected -> Must NOT trigger DVB subtitle continuity error
        pipeline.tsParser(parser, didEncounterContinuityErrorOnPID: 259, expected: 1, actual: 4)
        #expect(sink.errors.isEmpty, "Teletext continuity error must NOT affect DVBSubtitlePESAssembler")

        // 8. Feeding PID 258 after switch is ignored
        sink.packets.removeAll()
        pipeline.tsParser(parser, didEmitPayload: pesData, pid: 258, unitStart: true)
        #expect(sink.packets.isEmpty)
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
    dts90k: UInt64? = nil,
    subtitleStreamID: UInt8 = 0x00,
    segments: Data,
    includeEndMarker: Bool = true
) -> Data {
    var headerData = Data()
    let flags2: UInt8
    let headerDataLength: UInt8

    if let pts = pts90k, let dts = dts90k {
        flags2 = 0xC0 // PTS and DTS present ('11')
        headerDataLength = 10

        // PTS with '0011' (0x30) prefix
        let pts32_30 = UInt8((pts >> 30) & 0x07)
        let pts29_15 = UInt16((pts >> 15) & 0x7FFF)
        let pts14_0  = UInt16(pts & 0x7FFF)

        let p0 = 0x30 | (pts32_30 << 1) | 0x01
        let p1 = UInt8((pts29_15 >> 7) & 0xFF)
        let p2 = UInt8((pts29_15 << 1) & 0xFE) | 0x01
        let p3 = UInt8((pts14_0 >> 7) & 0xFF)
        let p4 = UInt8((pts14_0 << 1) & 0xFE) | 0x01
        headerData.append(contentsOf: [p0, p1, p2, p3, p4])

        // DTS with '0001' (0x10) prefix
        let dts32_30 = UInt8((dts >> 30) & 0x07)
        let dts29_15 = UInt16((dts >> 15) & 0x7FFF)
        let dts14_0  = UInt16(dts & 0x7FFF)

        let d0 = 0x10 | (dts32_30 << 1) | 0x01
        let d1 = UInt8((dts29_15 >> 7) & 0xFF)
        let d2 = UInt8((dts29_15 << 1) & 0xFE) | 0x01
        let d3 = UInt8((dts14_0 >> 7) & 0xFF)
        let d4 = UInt8((dts14_0 << 1) & 0xFE) | 0x01
        headerData.append(contentsOf: [d0, d1, d2, d3, d4])
    } else if let pts = pts90k {
        flags2 = 0x80 // PTS present ('10')
        headerDataLength = 5
        let pts32_30 = UInt8((pts >> 30) & 0x07)
        let pts29_15 = UInt16((pts >> 15) & 0x7FFF)
        let pts14_0  = UInt16(pts & 0x7FFF)

        let b0 = 0x20 | (pts32_30 << 1) | 0x01 // Prefix '0010' (0x20), 3 bits PTS, marker bit 1
        let b1 = UInt8((pts29_15 >> 7) & 0xFF)
        let b2 = UInt8((pts29_15 << 1) & 0xFE) | 0x01 // 8 bits PTS, marker bit 1
        let b3 = UInt8((pts14_0 >> 7) & 0xFF)
        let b4 = UInt8((pts14_0 << 1) & 0xFE) | 0x01 // 8 bits PTS, marker bit 1

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
    // Header data (PTS / DTS)
    pes.append(headerData)
    // DVB Payload
    pes.append(dvbPayload)

    return pes
}
