// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

public protocol DVBSubtitleSegmentParserDelegate: AnyObject, Sendable {
    func dvbSegmentParser(_ parser: DVBSubtitleSegmentParser, didParseSegment segment: DVBSubtitleSegment)
    func dvbSegmentParser(_ parser: DVBSubtitleSegmentParser, didEncounterWarning warning: String)
}

/// Parses raw DVB Subtitle elementary stream bytes into strongly-typed DVB Subtitle Segments (ETSI EN 300 743).
///
/// Features & Invariants:
/// - Validates sync byte 0x0F for every segment.
/// - Bounds-checks segment length and payload structures without crashing on truncated or malformed inputs.
/// - Skips unknown / future segment types gracefully.
/// - Decodes all standard DVB segments (DDS 0x14, PCS 0x10, RCS 0x11, CLUT 0x12, ODS 0x13, EoDS 0x80).
/// - Operates purely on binary segment structures without bitmap RLE decompression or rasterization.
public final class DVBSubtitleSegmentParser: @unchecked Sendable {

    public weak var delegate: DVBSubtitleSegmentParserDelegate?

    public init() {}

    /// Parses a sequence of DVB Subtitle Segments contained in a single DVB PES payload.
    ///
    /// - Parameter data: Raw DVB Subtitle segment stream (stripped of PES header and 0xFF end marker).
    /// - Returns: Array of parsed `DVBSubtitleSegment` objects.
    @discardableResult
    public func parse(data: Data) -> [DVBSubtitleSegment] {
        var segments: [DVBSubtitleSegment] = []
        var offset = 0

        while offset + 6 <= data.count {
            // 1. Sync byte must be 0x0F
            let syncByte = data[offset]
            guard syncByte == 0x0F else {
                delegate?.dvbSegmentParser(self, didEncounterWarning: "Invalid sync_byte 0x\(String(format: "%02X", syncByte)) at offset \(offset) (expected 0x0F)")
                break
            }

            let segmentType = data[offset + 1]
            let pageID = (UInt16(data[offset + 2]) << 8) | UInt16(data[offset + 3])
            let segmentLength = (UInt16(data[offset + 4]) << 8) | UInt16(data[offset + 5])

            let payloadStart = offset + 6
            let payloadEnd = payloadStart + Int(segmentLength)

            guard payloadEnd <= data.count else {
                delegate?.dvbSegmentParser(self, didEncounterWarning: "Truncated segment 0x\(String(format: "%02X", segmentType)) at offset \(offset): expected \(segmentLength) bytes, available \(data.count - payloadStart)")
                break
            }

            let segmentData = data.subdata(in: payloadStart..<payloadEnd)
            let payload = parseSegmentPayload(type: segmentType, data: segmentData)

            let segment = DVBSubtitleSegment(
                type: segmentType,
                pageID: pageID,
                length: segmentLength,
                payload: payload
            )
            segments.append(segment)
            delegate?.dvbSegmentParser(self, didParseSegment: segment)

            offset = payloadEnd
        }

        return segments
    }

    private func parseSegmentPayload(type: UInt8, data: Data) -> DVBSegmentPayload {
        switch type {
        case DVBSegmentType.displayDefinition.rawValue:
            if let dds = parseDisplayDefinition(data: data) {
                return .displayDefinition(dds)
            }
        case DVBSegmentType.pageComposition.rawValue:
            if let pcs = parsePageComposition(data: data) {
                return .pageComposition(pcs)
            }
        case DVBSegmentType.regionComposition.rawValue:
            if let rcs = parseRegionComposition(data: data) {
                return .regionComposition(rcs)
            }
        case DVBSegmentType.clutDefinition.rawValue:
            if let clut = parseCLUTDefinition(data: data) {
                return .clutDefinition(clut)
            }
        case DVBSegmentType.objectData.rawValue:
            if let ods = parseObjectData(data: data) {
                return .objectData(ods)
            }
        case DVBSegmentType.endOfDisplaySet.rawValue:
            return .endOfDisplaySet
        default:
            break
        }

        return .unknown(type: type, data: data)
    }

    // MARK: - Segment-Specific Parsers

