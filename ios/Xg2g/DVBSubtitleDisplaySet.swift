// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// An object positioned within an active subtitle region.
public struct DVBActiveObject: Sendable, Equatable {
    public let objectID: UInt16
    public let objectType: UInt8 // 0x00: bitmap, 0x01: basic char, 0x02: composite string
    public let horizontalPosition: Int // Pixel X relative to region origin
    public let verticalPosition: Int   // Pixel Y relative to region origin
    public let objectData: DVBObjectDataSegment
    public let foregroundColorCode: UInt8?
    public let backgroundColorCode: UInt8?

    public init(
        objectID: UInt16,
        objectType: UInt8,
        horizontalPosition: Int,
        verticalPosition: Int,
        objectData: DVBObjectDataSegment,
        foregroundColorCode: UInt8? = nil,
        backgroundColorCode: UInt8? = nil
    ) {
        self.objectID = objectID
        self.objectType = objectType
        self.horizontalPosition = horizontalPosition
        self.verticalPosition = verticalPosition
        self.objectData = objectData
        self.foregroundColorCode = foregroundColorCode
        self.backgroundColorCode = backgroundColorCode
    }
}

/// An active subtitle region positioned on screen with its associated CLUT and objects.
public struct DVBActiveRegion: Sendable, Equatable {
    public let regionID: UInt8
    public let horizontalAddress: Int // Screen X coordinate
    public let verticalAddress: Int   // Screen Y coordinate
    public let width: Int
    public let height: Int
    public let depth: UInt8 // 1 = 2-bit (4 colors), 2 = 4-bit (16 colors), 3 = 8-bit (256 colors)
    public let clut: DVBCLUTDefinitionSegment?
    public let fillFlag: Bool
    public let pixelCode8Bit: UInt8
    public let pixelCode4Bit: UInt8
    public let pixelCode2Bit: UInt8
    public let objects: [DVBActiveObject]

    public init(
        regionID: UInt8,
        horizontalAddress: Int,
        verticalAddress: Int,
        width: Int,
        height: Int,
        depth: UInt8,
        clut: DVBCLUTDefinitionSegment?,
        fillFlag: Bool,
        pixelCode8Bit: UInt8,
        pixelCode4Bit: UInt8,
        pixelCode2Bit: UInt8,
        objects: [DVBActiveObject]
    ) {
        self.regionID = regionID
        self.horizontalAddress = horizontalAddress
        self.verticalAddress = verticalAddress
        self.width = width
        self.height = height
        self.depth = depth
        self.clut = clut
        self.fillFlag = fillFlag
        self.pixelCode8Bit = pixelCode8Bit
        self.pixelCode4Bit = pixelCode4Bit
        self.pixelCode2Bit = pixelCode2Bit
        self.objects = objects
    }
}

/// A fully resolved logical snapshot of the DVB Subtitle page at a specific presentation timestamp (PTS).
///
/// Contains display bounds, positioned active regions, resolved CLUT color tables, and raw ODS objects.
/// Used by downstream RLE rendering without holding mutable state.
public struct DVBSubtitlePageSnapshot: Sendable, Equatable {
    public let displayWidth: Int
    public let displayHeight: Int
    public let presentationPTS: CMTime?
    public let presentationPTS90k: UInt64?
    public let timeoutDeadlinePTS: CMTime?
    public let pageVersion: UInt8
    public let pageTimeOut: UInt8
    public let regions: [DVBActiveRegion]

    public var isVisible: Bool {
        !regions.isEmpty
    }

    public init(
        displayWidth: Int,
        displayHeight: Int,
        presentationPTS: CMTime?,
        presentationPTS90k: UInt64?,
        timeoutDeadlinePTS: CMTime?,
        pageVersion: UInt8,
        pageTimeOut: UInt8,
        regions: [DVBActiveRegion]
    ) {
        self.displayWidth = displayWidth
        self.displayHeight = displayHeight
        self.presentationPTS = presentationPTS
        self.presentationPTS90k = presentationPTS90k
        self.timeoutDeadlinePTS = timeoutDeadlinePTS
        self.pageVersion = pageVersion
        self.pageTimeOut = pageTimeOut
        self.regions = regions
    }

    /// Evaluates whether the subtitle page is currently visible at the given playback media PTS.
    ///
    /// - Parameter mediaPTS: Current video playback presentation timestamp.
    /// - Returns: `true` if `mediaPTS >= presentationPTS` and `mediaPTS < timeoutDeadlinePTS` (if timeout is set).
    public func isValid(at mediaPTS: CMTime) -> Bool {
        guard isVisible else { return false }
        if let pPTS = presentationPTS {
            if mediaPTS < pPTS { return false }
        }
        if let deadline = timeoutDeadlinePTS {
            return mediaPTS < deadline
        }
        return true
    }
}

/// An atomic set of filtered DVB subtitle segments parsed from a single PES packet, bound to its PTS.
public struct DVBSubtitleDisplaySet: Sendable, Equatable {
    public let pts90k: UInt64?
    public let pts: CMTime?
    public let subtitleStreamID: UInt8
    public let compositionPageID: UInt16
    public let ancillaryPageID: UInt16?

    public private(set) var displayDefinition: DVBDisplayDefinitionSegment?
    public private(set) var pageComposition: DVBPageCompositionSegment?
    public private(set) var regions: [UInt8: DVBRegionCompositionSegment] = [:]
    public private(set) var cluts: [UInt8: DVBCLUTDefinitionSegment] = [:]
    public private(set) var objects: [UInt16: DVBObjectDataSegment] = [:]
    public private(set) var hasEndOfDisplaySet: Bool = false

    public init(
        pesPacket: DVBSubtitlePESPacket,
        segments: [DVBSubtitleSegment],
        compositionPageID: UInt16,
        ancillaryPageID: UInt16? = nil
    ) {
        self.pts90k = pesPacket.pts90k
        self.pts = pesPacket.pts
        self.subtitleStreamID = pesPacket.subtitleStreamID
        self.compositionPageID = compositionPageID
        self.ancillaryPageID = ancillaryPageID

        for segment in segments {
            let isComposition = segment.pageID == compositionPageID
            let isAncillary = (ancillaryPageID != nil && segment.pageID == ancillaryPageID!)

            guard isComposition || isAncillary else {
                continue
            }

            switch segment.payload {
            case .displayDefinition(let dds):
                self.displayDefinition = dds
            case .pageComposition(let pcs):
                if isComposition {
                    self.pageComposition = pcs
                }
            case .regionComposition(let rcs):
                if isComposition {
                    self.regions[rcs.regionID] = rcs
                }
            case .clutDefinition(let clut):
                self.cluts[clut.clutID] = clut
            case .objectData(let ods):
                self.objects[ods.objectID] = ods
            case .endOfDisplaySet:
                self.hasEndOfDisplaySet = true
            case .malformed, .unknown:
                break
            }
        }
    }
}
