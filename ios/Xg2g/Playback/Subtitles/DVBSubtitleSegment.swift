// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// DVB Subtitling Segment Types according to ETSI EN 300 743 Table 2.
public enum DVBSegmentType: UInt8, Sendable, CustomStringConvertible {
    case pageComposition      = 0x10 // PCS
    case regionComposition    = 0x11 // RCS
    case clutDefinition       = 0x12 // CLUT
    case objectData           = 0x13 // ODS
    case displayDefinition    = 0x14 // DDS
    case endOfDisplaySet      = 0x80 // EoDS
    case stuffing             = 0xFF // Stuffing segment

    public var description: String {
        switch self {
        case .pageComposition: return "Page Composition (PCS 0x10)"
        case .regionComposition: return "Region Composition (RCS 0x11)"
        case .clutDefinition: return "CLUT Definition (CLUT 0x12)"
        case .objectData: return "Object Data (ODS 0x13)"
        case .displayDefinition: return "Display Definition (DDS 0x14)"
        case .endOfDisplaySet: return "End of Display Set (EoDS 0x80)"
        case .stuffing: return "Stuffing (0xFF)"
        }
    }
}

/// Page State signaled in Page Composition Segment (ETSI EN 300 743 Table 4).
public enum DVBPageState: UInt8, Sendable, CustomStringConvertible {
    case normalCase       = 0x00 // Update to current page (incremental)
    case acquisitionPoint = 0x01 // Refresh all page parameters (tune-in / recovery)
    case modeChange       = 0x02 // Clear screen & reset all page components
    case reserved         = 0x03

    public var description: String {
        switch self {
        case .normalCase: return "Normal Case (0x00)"
        case .acquisitionPoint: return "Acquisition Point (0x01)"
        case .modeChange: return "Mode Change (0x02)"
        case .reserved: return "Reserved (0x03)"
        }
    }
}

/// Display Definition Segment (DDS 0x14) - ETSI EN 300 743 Clause 5.2.1.
public struct DVBDisplayDefinitionSegment: Sendable, Equatable {
    public let versionNumber: UInt8
    public let displayWidth: Int // Display width in pixels (e.g. 720, 1920)
    public let displayHeight: Int // Display height in pixels (e.g. 576, 1080)
    public let windowHorizontalPositionMinimum: Int?
    public let windowHorizontalPositionMaximum: Int?
    public let windowVerticalPositionMinimum: Int?
    public let windowVerticalPositionMaximum: Int?

    public init(
        versionNumber: UInt8,
        displayWidth: Int,
        displayHeight: Int,
        windowHorizontalPositionMinimum: Int? = nil,
        windowHorizontalPositionMaximum: Int? = nil,
        windowVerticalPositionMinimum: Int? = nil,
        windowVerticalPositionMaximum: Int? = nil
    ) {
        self.versionNumber = versionNumber
        self.displayWidth = displayWidth
        self.displayHeight = displayHeight
        self.windowHorizontalPositionMinimum = windowHorizontalPositionMinimum
        self.windowHorizontalPositionMaximum = windowHorizontalPositionMaximum
        self.windowVerticalPositionMinimum = windowVerticalPositionMinimum
        self.windowVerticalPositionMaximum = windowVerticalPositionMaximum
    }
}

/// Region position reference within a Page Composition Segment.
public struct DVBPageRegionReference: Sendable, Equatable {
    public let regionID: UInt8
    public let horizontalAddress: Int
    public let verticalAddress: Int

    public init(regionID: UInt8, horizontalAddress: Int, verticalAddress: Int) {
        self.regionID = regionID
        self.horizontalAddress = horizontalAddress
        self.verticalAddress = verticalAddress
    }
}

/// Page Composition Segment (PCS 0x10) - ETSI EN 300 743 Clause 5.2.2.
public struct DVBPageCompositionSegment: Sendable, Equatable {
    public let pageTimeOut: UInt8 // Time out duration in seconds
    public let versionNumber: UInt8
    public let state: DVBPageState
    public let regions: [DVBPageRegionReference]

