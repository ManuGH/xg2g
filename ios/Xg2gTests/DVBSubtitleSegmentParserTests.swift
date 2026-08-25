// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct DVBSubtitleSegmentParserTests {

    @Test("DDS (0x14): Display Definition Segment parsing and boundary values with low-byte 0xFF")
    func ddsSegmentParsingAndBoundaries() {
        let parser = DVBSubtitleSegmentParser()

        // 1. Standard 1920x1080 (0x077F = 1919, 0x0437 = 1079) with display window
        var ddsPayload1 = Data()
        ddsPayload1.append(0x18) // version 1, window_flag = 1
        ddsPayload1.append(contentsOf: [0x07, 0x7F]) // width 1919 -> 1920
        ddsPayload1.append(contentsOf: [0x04, 0x37]) // height 1079 -> 1080
        ddsPayload1.append(contentsOf: [0x00, 0x64]) // minX 100
        ddsPayload1.append(contentsOf: [0x07, 0x1C]) // maxX 1820
        ddsPayload1.append(contentsOf: [0x00, 0x32]) // minY 50
        ddsPayload1.append(contentsOf: [0x04, 0x06]) // maxY 1030

        let segmentData1 = makeSegment(type: 0x14, pageID: 1, payload: ddsPayload1)
        let segments1 = parser.parse(data: segmentData1)

        #expect(segments1.count == 1)
        guard case let .displayDefinition(dds1) = segments1[0].payload else {
            Issue.record("Expected .displayDefinition payload")
            return
        }

        #expect(dds1.displayWidth == 1920)
        #expect(dds1.displayHeight == 1080)
        #expect(dds1.windowHorizontalPositionMinimum == 100)
        #expect(dds1.windowHorizontalPositionMaximum == 1820)
        #expect(dds1.windowVerticalPositionMinimum == 50)
        #expect(dds1.windowVerticalPositionMaximum == 1030)

        // 2. Boundary: Low-byte 0xFF (0x02FF = 767 -> width 768, 0x023F = 575 -> height 576)
        var ddsPayload2 = Data()
        ddsPayload2.append(0x00) // version 0, no window
        ddsPayload2.append(contentsOf: [0x02, 0xFF]) // 767 -> 768
        ddsPayload2.append(contentsOf: [0x02, 0x3F]) // 575 -> 576

        let segmentData2 = makeSegment(type: 0x14, pageID: 1, payload: ddsPayload2)
        let segments2 = parser.parse(data: segmentData2)

        #expect(segments2.count == 1)
        guard case let .displayDefinition(dds2) = segments2[0].payload else {
            Issue.record("Expected .displayDefinition payload")
            return
        }

        #expect(dds2.displayWidth == 768)
        #expect(dds2.displayHeight == 576)
        #expect(dds2.windowHorizontalPositionMinimum == nil)
    }

    @Test("CLUT (0x12): Transparency semantics (T=0 opaque, increasing T, Y=0 full transparency)")
    func clutTransparencySemantics() {
        let parser = DVBSubtitleSegmentParser()

        // CLUT: ID 1, version 0, 3 entries
        var clutPayload = Data()
        clutPayload.append(0x01) // clutID 1
        clutPayload.append(0x00) // version 0

        // Entry 0: Full Range Opaque (Y=200, Cr=128, Cb=128, T=0 -> opaque)
        clutPayload.append(contentsOf: [0x00, 0x41, 200, 128, 128, 0])

        // Entry 1: Full Range Special Full Transparency (Y=0, Cr=128, Cb=128, T=0 -> forced T=255)
        clutPayload.append(contentsOf: [0x01, 0x41, 0, 128, 128, 0])

        // Entry 2: Compact 2-byte format (Y=63, Cr=8, Cb=8, rawT=0 -> T=0 opaque)
        // rawY = 63 (0x3F), rawCr = 8, rawCb = 8, rawT = 0
        // Byte 2: (63 << 2) | (8 >> 2) = 0xFC | 0x02 = 0xFE
        // Byte 3: ((8 & 3) << 6) | (8 << 2) | 0 = 0x00 | 0x20 | 0x00 = 0x20
        clutPayload.append(contentsOf: [0x02, 0x40, 0xFE, 0x20])

        // Entry 3: Compact 2-byte format (Y=63, Cr=8, Cb=8, rawT=3 -> T=255 transparent)
        // Byte 3: 0x23 (rawT = 3)
        clutPayload.append(contentsOf: [0x03, 0x40, 0xFE, 0x23])

        let segmentData = makeSegment(type: 0x12, pageID: 1, payload: clutPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        guard case let .clutDefinition(clut) = segments[0].payload else {
            Issue.record("Expected .clutDefinition payload")
            return
        }

        #expect(clut.entries.count == 4)

        // Entry 0: Opaque
        #expect(clut.entries[0].y == 200)
        #expect(clut.entries[0].t == 0, "T=0 must represent fully opaque")
        #expect(!clut.entries[0].isFullyTransparent)

        // Entry 1: Y=0 Special Full Transparency
        #expect(clut.entries[1].y == 0)
        #expect(clut.entries[1].t == 255, "Y=0 must signal 100% full transparency (T=255)")
        #expect(clut.entries[1].isFullyTransparent)

        // Entry 2: Compact Opaque
        #expect(clut.entries[2].t == 0)
        #expect(!clut.entries[2].isFullyTransparent)

        // Entry 3: Compact Transparent
        #expect(clut.entries[3].t == 255)
        #expect(clut.entries[3].isFullyTransparent)
    }

    @Test("ODS (0x13): Reserved object_coding_method (0x02, 0x03) does NOT fall back to pixels")
    func reservedCodingMethodDoesNotFallbackToPixels() {
        let parser = DVBSubtitleSegmentParser()

        // ODS: objectID 100, version 1, codingMethod 0x02 (reserved), nonModifying false
        var odsPayload = Data()
        odsPayload.append(contentsOf: [0x00, 0x64]) // objectID 100
        odsPayload.append((1 << 4) | (0x02 << 2)) // version 1, codingMethod 0x02 (reserved)
        odsPayload.append(contentsOf: [0xAA, 0xBB, 0xCC, 0xDD])

        let segmentData = makeSegment(type: 0x13, pageID: 1, payload: odsPayload)
        let segments = parser.parse(data: segmentData)

        #expect(segments.count == 1)
        guard case let .objectData(ods) = segments[0].payload else {
            Issue.record("Expected .objectData payload")
            return
        }

        #expect(ods.objectID == 100)
        #expect(ods.codingMethod == .reserved(2), "Reserved coding method 0x02 must NOT default to .pixels")
        #expect(ods.topFieldData.isEmpty)
        #expect(ods.bottomFieldData == nil)
    }

    @Test("Error Handling: Malformed known segments (0x10..0x14) produce .malformed, NOT .unknown")
    func malformedKnownSegmentsProduceMalformedPayload() {
        let parser = DVBSubtitleSegmentParser()
        let sink = MockSegmentParserSink()
        parser.delegate = sink

        var stream = Data()

        // 1. Truncated ODS (claims 50 bytes top data, only 4 provided)
        var odsPayload = Data()
        odsPayload.append(contentsOf: [0x00, 0x01, 0x10, 0x00, 0x32, 0x00, 0x00, 0x11, 0x22]) // only 2 bytes top data instead of 50
        stream.append(makeSegment(type: 0x13, pageID: 1, payload: odsPayload))

        // 2. Truncated RCS (only 5 bytes instead of >= 10 bytes)
        let rcsPayload = Data([0x01, 0x10, 0x00, 0x50, 0x00])
        stream.append(makeSegment(type: 0x11, pageID: 1, payload: rcsPayload))

        // 3. Truncated CLUT (only 1 byte header instead of >= 2 bytes)
        let clutPayload = Data([0x01])
        stream.append(makeSegment(type: 0x12, pageID: 1, payload: clutPayload))

        // 4. Truncated DDS (only 3 bytes instead of >= 5 bytes)
        let ddsPayload = Data([0x10, 0x07, 0x7F])
        stream.append(makeSegment(type: 0x14, pageID: 1, payload: ddsPayload))

        // 5. Truly unknown segment type 0x99
        let unknownPayload = Data([0x01, 0x02, 0x03, 0x04])
        stream.append(makeSegment(type: 0x99, pageID: 1, payload: unknownPayload))

        let segments = parser.parse(data: stream)
        #expect(segments.count == 5)

        // 1. ODS -> .malformed
        guard case let .malformed(type1, reason1, _) = segments[0].payload else {
            Issue.record("Expected .malformed for truncated ODS")
            return
        }
        #expect(type1 == 0x13)
        #expect(reason1.contains("ODS"))

        // 2. RCS -> .malformed
        guard case let .malformed(type2, reason2, _) = segments[1].payload else {
            Issue.record("Expected .malformed for truncated RCS")
            return
        }
        #expect(type2 == 0x11)
        #expect(reason2.contains("RCS"))

        // 3. CLUT -> .malformed
        guard case let .malformed(type3, reason3, _) = segments[2].payload else {
            Issue.record("Expected .malformed for truncated CLUT")
            return
        }
        #expect(type3 == 0x12)
        #expect(reason3.contains("CLUT"))

        // 4. DDS -> .malformed
        guard case let .malformed(type4, reason4, _) = segments[3].payload else {
            Issue.record("Expected .malformed for truncated DDS")
            return
        }
        #expect(type4 == 0x14)
        #expect(reason4.contains("DDS"))

        // 5. Truly unknown -> .unknown (NOT .malformed)
        guard case let .unknown(type5, data5) = segments[4].payload else {
            Issue.record("Expected .unknown for type 0x99")
            return
        }
        #expect(type5 == 0x99)
        #expect(data5 == unknownPayload)

        #expect(sink.warnings.count == 4)
    }

    @Test("Page ID Filtering: Retains composition and ancillary pages, discards foreign subtitle services")
    func pageIDFiltering() {
        let parser = DVBSubtitleSegmentParser()

        var stream = Data()
        // Page 1: Target Composition Page
        stream.append(makeSegment(type: 0x10, pageID: 1, payload: Data([10, 0x10])))
        // Page 2: Target Ancillary Page (shared CLUT)
        stream.append(makeSegment(type: 0x12, pageID: 2, payload: Data([0x01, 0x00])))
        // Page 99: Foreign Subtitle Service on same PID
        stream.append(makeSegment(type: 0x10, pageID: 99, payload: Data([10, 0x10])))

        let allSegments = parser.parse(data: stream)
        #expect(allSegments.count == 3)

        let filtered = parser.filter(segments: allSegments, compositionPageID: 1, ancillaryPageID: 2)
        #expect(filtered.count == 2)
        #expect(filtered[0].pageID == 1)
        #expect(filtered[1].pageID == 2)
        #expect(!filtered.contains(where: { $0.pageID == 99 }), "Foreign page ID 99 must be filtered out")
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
