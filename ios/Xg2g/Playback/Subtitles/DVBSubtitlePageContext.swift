// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// Manages the DVB Subtitle page state machine across successive Display Sets (ETSI EN 300 743).
///
/// Implements standard DVB Subtitle Epoch and Page transitions:
/// - `.modeChange` (0x02): Starts a new epoch; flushes all cached regions, CLUTs, and objects.
/// - `.acquisitionPoint` (0x01): Full page refresh; replaces active components and purges stale unreferenced regions.
/// - `.normalCase` (0x00): Incremental update; merges new or modified components while retaining unchanged definitions.
/// - Empty PCS region list: Clears visible subtitle presentation at the associated PTS.
/// - Media-time timeout calculation: `timeoutDeadlinePTS = pts + pageTimeOut` (strictly presentation-time based).
public final class DVBSubtitlePageContext: @unchecked Sendable {

    public let compositionPageID: UInt16
    public let ancillaryPageID: UInt16?

    private var cachedDisplayDefinition: DVBDisplayDefinitionSegment?
    private var cachedPageComposition: DVBPageCompositionSegment?
    private var cachedRegions: [UInt8: DVBRegionCompositionSegment] = [:]
    private var cachedCLUTs: [UInt8: DVBCLUTDefinitionSegment] = [:]
    private var cachedObjects: [UInt16: DVBObjectDataSegment] = [:]

    private var currentPTS: CMTime?
    private var currentPTS90k: UInt64?
    private var currentTimeoutDeadlinePTS: CMTime?

    public init(compositionPageID: UInt16, ancillaryPageID: UInt16? = nil) {
        self.compositionPageID = compositionPageID
        self.ancillaryPageID = ancillaryPageID
    }

    /// Completely clears all cached epoch state (on channel zap, subtitle track switch, or continuity loss).
    public func reset() {
        cachedDisplayDefinition = nil
        cachedPageComposition = nil
        cachedRegions.removeAll(keepingCapacity: true)
        cachedCLUTs.removeAll(keepingCapacity: true)
        cachedObjects.removeAll(keepingCapacity: true)
        currentPTS = nil
        currentPTS90k = nil
        currentTimeoutDeadlinePTS = nil
    }

    /// Ingests a new Display Set and updates the state machine according to its `page_state`.
    ///
    /// - Parameter displaySet: The parsed display set.
    /// - Returns: A resolved `DVBSubtitlePageSnapshot` if a valid page composition exists.
    @discardableResult
    public func process(displaySet: DVBSubtitleDisplaySet) -> DVBSubtitlePageSnapshot? {
        // Update display definition if present in display set
        if let dds = displaySet.displayDefinition {
            self.cachedDisplayDefinition = dds
        }

        // If no PCS is present in this display set, update any new CLUTs/objects/regions (e.g. non-display set data)
        guard let pcs = displaySet.pageComposition else {
            for (id, clut) in displaySet.cluts { cachedCLUTs[id] = clut }
            for (id, obj) in displaySet.objects { cachedObjects[id] = obj }
            for (id, rcs) in displaySet.regions { cachedRegions[id] = rcs }
            return generateSnapshot()
        }

        self.currentPTS = displaySet.pts
        self.currentPTS90k = displaySet.pts90k

        // Calculate media-time timeout deadline (ETSI EN 300 743 Clause 5.2.2: 0 means no timeout)
        if pcs.pageTimeOut > 0, let pts = displaySet.pts {
            let timeoutDuration = CMTime(seconds: Double(pcs.pageTimeOut), preferredTimescale: 90000)
            self.currentTimeoutDeadlinePTS = pts + timeoutDuration
        } else {
            self.currentTimeoutDeadlinePTS = nil
        }

        switch pcs.state {
        case .modeChange:
            // 1. Mode Change: Start of a new Epoch (Clause 5.1) -> Complete memory wipe of old epoch
            self.cachedPageComposition = pcs
            self.cachedRegions = displaySet.regions
            self.cachedCLUTs = displaySet.cluts
            self.cachedObjects = displaySet.objects

        case .acquisitionPoint:
            // 2. Acquisition Point: Full refresh of the page (Clause 5.1 / 5.2.2)
            // Replaces active page composition, updates components, and purges stale unreferenced regions
            self.cachedPageComposition = pcs
            for (id, rcs) in displaySet.regions { cachedRegions[id] = rcs }
            for (id, clut) in displaySet.cluts { cachedCLUTs[id] = clut }
            for (id, obj) in displaySet.objects { cachedObjects[id] = obj }
            let referencedRegionIDs = Set(pcs.regions.map(\.regionID))
            cachedRegions = cachedRegions.filter { referencedRegionIDs.contains($0.key) }

        case .normalCase:
            // 3. Normal Case: Incremental update
            self.cachedPageComposition = pcs
            for (id, rcs) in displaySet.regions { cachedRegions[id] = rcs }
            for (id, clut) in displaySet.cluts { cachedCLUTs[id] = clut }
            for (id, obj) in displaySet.objects { cachedObjects[id] = obj }

        case .reserved:
            break
        }

        return generateSnapshot()
    }

