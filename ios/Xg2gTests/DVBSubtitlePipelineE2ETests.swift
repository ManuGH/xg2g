// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreGraphics
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct DVBSubtitlePipelineE2ETests {

    @Test("E2E DVB Pipeline: 188-byte TS feed (PAT -> PMT -> 188-Byte TS Packets) decodes and composites subtitle CGImage frame")
    func e2eDVBSubtitlePipelineRendersBitmapFromPES() async {
        let pipeline = NativeTSVideoPipeline()
        let parser = TSPacketParser()
        parser.delegate = pipeline

        // 1. Ingest PAT (PID 0 -> PMT PID 100)
        let patPacket = makePAT(programNumber: 1, pmtPID: 100)
        parser.feed(data: patPacket)

        // 2. Ingest PMT with DVB Subtitle (PID 200, descriptor 0x59, compID 1, ancID 1)
        var streamData = Data()
        streamData.append(0x06) // stream_type: private PES
        streamData.append(0xE0 | 0x00) // PID high 200 -> 0x00C8
        streamData.append(0xC8) // PID low
        var dvbDesc = Data([0x59, 0x08])
        dvbDesc.append(contentsOf: [0x64, 0x65, 0x75]) // "deu"
        dvbDesc.append(0x20) // hearing impaired DVB
        dvbDesc.append(contentsOf: [0x00, 0x01, 0x00, 0x01]) // comp 1, anc 1
        streamData.append(0xF0 | UInt8((dvbDesc.count >> 8) & 0x0F))
        streamData.append(UInt8(dvbDesc.count & 0xFF))
        streamData.append(dvbDesc)

        let pmtPacket = makePMT(pmtPID: 100, programNumber: 1, pcrPID: 101, streamData: streamData)
        parser.feed(data: pmtPacket)

        #expect(pipeline.availableSubtitleTracks.count == 1)
        let track = pipeline.availableSubtitleTracks[0]
        #expect(track.pid == 200)

        // 3. User selects the discovered subtitle track
        pipeline.selectSubtitleTrack(track)
        try? await Task.sleep(nanoseconds: 50_000_000) // 50ms for ingestQueue

        // 4. Build a complete DVB PES payload with DDS, PCS, RCS, CLUT, ODS, EoDS
        var segmentStream = Data()

        // DDS (0x14): 1920x1080
        var ddsPayload = Data()
        ddsPayload.append(0x00) // version 0, no window
        ddsPayload.append(contentsOf: [0x07, 0x7F]) // 1919 -> 1920
        ddsPayload.append(contentsOf: [0x04, 0x37]) // 1079 -> 1080
        segmentStream.append(makeSegment(type: 0x14, pageID: 1, payload: ddsPayload))

        // PCS (0x10): Mode Change, 1 region at (100, 900)
        var pcsPayload = Data()
        pcsPayload.append(10) // timeout 10s
        pcsPayload.append(0x08) // version 0, modeChange (0x02 << 2)
        pcsPayload.append(contentsOf: [0x01, 0xFF, 0x00, 0x64, 0x03, 0x84]) // Region 1 at (100, 900)
        segmentStream.append(makeSegment(type: 0x10, pageID: 1, payload: pcsPayload))

        // RCS (0x11): Region 1 (width 400, height 50, depth 2-bit, CLUT 1, 1 object)
        var rcsPayload = Data()
        rcsPayload.append(0x01) // regionID 1
        rcsPayload.append((0 << 4) | 0x08) // version 0, fill = true
        rcsPayload.append(contentsOf: [0x01, 0x90]) // width 400
        rcsPayload.append(contentsOf: [0x00, 0x32]) // height 50
        rcsPayload.append((0x01 << 5) | (0x01 << 2)) // depth 1 (2-bit)
        rcsPayload.append(0x01) // clutID 1
        rcsPayload.append(0x00) // pixel code 8-bit
        rcsPayload.append(0x00) // pixel code 4-bit / 2-bit
        rcsPayload.append(contentsOf: [0x00, 0x0A, 0x00, 0x00, 0x00, 0x00]) // Object 10 at (0, 0)
        segmentStream.append(makeSegment(type: 0x11, pageID: 1, payload: rcsPayload))

        // CLUT (0x12): CLUT 1 (Entry 0: transparent, Entry 1: yellow)
        var clutPayload = Data([0x01, 0x00])
        clutPayload.append(contentsOf: [0x00, 0x41, 0, 128, 128, 0]) // Entry 0: Y=0 transparent
        clutPayload.append(contentsOf: [0x01, 0x41, 210, 146, 16, 0]) // Entry 1: Yellow
        segmentStream.append(makeSegment(type: 0x12, pageID: 1, payload: clutPayload))

        // ODS (0x13): Object 10 (2-bit RLE spanning multiple lines to exceed single TS payload capacity)
        var odsPayload = Data()
        odsPayload.append(contentsOf: [0x00, 0x0A, 0x00]) // objectID 10, version 0, codingMethod pixels
        var rleTop = Data([0x10]) // 2-bit
        for _ in 0..<50 {
            rleTop.append(contentsOf: [0x55, 0x55, 0x00, 0x00]) // 8 pixels + EOL
        }
        odsPayload.append(contentsOf: [UInt8((rleTop.count >> 8) & 0xFF), UInt8(rleTop.count & 0xFF)])
        odsPayload.append(contentsOf: [0x00, 0x00]) // bottom field length 0
        odsPayload.append(rleTop)
        segmentStream.append(makeSegment(type: 0x13, pageID: 1, payload: odsPayload))

        // EoDS (0x80)
        segmentStream.append(makeSegment(type: 0x80, pageID: 1, payload: Data()))

        // Build PES Packet (PTS = 15.0s, 90k = 1350000)
        let pts90k: UInt64 = 1350000
        let pesData = makeDVBSubtitlePES(pts90k: pts90k, segmentData: segmentStream)

        // 5. Packetize into 188-byte MPEG-TS packets on PID 200 with adaptation fields & feed to TSPacketParser
        let tsPackets = makeTSPackets(pid: 200, payload: pesData)
        #expect(tsPackets.count > 1, "Must span multiple 188-byte TS packets")

        for tsPacket in tsPackets {
            #expect(tsPacket.count == 188)
            parser.feed(data: tsPacket)
        }

        // Wait briefly for ingestQueue processing
        try? await Task.sleep(nanoseconds: 50_000_000) // 50ms

        let currentFrame = pipeline.currentSubtitleFrame
        #expect(currentFrame != nil)
        #expect(currentFrame?.isVisible == true)
        #expect(currentFrame?.image != nil)
        #expect(currentFrame?.displayWidth == 1920)
        #expect(currentFrame?.displayHeight == 1080)
        #expect(currentFrame?.presentationPTS?.seconds == 15.0)
        #expect(currentFrame?.timeoutDeadlinePTS?.seconds == 25.0) // 15 + 10s
    }

    @Test("E2E Track Switching: Deselecting subtitle track clears rendered subtitle frame")
    func e2eDVBSubtitleTrackDeselectionClearsFrame() async {
        let pipeline = NativeTSVideoPipeline()

        let track = SubtitleTrackInfo(
            pid: 200,
            format: .dvb(compositionPageID: 1, ancillaryPageID: 1),
            language: "deu",
            isHearingImpaired: true
        )
        pipeline.selectSubtitleTrack(track)
        try? await Task.sleep(nanoseconds: 20_000_000)

        // Deselect
        pipeline.selectSubtitleTrack(nil)
        try? await Task.sleep(nanoseconds: 20_000_000)

        #expect(pipeline.selectedSubtitleTrack == nil)
        #expect(pipeline.currentSubtitleFrame == nil)
    }

    @Test("Map-Table Decoding: 2-to-4-bit Map Table properly remaps pixel color indices")
    func mapTable2to4RemapsPixelColors() {
        let decoder = DVBSubtitleRLEDecoder()

        // 2-to-4-bit Map Table (0x20):
        // Map 0 -> 0, Map 1 -> 9, Map 2 -> 14, Map 3 -> 15
        // Bytes: [0x09, 0xEF]
        var bitstream = Data([0x20, 0x09, 0xEF])

        // 2-bit RLE data (0x10):
        // 4 pixels of color 1 ('01 01 01 01' = 0x55) + EOL
        bitstream.append(contentsOf: [0x10, 0x55, 0x00, 0x00])

        let lines = decoder.decodeField(data: bitstream, width: 4, depth: 2)
        #expect(lines.count == 1)

        let line = lines[0]
        #expect(line.count == 4)
        #expect(Array(line) == [9, 9, 9, 9], "Color 1 must be remapped to 9 via map table 0x20")
    }
}