    private func parseDisplayDefinition(data: Data) -> DVBDisplayDefinitionSegment? {
        guard data.count >= 5 else { return nil }

        let version = (data[0] >> 4) & 0x0F
        let displayWindowFlag = (data[0] & 0x08) != 0

        // Display width & height specify max pixel coordinates (0-indexed max, so +1 gives dimension)
        let displayWidth = (Int(data[1]) << 8) | Int(data[2]) + 1
        let displayHeight = (Int(data[3]) << 8) | Int(data[4]) + 1

        var minX: Int? = nil
        var maxX: Int? = nil
        var minY: Int? = nil
        var maxY: Int? = nil

        if displayWindowFlag && data.count >= 13 {
            minX = (Int(data[5]) << 8) | Int(data[6])
            maxX = (Int(data[7]) << 8) | Int(data[8])
            minY = (Int(data[9]) << 8) | Int(data[10])
            maxY = (Int(data[11]) << 8) | Int(data[12])
        }

        return DVBDisplayDefinitionSegment(
            versionNumber: version,
            displayWidth: displayWidth,
            displayHeight: displayHeight,
            windowHorizontalPositionMinimum: minX,
            windowHorizontalPositionMaximum: maxX,
            windowVerticalPositionMinimum: minY,
            windowVerticalPositionMaximum: maxY
        )
    }

    private func parsePageComposition(data: Data) -> DVBPageCompositionSegment? {
        guard data.count >= 2 else { return nil }

        let pageTimeOut = data[0]
        let version = (data[1] >> 4) & 0x0F
        let pageStateRaw = (data[1] >> 2) & 0x03
        let state = DVBPageState(rawValue: pageStateRaw) ?? .normalCase

        var regions: [DVBPageRegionReference] = []
        var pos = 2

        while pos + 6 <= data.count {
            let regionID = data[pos]
            // data[pos+1] is reserved
            let hAddr = (Int(data[pos + 2]) << 8) | Int(data[pos + 3])
            let vAddr = (Int(data[pos + 4]) << 8) | Int(data[pos + 5])

            regions.append(DVBPageRegionReference(
                regionID: regionID,
                horizontalAddress: hAddr,
                verticalAddress: vAddr
            ))
            pos += 6
        }

        return DVBPageCompositionSegment(
            pageTimeOut: pageTimeOut,
            versionNumber: version,
            state: state,
            regions: regions
        )
    }

    private func parseRegionComposition(data: Data) -> DVBRegionCompositionSegment? {
        guard data.count >= 10 else { return nil }

        let regionID = data[0]
        let version = (data[1] >> 4) & 0x0F
        let fillFlag = (data[1] & 0x08) != 0
        let width = (Int(data[2]) << 8) | Int(data[3])
        let height = (Int(data[4]) << 8) | Int(data[5])
        let compat = (data[6] >> 5) & 0x07
        let depth = (data[6] >> 2) & 0x07
        let clutID = data[7]
        let pixel8 = data[8]
        let pixel4 = (data[9] >> 4) & 0x0F
        let pixel2 = (data[9] >> 2) & 0x03

        var objects: [DVBRegionObjectReference] = []
        var pos = 10

        while pos + 6 <= data.count {
            let objectID = (UInt16(data[pos]) << 8) | UInt16(data[pos + 1])
            let objectType = (data[pos + 2] >> 6) & 0x03
            let provider = (data[pos + 2] >> 4) & 0x03
            let hPos = (Int(data[pos + 2] & 0x0F) << 8) | Int(data[pos + 3])
            let vPos = (Int(data[pos + 4] & 0x0F) << 8) | Int(data[pos + 5])

            pos += 6

            var fgCode: UInt8? = nil
            var bgCode: UInt8? = nil

            if (objectType == 0x01 || objectType == 0x02) && pos + 2 <= data.count {
                fgCode = data[pos]
                bgCode = data[pos + 1]
                pos += 2
            }

            objects.append(DVBRegionObjectReference(
                objectID: objectID,
                objectType: objectType,
                objectProviderFlag: provider,
                horizontalPosition: hPos,
                verticalPosition: vPos,
                foregroundColorCode: fgCode,
                backgroundColorCode: bgCode
            ))
        }

        return DVBRegionCompositionSegment(
            regionID: regionID,
            versionNumber: version,
            fillFlag: fillFlag,
            width: width,
            height: height,
            compatibilityLevel: compat,
            depth: depth,
            clutID: clutID,
            pixelCode8Bit: pixel8,
            pixelCode4Bit: pixel4,
            pixelCode2Bit: pixel2,
            objects: objects
        )
    }