    public init(
        pageTimeOut: UInt8,
        versionNumber: UInt8,
        state: DVBPageState,
        regions: [DVBPageRegionReference]
    ) {
        self.pageTimeOut = pageTimeOut
        self.versionNumber = versionNumber
        self.state = state
        self.regions = regions
    }
}

/// Object reference positioned inside a Region Composition Segment.
public struct DVBRegionObjectReference: Sendable, Equatable {
    public let objectID: UInt16
    public let objectType: UInt8 // 0x00: bitmap, 0x01: basic char, 0x02: composite string
    public let objectProviderFlag: UInt8
    public let horizontalPosition: Int
    public let verticalPosition: Int
    public let foregroundColorCode: UInt8?
    public let backgroundColorCode: UInt8?

    public init(
        objectID: UInt16,
        objectType: UInt8,
        objectProviderFlag: UInt8,
        horizontalPosition: Int,
        verticalPosition: Int,
        foregroundColorCode: UInt8? = nil,
        backgroundColorCode: UInt8? = nil
    ) {
        self.objectID = objectID
        self.objectType = objectType
        self.objectProviderFlag = objectProviderFlag
        self.horizontalPosition = horizontalPosition
        self.verticalPosition = verticalPosition
        self.foregroundColorCode = foregroundColorCode
        self.backgroundColorCode = backgroundColorCode
    }
}

/// Region Composition Segment (RCS 0x11) - ETSI EN 300 743 Clause 5.2.3.
public struct DVBRegionCompositionSegment: Sendable, Equatable {
    public let regionID: UInt8
    public let versionNumber: UInt8
    public let fillFlag: Bool
    public let width: Int
    public let height: Int
    public let compatibilityLevel: UInt8
    public let depth: UInt8 // 1 = 2-bit (4 colors), 2 = 4-bit (16 colors), 3 = 8-bit (256 colors)
    public let clutID: UInt8
    public let pixelCode8Bit: UInt8
    public let pixelCode4Bit: UInt8
    public let pixelCode2Bit: UInt8
    public let objects: [DVBRegionObjectReference]

    public init(
        regionID: UInt8,
        versionNumber: UInt8,
        fillFlag: Bool,
        width: Int,
        height: Int,
        compatibilityLevel: UInt8,
        depth: UInt8,
        clutID: UInt8,
        pixelCode8Bit: UInt8,
        pixelCode4Bit: UInt8,
        pixelCode2Bit: UInt8,
        objects: [DVBRegionObjectReference]
    ) {
        self.regionID = regionID
        self.versionNumber = versionNumber
        self.fillFlag = fillFlag
        self.width = width
        self.height = height
        self.compatibilityLevel = compatibilityLevel
        self.depth = depth
        self.clutID = clutID
        self.pixelCode8Bit = pixelCode8Bit
        self.pixelCode4Bit = pixelCode4Bit
        self.pixelCode2Bit = pixelCode2Bit
        self.objects = objects
    }
}

/// Color entry in a CLUT Definition Segment (Y, Cr, Cb, T transparency).
///
/// In DVB Subtitling (ETSI EN 300 743 Clause 5.2.4 Table 6):
/// - `t` is the transparency value: `0` = fully opaque (no transparency), `255` = fully transparent.
/// - Special case: `y == 0` corresponds to full transparency (100% transparent, `t = 255`).
public struct DVBCLUTEntry: Sendable, Equatable {
    public let entryID: UInt8
    public let entry2BitCLUTFlag: Bool
    public let entry4BitCLUTFlag: Bool
    public let entry8BitCLUTFlag: Bool
    public let y: UInt8
    public let cr: UInt8
    public let cb: UInt8
    public let t: UInt8 // Transparency: 0 = fully opaque (no transparency), 255 = fully transparent

    /// Indicates whether this entry represents 100% transparent pixels (Y == 0 or T == 255).
    public var isFullyTransparent: Bool {
        y == 0 || t == 255
    }