// MARK: - Test Helpers

private func makePAT(programNumber: UInt16, pmtPID: UInt16) -> Data {
    var pat = Data(repeating: 0xFF, count: 188)
    pat[0] = 0x47
    pat[1] = 0x40
    pat[2] = 0x00
    pat[3] = 0x10

    pat[4] = 0x00 // pointer field
    pat[5] = 0x00 // table_id = 0 (PAT)
    pat[6] = 0xB0 // section_syntax_indicator
    pat[7] = 0x0D // section_length (13 bytes)
    pat[8] = 0x00
    pat[9] = 0x01 // transport_stream_id
    pat[10] = 0xC1
    pat[11] = 0x00
    pat[12] = 0x00

    pat[13] = UInt8((programNumber >> 8) & 0xFF)
    pat[14] = UInt8(programNumber & 0xFF)
    pat[15] = 0xE0 | UInt8((pmtPID >> 8) & 0x1F)
    pat[16] = UInt8(pmtPID & 0xFF)

    return pat
}

private func makePMT(pmtPID: UInt16, programNumber: UInt16, pcrPID: UInt16, version: UInt8 = 0, streamData: Data) -> Data {
    var pmt = Data(repeating: 0xFF, count: 188)
    pmt[0] = 0x47
    pmt[1] = 0x40 | UInt8((pmtPID >> 8) & 0x1F)
    pmt[2] = UInt8(pmtPID & 0xFF)
    pmt[3] = 0x10

    pmt[4] = 0x00 // pointer field
    pmt[5] = 0x02 // table_id = 2 (PMT)

    let sectionLength = 9 + streamData.count + 4
    pmt[6] = 0xB0 | UInt8((sectionLength >> 8) & 0x0F)
    pmt[7] = UInt8(sectionLength & 0xFF)
    pmt[8] = UInt8((programNumber >> 8) & 0xFF)
    pmt[9] = UInt8(programNumber & 0xFF)
    pmt[10] = 0xC1 | ((version & 0x1F) << 1)
    pmt[11] = 0x00
    pmt[12] = 0x00
    pmt[13] = 0xE0 | UInt8((pcrPID >> 8) & 0x1F)
    pmt[14] = UInt8(pcrPID & 0xFF)
    pmt[15] = 0xF0
    pmt[16] = 0x00

    pmt.replaceSubrange(17..<(17 + streamData.count), with: streamData)
    return pmt
}

