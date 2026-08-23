// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct DVBSubtitleSegmentParserTests {

    @Test("DDS (0x14): Display Definition Segment parsing with and without display window")
    func ddsSegmentParsing() {
        let parser = DVBSubtitleSegmentParser()

        // DDS: 1920x1080 (0x077F = 1919, 0x0437 = 1079) with window (minX=100, maxX=1820, minY=50, maxY=1030)
        var ddsPayload = Data()
        ddsPayload.append(0x18) // version 1, window_flag = 1
        ddsPayload.append(contentsOf: [0x07, 0x7F]) // width 1919 -> 1920
        ddsPayload.append(contentsOf: [0x04, 0x37]) // height 1079 -> 1080
        ddsPayload.append(contentsOf: [0x00, 0x64]) // minX 100
        ddsPayload.append(contentsOf: [0x07, 0x1C]) // maxX 1820
        ddsPayload.append(contentsOf: [0x00, 0x32]) // minY 50
        ddsPayload.append(contentsOf: [0x04, 0x06]) // maxY 1030

        let segmentData = makeSegment(type: 0x14, pageID: 1, payload: ddsPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        let seg = segments[0]
        #expect(seg.type == 0x14)
        #expect(seg.pageID == 1)

        guard case let .displayDefinition(dds) = seg.payload else {
            Issue.record("Expected .displayDefinition payload")
            return
        }

        #expect(dds.versionNumber == 1)
        #expect(dds.displayWidth == 1920)
        #expect(dds.displayHeight == 1080)
        #expect(dds.windowHorizontalPositionMinimum == 100)
        #expect(dds.windowHorizontalPositionMaximum == 1820)
        #expect(dds.windowVerticalPositionMinimum == 50)
        #expect(dds.windowVerticalPositionMaximum == 1030)
    }

    @Test("PCS (0x10): Page Composition Segment with multiple regions and page states")
    func pcsSegmentParsing() {
        let parser = DVBSubtitleSegmentParser()

        // PCS: timeout 30s, version 2, state = Mode Change (0x02), 2 regions
        var pcsPayload = Data()
        pcsPayload.append(30) // timeout 30s
        pcsPayload.append((2 << 4) | (0x02 << 2)) // version 2, state modeChange

        // Region 1: ID 0x01 at (x: 100, y: 800)
        pcsPayload.append(contentsOf: [0x01, 0xFF, 0x00, 0x64, 0x03, 0x20])
        // Region 2: ID 0x02 at (x: 200, y: 900)
        pcsPayload.append(contentsOf: [0x02, 0xFF, 0x00, 0xC8, 0x03, 0x84])

        let segmentData = makeSegment(type: 0x10, pageID: 1, payload: pcsPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        guard case let .pageComposition(pcs) = segments[0].payload else {
            Issue.record("Expected .pageComposition payload")
            return
        }

        #expect(pcs.pageTimeOut == 30)
        #expect(pcs.versionNumber == 2)
        #expect(pcs.state == .modeChange)
        #expect(pcs.regions.count == 2)

        #expect(pcs.regions[0].regionID == 1)
        #expect(pcs.regions[0].horizontalAddress == 100)
        #expect(pcs.regions[0].verticalAddress == 800)

        #expect(pcs.regions[1].regionID == 2)
        #expect(pcs.regions[1].horizontalAddress == 200)
        #expect(pcs.regions[1].verticalAddress == 900)
    }

    @Test("RCS (0x11): Region Composition Segment with object references")
    func rcsSegmentParsing() {
        let parser = DVBSubtitleSegmentParser()

        // RCS: regionID 1, version 1, fill = true, width 720, height 80, depth 2 (4-bit), CLUT 1, 1 object
        var rcsPayload = Data()
        rcsPayload.append(0x01) // regionID 1
        rcsPayload.append((1 << 4) | 0x08) // version 1, fillFlag = true
        rcsPayload.append(contentsOf: [0x02, 0xD0]) // width 720
        rcsPayload.append(contentsOf: [0x00, 0x50]) // height 80
        rcsPayload.append((0x01 << 5) | (0x02 << 2)) // compat 1, depth 2 (4-bit)
        rcsPayload.append(0x01) // clutID 1
        rcsPayload.append(0x00) // 8-bit pixel code
        rcsPayload.append((0x03 << 4) | (0x01 << 2)) // 4-bit code 3, 2-bit code 1

        // Object 1: ID 100, type 0 (bitmap), provider 0, posX: 10, posY: 5
        rcsPayload.append(contentsOf: [
            0x00, 0x64, // objectID 100
            0x00, 0x0A, // type 0, provider 0, hPos 10 (0x000A)
            0x00, 0x05  // vPos 5 (0x0005)
        ])

        let segmentData = makeSegment(type: 0x11, pageID: 1, payload: rcsPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        guard case let .regionComposition(rcs) = segments[0].payload else {
            Issue.record("Expected .regionComposition payload")
            return
        }

        #expect(rcs.regionID == 1)
        #expect(rcs.versionNumber == 1)
        #expect(rcs.fillFlag == true)
        #expect(rcs.width == 720)
        #expect(rcs.height == 80)
        #expect(rcs.depth == 2)
        #expect(rcs.clutID == 1)
        #expect(rcs.pixelCode4Bit == 3)
        #expect(rcs.objects.count == 1)
        #expect(rcs.objects[0].objectID == 100)
        #expect(rcs.objects[0].objectType == 0)
        #expect(rcs.objects[0].horizontalPosition == 10)
        #expect(rcs.objects[0].verticalPosition == 5)
    }

    @Test("CLUT (0x12): CLUT Definition Segment with Full-Range and Compact entries")
    func clutSegmentParsing() {
        let parser = DVBSubtitleSegmentParser()

        // CLUT: ID 1, version 0, 2 entries (Entry 0: full range, Entry 1: compact)
        var clutPayload = Data()
        clutPayload.append(0x01) // clutID 1
        clutPayload.append(0x00) // version 0

        // Entry 0: fullRange = 1, 4-bit flag, Y=255, Cr=128, Cb=128, T=255
        clutPayload.append(contentsOf: [0x00, 0x41, 0xFF, 0x80, 0x80, 0xFF])

        // Entry 1: fullRange = 0, 4-bit flag, 2-byte compact format
        // Y = 0x3F (63), Cr = 0x08, Cb = 0x08, T = 0x03
        // Byte 2: (63 << 2) | (0x08 >> 2) = 0xFC | 0x02 = 0xFE
        // Byte 3: ((0x08 & 3) << 6) | (0x08 << 2) | 3 = 0x00 | 0x20 | 0x03 = 0x23
        clutPayload.append(contentsOf: [0x01, 0x40, 0xFE, 0x23])

        let segmentData = makeSegment(type: 0x12, pageID: 1, payload: clutPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        guard case let .clutDefinition(clut) = segments[0].payload else {
            Issue.record("Expected .clutDefinition payload")
            return
        }

        #expect(clut.clutID == 1)
        #expect(clut.versionNumber == 0)
        #expect(clut.entries.count == 2)

        // Entry 0
        #expect(clut.entries[0].entryID == 0)
        #expect(clut.entries[0].y == 255)
        #expect(clut.entries[0].cr == 128)
        #expect(clut.entries[0].cb == 128)
        #expect(clut.entries[0].t == 255)

        // Entry 1
        #expect(clut.entries[1].entryID == 1)
        #expect(clut.entries[1].y == 255) // 63 scaled to 8-bit = 255
        #expect(clut.entries[1].t == 255)
    }

    @Test("ODS (0x13): Object Data Segment with Top and Bottom field bitmap payloads")
    func odsSegmentParsing() {
        let parser = DVBSubtitleSegmentParser()

        // ODS: objectID 100, version 1, codingMethod 0 (pixels), nonModifying false
        // topFieldLen 4: [0x11, 0x22, 0x33, 0x44]
        // bottomFieldLen 4: [0x55, 0x66, 0x77, 0x88]
        var odsPayload = Data()
        odsPayload.append(contentsOf: [0x00, 0x64]) // objectID 100
        odsPayload.append(0x10) // version 1, codingMethod 0
        odsPayload.append(contentsOf: [0x00, 0x04]) // topLen 4
        odsPayload.append(contentsOf: [0x00, 0x04]) // bottomLen 4
        odsPayload.append(contentsOf: [0x11, 0x22, 0x33, 0x44]) // topField
        odsPayload.append(contentsOf: [0x55, 0x66, 0x77, 0x88]) // bottomField

        let segmentData = makeSegment(type: 0x13, pageID: 1, payload: odsPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        guard case let .objectData(ods) = segments[0].payload else {
            Issue.record("Expected .objectData payload")
            return
        }

        #expect(ods.objectID == 100)
        #expect(ods.versionNumber == 1)
        #expect(ods.codingMethod == .pixels)
        #expect(ods.nonModifyingColorFlag == false)
        #expect(ods.topFieldData == Data([0x11, 0x22, 0x33, 0x44]))
        #expect(ods.bottomFieldData == Data([0x55, 0x66, 0x77, 0x88]))
    }

    @Test("EoDS (0x80): End of Display Set Segment")
    func eodsSegmentParsing() {
        let parser = DVBSubtitleSegmentParser()
        let segmentData = makeSegment(type: 0x80, pageID: 1, payload: Data())
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        #expect(segments[0].type == 0x80)
        #expect(segments[0].payload == .endOfDisplaySet)
    }

    @Test("Multi-Segment: Complete Display Set with PCS, RCS, CLUT, ODS, and EoDS in a single PES payload")
    func multiSegmentDisplaySetParsing() {
        let parser = DVBSubtitleSegmentParser()

        var stream = Data()

        // 1. PCS (Page ID 1)
        var pcs = Data()
        pcs.append(contentsOf: [10, (1 << 4) | (0x00 << 2), 0x01, 0xFF, 0x00, 0x64, 0x01, 0x00])
        stream.append(makeSegment(type: 0x10, pageID: 1, payload: pcs))

        // 2. RCS (Page ID 1)
        var rcs = Data()
        rcs.append(contentsOf: [0x01, 0x10, 0x01, 0x00, 0x00, 0x20, 0x28, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00])
        stream.append(makeSegment(type: 0x11, pageID: 1, payload: rcs))

        // 3. CLUT (Page ID 2 - Ancillary Page)
        var clut = Data()
        clut.append(contentsOf: [0x01, 0x00, 0x00, 0x41, 0xFF, 0x80, 0x80, 0xFF])
        stream.append(makeSegment(type: 0x12, pageID: 2, payload: clut))

        // 4. ODS (Page ID 1)
        var ods = Data()
        ods.append(contentsOf: [0x00, 0x01, 0x00, 0x00, 0x02, 0x00, 0x00, 0xAA, 0xBB])
        stream.append(makeSegment(type: 0x13, pageID: 1, payload: ods))

        // 5. EoDS (Page ID 1)
        stream.append(makeSegment(type: 0x80, pageID: 1, payload: Data()))

        let segments = parser.parse(data: stream)
        #expect(segments.count == 5)

        #expect(segments[0].type == 0x10 && segments[0].pageID == 1)
        #expect(segments[1].type == 0x11 && segments[1].pageID == 1)
        #expect(segments[2].type == 0x12 && segments[2].pageID == 2, "Ancillary CLUT must retain its page ID")
        #expect(segments[3].type == 0x13 && segments[3].pageID == 1)
        #expect(segments[4].type == 0x80 && segments[4].pageID == 1)
    }

    @Test("Resilience: Truncated last segment is discarded without failing preceding segments")
    func truncatedLastSegmentDiscardedSafely() {
        let parser = DVBSubtitleSegmentParser()
        let sink = MockSegmentParserSink()
        parser.delegate = sink

        var stream = Data()

        // Valid PCS
        var pcs = Data()
        pcs.append(contentsOf: [10, (1 << 4), 0x01, 0xFF, 0x00, 0x64, 0x01, 0x00])
        stream.append(makeSegment(type: 0x10, pageID: 1, payload: pcs))

        // Incomplete RCS (claims 20 bytes, but only 5 provided)
        stream.append(contentsOf: [0x0F, 0x11, 0x00, 0x01, 0x00, 0x14, 0x01, 0x02, 0x03, 0x04, 0x05])

        let segments = parser.parse(data: stream)
        #expect(segments.count == 1, "Only the valid PCS should be parsed")
        #expect(segments[0].type == 0x10)
        #expect(sink.warnings.count == 1)
        #expect(sink.warnings[0].contains("Truncated segment"))
    }

    @Test("Resilience: Unknown segment type is gracefully skipped without error")
    func unknownSegmentTypeSkipped() {
        let parser = DVBSubtitleSegmentParser()

        var stream = Data()

        // Unknown segment 0x99 with 4 bytes payload
        stream.append(makeSegment(type: 0x99, pageID: 1, payload: Data([0x01, 0x02, 0x03, 0x04])))

        // Followed by valid EoDS
        stream.append(makeSegment(type: 0x80, pageID: 1, payload: Data()))

        let segments = parser.parse(data: stream)
        #expect(segments.count == 2)
        #expect(segments[0].type == 0x99)
        guard case let .unknown(type, data) = segments[0].payload else {
            Issue.record("Expected .unknown payload")
            return
        }
        #expect(type == 0x99)
        #expect(data == Data([0x01, 0x02, 0x03, 0x04]))
        #expect(segments[1].type == 0x80)
    }

    @Test("Resilience: Invalid sync byte stops parser cleanly")
    func invalidSyncByteStopsParser() {
        let parser = DVBSubtitleSegmentParser()
        let sink = MockSegmentParserSink()
        parser.delegate = sink

        // Stream with invalid sync byte (0x0E instead of 0x0F)
        let corruptData = Data([0x0E, 0x10, 0x00, 0x01, 0x00, 0x02, 0x00, 0x00])
        let segments = parser.parse(data: corruptData)

        #expect(segments.isEmpty)
        #expect(sink.warnings.count == 1)
        #expect(sink.warnings[0].contains("Invalid sync_byte"))
    }
}

// MARK: - Test Helpers

private final class MockSegmentParserSink: DVBSubtitleSegmentParserDelegate, @unchecked Sendable {
    var parsedSegments: [DVBSubtitleSegment] = []
    var warnings: [String] = []

    func dvbSegmentParser(_ parser: DVBSubtitleSegmentParser, didParseSegment segment: DVBSubtitleSegment) {
        parsedSegments.append(segment)
    }

    func dvbSegmentParser(_ parser: DVBSubtitleSegmentParser, didEncounterWarning warning: String) {
        warnings.append(warning)
    }
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