    public init(
        entryID: UInt8,
        entry2BitCLUTFlag: Bool,
        entry4BitCLUTFlag: Bool,
        entry8BitCLUTFlag: Bool,
        y: UInt8,
        cr: UInt8,
        cb: UInt8,
        t: UInt8
    ) {
        self.entryID = entryID
        self.entry2BitCLUTFlag = entry2BitCLUTFlag
        self.entry4BitCLUTFlag = entry4BitCLUTFlag
        self.entry8BitCLUTFlag = entry8BitCLUTFlag
        self.y = y
        self.cr = cr
        self.cb = cb
        // ETSI EN 300 743 Clause 5.2.4: Y=0 corresponds to full transparency (T=255)
        if y == 0 {
            self.t = 255
        } else {
            self.t = t
        }
    }
}

/// CLUT Definition Segment (CLUT 0x12) - ETSI EN 300 743 Clause 5.2.4.
public struct DVBCLUTDefinitionSegment: Sendable, Equatable {
    public let clutID: UInt8
    public let versionNumber: UInt8
    public let entries: [DVBCLUTEntry]

    public init(clutID: UInt8, versionNumber: UInt8, entries: [DVBCLUTEntry]) {
        self.clutID = clutID
        self.versionNumber = versionNumber
        self.entries = entries
    }
}

/// Object Coding Method in Object Data Segment (ETSI EN 300 743 Table 7).
public enum DVBObjectCodingMethod: Sendable, Equatable, CustomStringConvertible {
    case pixels            // 0x00: Bitmap RLE bitstreams
    case string            // 0x01: Character string
    case reserved(UInt8)   // 0x02, 0x03: Reserved / unknown coding methods

    public var description: String {
        switch self {
        case .pixels: return "Pixels (Bitmap RLE 0x00)"
        case .string: return "String (0x01)"
        case .reserved(let val): return "Reserved (0x\(String(format: "%02X", val)))"
        }
    }
}

/// Object Data Segment (ODS 0x13) - ETSI EN 300 743 Clause 5.2.5.
public struct DVBObjectDataSegment: Sendable, Equatable {
    public let objectID: UInt16
    public let versionNumber: UInt8
    public let codingMethod: DVBObjectCodingMethod
    public let nonModifyingColorFlag: Bool
    public let topFieldData: Data
    public let bottomFieldData: Data?
    public let characterCodes: Data?

    public init(
        objectID: UInt16,
        versionNumber: UInt8,
        codingMethod: DVBObjectCodingMethod,
        nonModifyingColorFlag: Bool,
        topFieldData: Data = Data(),
        bottomFieldData: Data? = nil,
        characterCodes: Data? = nil
    ) {
        self.objectID = objectID
        self.versionNumber = versionNumber
        self.codingMethod = codingMethod
        self.nonModifyingColorFlag = nonModifyingColorFlag
        self.topFieldData = topFieldData
        self.bottomFieldData = bottomFieldData
        self.characterCodes = characterCodes
    }
}

/// Strongly-typed DVB Subtitle Segment Payload.
public enum DVBSegmentPayload: Sendable, Equatable {
    case displayDefinition(DVBDisplayDefinitionSegment)
    case pageComposition(DVBPageCompositionSegment)
    case regionComposition(DVBRegionCompositionSegment)
    case clutDefinition(DVBCLUTDefinitionSegment)
    case objectData(DVBObjectDataSegment)
    case endOfDisplaySet
    case malformed(type: UInt8, reason: String, data: Data)
    case unknown(type: UInt8, data: Data)
}

/// A parsed DVB Subtitle Segment with header metadata.
public struct DVBSubtitleSegment: Sendable, Equatable {
    public let type: UInt8
    public let pageID: UInt16
    public let length: UInt16
    public let payload: DVBSegmentPayload

    public init(type: UInt8, pageID: UInt16, length: UInt16, payload: DVBSegmentPayload) {
        self.type = type
        self.pageID = pageID
        self.length = length
        self.payload = payload
    }
}