private func makeSegment(type: UInt8, pageID: UInt16, payload: Data) -> Data {
    var seg = Data()
    seg.append(0x0F) // sync_byte
    seg.append(type) // segment_type
    seg.append(UInt8((pageID >> 8) & 0xFF))
    seg.append(UInt8(pageID & 0xFF))
    seg.append(UInt8((payload.count >> 8) & 0xFF))
    seg.append(UInt8(payload.count & 0xFF))
    seg.append(payload)
    return seg
}

private func makeDVBSubtitlePES(pts90k: UInt64, segmentData: Data) -> Data {
    var pes = Data()
    pes.append(contentsOf: [0x00, 0x00, 0x01, 0xBD]) // Start code + private_stream_1

    // DVB Subtitle payload: data_identifier (0x20) + subtitle_stream_id (0x00) + segments + 0xFF
    var dvbPayload = Data([0x20, 0x00])
    dvbPayload.append(segmentData)
    dvbPayload.append(0xFF) // End marker

    // PES Header: Flags = 0x80 (PTS present = 0x80), HeaderDataLen = 5
    let pesHeaderLen = 3 + 5 // flags (2) + len (1) + PTS (5)
    let pesPacketLength = UInt16(pesHeaderLen + dvbPayload.count)

    pes.append(UInt8((pesPacketLength >> 8) & 0xFF))
    pes.append(UInt8(pesPacketLength & 0xFF))

    // PES Header Flags
    pes.append(0x80) // '1000 0000' (standard prefix)
    pes.append(0x80) // PTS_DTS_flags = '10' (PTS only)
    pes.append(0x05) // Header data length = 5 bytes

    // 5-byte PTS encoding: '0010' prefix + marker bits
    let pts32_30 = UInt8((pts90k >> 30) & 0x07)
    let pts29_15 = UInt16((pts90k >> 15) & 0x7FFF)
    let pts14_0 = UInt16(pts90k & 0x7FFF)

    let b0 = (0x02 << 4) | (pts32_30 << 1) | 0x01
    let b1 = UInt8((pts29_15 >> 7) & 0xFF)
    let b2 = UInt8(((pts29_15 & 0x7F) << 1) | 0x01)
    let b3 = UInt8((pts14_0 >> 7) & 0xFF)
    let b4 = UInt8(((pts14_0 & 0x7F) << 1) | 0x01)

    pes.append(contentsOf: [b0, b1, b2, b3, b4])
    pes.append(dvbPayload)

    return pes
}

private func makeTSPackets(pid: UInt16, payload: Data) -> [Data] {
    var packets: [Data] = []
    var offset = 0
    var cc: UInt8 = 0

    while offset < payload.count {
        var packet = Data()
        packet.append(0x47) // Sync byte

        let isStart = (offset == 0)
        let pidHigh = UInt8((pid >> 8) & 0x1F) | (isStart ? 0x40 : 0x00) // PUSI flag
        let pidLow = UInt8(pid & 0xFF)
        packet.append(pidHigh)
        packet.append(pidLow)

        let remaining = payload.count - offset
        let maxPayloadPerTS = 184

        if remaining >= maxPayloadPerTS {
            // Adaptation field = 01 (payload only)
            packet.append(0x10 | (cc & 0x0F))
            packet.append(payload.subdata(in: offset..<(offset + maxPayloadPerTS)))
            offset += maxPayloadPerTS
        } else {
            // Adaptation field with stuffing (0x30)
            let stuffingLen = maxPayloadPerTS - remaining
            packet.append(0x30 | (cc & 0x0F))
            if stuffingLen == 1 {
                packet.append(0x00) // AF length = 0
            } else {
                let afLen = UInt8(stuffingLen - 1)
                packet.append(afLen)
                packet.append(0x00) // Flags
                if afLen > 1 {
                    packet.append(Data(repeating: 0xFF, count: Int(afLen - 1)))
                }
            }
            packet.append(payload.subdata(in: offset..<payload.count))
            offset = payload.count
        }

        cc = (cc + 1) & 0x0F
        packets.append(packet)
    }

    return packets
}