    /// Generates a fully resolved snapshot of the active subtitle page.
    public func generateSnapshot() -> DVBSubtitlePageSnapshot? {
        guard let pcs = cachedPageComposition else { return nil }

        let displayW = cachedDisplayDefinition?.displayWidth ?? 720
        let displayH = cachedDisplayDefinition?.displayHeight ?? 576

        // If PCS has an empty region list, the page is intentionally blank/cleared
        if pcs.regions.isEmpty {
            return DVBSubtitlePageSnapshot(
                displayWidth: displayW,
                displayHeight: displayH,
                presentationPTS: currentPTS,
                presentationPTS90k: currentPTS90k,
                timeoutDeadlinePTS: currentTimeoutDeadlinePTS,
                pageVersion: pcs.versionNumber,
                pageTimeOut: pcs.pageTimeOut,
                regions: []
            )
        }

        var activeRegions: [DVBActiveRegion] = []

        for regionRef in pcs.regions {
            guard let rcs = cachedRegions[regionRef.regionID] else { continue }
            let clut = cachedCLUTs[rcs.clutID]

            var activeObjects: [DVBActiveObject] = []
            for objRef in rcs.objects {
                guard let ods = cachedObjects[objRef.objectID] else { continue }
                activeObjects.append(DVBActiveObject(
                    objectID: objRef.objectID,
                    objectType: objRef.objectType,
                    horizontalPosition: objRef.horizontalPosition,
                    verticalPosition: objRef.verticalPosition,
                    objectData: ods,
                    foregroundColorCode: objRef.foregroundColorCode,
                    backgroundColorCode: objRef.backgroundColorCode
                ))
            }

            activeRegions.append(DVBActiveRegion(
                regionID: rcs.regionID,
                horizontalAddress: regionRef.horizontalAddress,
                verticalAddress: regionRef.verticalAddress,
                width: rcs.width,
                height: rcs.height,
                depth: rcs.depth,
                clut: clut,
                fillFlag: rcs.fillFlag,
                pixelCode8Bit: rcs.pixelCode8Bit,
                pixelCode4Bit: rcs.pixelCode4Bit,
                pixelCode2Bit: rcs.pixelCode2Bit,
                objects: activeObjects
            ))
        }

        return DVBSubtitlePageSnapshot(
            displayWidth: displayW,
            displayHeight: displayH,
            presentationPTS: currentPTS,
            presentationPTS90k: currentPTS90k,
            timeoutDeadlinePTS: currentTimeoutDeadlinePTS,
            pageVersion: pcs.versionNumber,
            pageTimeOut: pcs.pageTimeOut,
            regions: activeRegions
        )
    }
}