    private func parseCLUTDefinition(data: Data) -> DVBCLUTDefinitionSegment? {
        guard data.count >= 2 else { return nil }

        let clutID = data[0]
        let version = (data[1] >> 4) & 0x0F

        var entries: [DVBCLUTEntry] = []
        var pos = 2

        while pos + 2 <= data.count {
            let entryID = data[pos]
            let flags = data[pos + 1]

            let flag2 = (flags & 0x80) != 0
            let flag4 = (flags & 0x40) != 0
            let flag8 = (flags & 0x20) != 0
            let fullRange = (flags & 0x01) != 0

            let y: UInt8
            let cr: UInt8
            let cb: UInt8
            let t: UInt8

            if fullRange {
                guard pos + 6 <= data.count else { break }
                y = data[pos + 2]
                cr = data[pos + 3]
                cb = data[pos + 4]
                t = data[pos + 5]
                pos += 6
            } else {
                guard pos + 4 <= data.count else { break }
                let rawY = (data[pos + 2] >> 2) & 0x3F
                let rawCr = ((data[pos + 2] & 0x03) << 2) | ((data[pos + 3] >> 6) & 0x03)
                let rawCb = (data[pos + 3] >> 2) & 0x0F
                let rawT = data[pos + 3] & 0x03

                // Scale compact bit representations to standard 8-bit color range
                y = (rawY << 2) | (rawY >> 4)
                cr = (rawCr << 4) | rawCr
                cb = (rawCb << 4) | rawCb
                t = rawT == 0 ? 0 : (rawT == 3 ? 255 : rawT * 85)
                pos += 4
            }

            entries.append(DVBCLUTEntry(
                entryID: entryID,
                entry2BitCLUTFlag: flag2,
                entry4BitCLUTFlag: flag4,
                entry8BitCLUTFlag: flag8,
                y: y,
                cr: cr,
                cb: cb,
                t: t
            ))
        }

        return DVBCLUTDefinitionSegment(
            clutID: clutID,
            versionNumber: version,
            entries: entries
        )
    }

    private func parseObjectData(data: Data) -> DVBObjectDataSegment? {
        guard data.count >= 3 else { return nil }

        let objectID = (UInt16(data[0]) << 8) | UInt16(data[1])
        let version = (data[2] >> 4) & 0x0F
        let codingMethodRaw = (data[2] >> 2) & 0x03
        let codingMethod = DVBObjectCodingMethod(rawValue: codingMethodRaw) ?? .pixels
        let nonModifying = (data[2] & 0x02) != 0

        var topData = Data()
        var bottomData: Data? = nil
        var characterCodes: Data? = nil

        if codingMethod == .pixels {
            guard data.count >= 7 else { return nil }
            let topLen = (Int(data[3]) << 8) | Int(data[4])
            let bottomLen = (Int(data[5]) << 8) | Int(data[6])

            guard 7 + topLen + bottomLen <= data.count else { return nil }

            topData = data.subdata(in: 7..<(7 + topLen))
            if bottomLen > 0 {
                bottomData = data.subdata(in: (7 + topLen)..<(7 + topLen + bottomLen))
            }
        } else if codingMethod == .string {
            guard data.count >= 4 else { return nil }
            let count = Int(data[3])
            guard 4 + count <= data.count else { return nil }
            characterCodes = data.subdata(in: 4..<(4 + count))
        }

        return DVBObjectDataSegment(
            objectID: objectID,
            versionNumber: version,
            codingMethod: codingMethod,
            nonModifyingColorFlag: nonModifying,
            topFieldData: topData,
            bottomFieldData: bottomData,
            characterCodes: characterCodes
        )
    }
}
