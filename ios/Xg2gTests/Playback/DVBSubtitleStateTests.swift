// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct DVBSubtitleStateTests {

    @Test("1. Mode Change: Complete epoch reset flushes all stale regions, CLUTs, and objects")
    func modeChangeFlushesAllPreviousComponents() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Initial Display Set in Epoch 1 with Region 1, CLUT 1, Object 10
        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1)],
            objects: [makeODS(objectID: 10)]
        )
        let snap1 = context.process(displaySet: ds1)!
        #expect(snap1.regions.count == 1)
        #expect(snap1.regions[0].regionID == 1)
        #expect(snap1.regions[0].objects[0].objectID == 10)

        // Epoch 2 starts with Mode Change: Introduces Region 2, CLUT 2, Object 20 (NO Region 1 / Object 10)
        let ds2 = makeDisplaySet(
            ptsSeconds: 20.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 2, clutID: 2, objectIDs: [20])],
            cluts: [makeCLUT(clutID: 2)],
            objects: [makeODS(objectID: 20)]
        )
        let snap2 = context.process(displaySet: ds2)!

        #expect(snap2.regions.count == 1)
        #expect(snap2.regions[0].regionID == 2)
        #expect(snap2.regions[0].clut?.clutID == 2)
        #expect(snap2.regions[0].objects[0].objectID == 20)

        // Verify Region 1 / Object 10 / CLUT 1 are completely gone
        #expect(!snap2.regions.contains(where: { $0.regionID == 1 }))
    }

    @Test("2. Acquisition Point: Replaces page components and purges stale unreferenced regions")
    func acquisitionPointReplacesAndPurgesStaleRegions() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Normal Case: 2 regions (Region 1 and Region 2)
        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [
                makeRCS(regionID: 1, clutID: 1, objectIDs: [10]),
                makeRCS(regionID: 2, clutID: 1, objectIDs: [20])
            ],
            cluts: [makeCLUT(clutID: 1)],
            objects: [makeODS(objectID: 10), makeODS(objectID: 20)]
        )
        context.process(displaySet: ds1)

        // Acquisition Point arrives containing ONLY Region 2
        let ds2 = makeDisplaySet(
            ptsSeconds: 15.0,
            pageState: .acquisitionPoint,
            pageVersion: 2,
            regions: [makeRCS(regionID: 2, clutID: 1, objectIDs: [20])],
            cluts: [],
            objects: []
        )
        let snap2 = context.process(displaySet: ds2)!

        #expect(snap2.regions.count == 1)
        #expect(snap2.regions[0].regionID == 2)
        #expect(!snap2.regions.contains(where: { $0.regionID == 1 }), "Unreferenced Region 1 must be purged on acquisition point")
    }

    @Test("3. Normal Update: Modifies only specified components while preserving unchanged ones")
    func normalCaseIncrementallyUpdatesOnlySpecifiedComponents() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Initial setup with Region 1 (Object 10) and CLUT 1
        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1, y: 100)],
            objects: [makeODS(objectID: 10)]
        )
        context.process(displaySet: ds1)

        // Normal Case update: only CLUT 1 is updated (Y changes to 200), RCS and ODS are NOT re-sent
        let ds2 = makeDisplaySet(
            ptsSeconds: 12.0,
            pageState: .normalCase,
            pageVersion: 2,
            regions: [], // PCS still references Region 1, but RCS definition is not re-transmitted
            cluts: [makeCLUT(clutID: 1, y: 200)],
            objects: []
        )
        let snap2 = context.process(displaySet: ds2)!

        #expect(snap2.regions.count == 1)
        #expect(snap2.regions[0].regionID == 1)
        #expect(snap2.regions[0].clut?.entries[0].y == 200, "CLUT 1 must have updated Y value")
        #expect(snap2.regions[0].objects[0].objectID == 10, "Cached Object 10 must be preserved")
    }

    @Test("4. Empty PCS: Clears the visible page snapshot at the specified PTS")
    func emptyPCSRegionsClearsVisibleSnapshot() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1)],
            objects: [makeODS(objectID: 10)]
        )
        let snap1 = context.process(displaySet: ds1)!
        #expect(snap1.isVisible == true)

        // Display Set with empty PCS regions (subtitle cleared)
        let ds2 = makeDisplaySet(
            ptsSeconds: 15.0,
            pageState: .normalCase,
            pageVersion: 2,
            regions: [],
            cluts: [],
            objects: [],
            pcsRegionIDs: [] // empty region list
        )
        let snap2 = context.process(displaySet: ds2)!

        #expect(snap2.isVisible == false)
        #expect(snap2.regions.isEmpty)
        #expect(snap2.presentationPTS?.seconds == 15.0)
    }

    @Test("5. Versioning: Successive region versions update region parameters")
    func twoConsecutiveRegionVersions() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Region 1 Version 1: Width 720, Height 50
        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, version: 1, width: 720, height: 50, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1)],
            objects: [makeODS(objectID: 10)]
        )
        let snap1 = context.process(displaySet: ds1)!
        #expect(snap1.regions[0].height == 50)

        // Region 1 Version 2: Width 720, Height 100
        let ds2 = makeDisplaySet(
            ptsSeconds: 12.0,
            pageState: .normalCase,
            pageVersion: 2,
            regions: [makeRCS(regionID: 1, version: 2, width: 720, height: 100, clutID: 1, objectIDs: [10])],
            cluts: [],
            objects: []
        )
        let snap2 = context.process(displaySet: ds2)!
        #expect(snap2.regions[0].height == 100, "Region height must update with new version")
    }

    @Test("6. Ancillary Page: Ancillary CLUT resolves correctly with Composition Page regions")
    func ancillaryCLUTResolvesWithCompositionPage() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 2)

        // PCS and RCS are on Composition Page (1); CLUT 1 is on Ancillary Page (2)
        let pcs = DVBPageCompositionSegment(pageTimeOut: 10, versionNumber: 1, state: .modeChange, regions: [
            DVBPageRegionReference(regionID: 1, horizontalAddress: 100, verticalAddress: 800)
        ])
        let rcs = makeRCS(regionID: 1, clutID: 1, objectIDs: [10])
        let clut = makeCLUT(clutID: 1, y: 180)
        let ods = makeODS(objectID: 10)

        let segments = [
            DVBSubtitleSegment(type: 0x10, pageID: 1, length: 10, payload: .pageComposition(pcs)),
            DVBSubtitleSegment(type: 0x11, pageID: 1, length: 10, payload: .regionComposition(rcs)),
            DVBSubtitleSegment(type: 0x12, pageID: 2, length: 10, payload: .clutDefinition(clut)), // Ancillary Page 2
            DVBSubtitleSegment(type: 0x13, pageID: 1, length: 10, payload: .objectData(ods)),
            DVBSubtitleSegment(type: 0x80, pageID: 1, length: 0, payload: .endOfDisplaySet)
        ]

        let pes = DVBSubtitlePESPacket(pts90k: 900000, payload: Data())
        let ds = DVBSubtitleDisplaySet(pesPacket: pes, segments: segments, compositionPageID: 1, ancillaryPageID: 2)
        let snap = context.process(displaySet: ds)!

        #expect(snap.regions.count == 1)
        #expect(snap.regions[0].clut?.clutID == 1)
        #expect(snap.regions[0].clut?.entries[0].y == 180)
    }

    @Test("7. Page ID Isolation: Segments with foreign page IDs are completely ignored")
    func foreignPageIDIsCompletelyIgnored() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Foreign segment on Page 99
        let foreignCLUT = makeCLUT(clutID: 99, y: 50)
        let segments = [
            DVBSubtitleSegment(type: 0x12, pageID: 99, length: 10, payload: .clutDefinition(foreignCLUT))
        ]

        let pes = DVBSubtitlePESPacket(pts90k: 900000, payload: Data())
        let ds = DVBSubtitleDisplaySet(pesPacket: pes, segments: segments, compositionPageID: 1, ancillaryPageID: 1)
        context.process(displaySet: ds)

        #expect(ds.cluts.isEmpty, "Foreign CLUT on page 99 must not enter DisplaySet")
    }

    @Test("8. Media-Time Timeout: Snapshot validity before, on, and after deadline")
    func timeoutBoundaryExactMatch() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // PTS = 10.0s, Timeout = 5s -> Deadline = 15.0s
        let ds = makeDisplaySet(
            ptsSeconds: 10.0,
            pageTimeOut: 5,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1)],
            objects: [makeODS(objectID: 10)]
        )
        let snap = context.process(displaySet: ds)!

        #expect(snap.presentationPTS?.seconds == 10.0)
        #expect(snap.timeoutDeadlinePTS?.seconds == 15.0)

        // Test before presentation PTS (< 10.0s)
        #expect(!snap.isValid(at: CMTime(seconds: 9.9, preferredTimescale: 90000)))

        // Test at start of presentation (10.0s)
        #expect(snap.isValid(at: CMTime(seconds: 10.0, preferredTimescale: 90000)))

        // Test mid-duration (12.5s)
        #expect(snap.isValid(at: CMTime(seconds: 12.5, preferredTimescale: 90000)))

        // Test just before deadline (14.99s)
        #expect(snap.isValid(at: CMTime(seconds: 14.99, preferredTimescale: 90000)))

        // Test exactly on deadline (15.0s) -> Expired
        #expect(!snap.isValid(at: CMTime(seconds: 15.0, preferredTimescale: 90000)))

        // Test after deadline (16.0s) -> Expired
        #expect(!snap.isValid(at: CMTime(seconds: 16.0, preferredTimescale: 90000)))
    }

    @Test("9. Reset / Zap: Completely flushes all cached components and epoch context")
    func resetClearsAllCachedComponents() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        let ds = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1)],
            objects: [makeODS(objectID: 10)]
        )
        context.process(displaySet: ds)
        #expect(context.generateSnapshot() != nil)

        // Reset
        context.reset()
        #expect(context.generateSnapshot() == nil)
    }

    @Test("10. Malformed Segment: Does NOT overwrite or corrupt valid existing state")
    func malformedSegmentDoesNotCorruptValidExistingState() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Valid setup
        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, clutID: 1, objectIDs: [10])],
            cluts: [makeCLUT(clutID: 1, y: 150)],
            objects: [makeODS(objectID: 10)]
        )
        context.process(displaySet: ds1)

        // Next DisplaySet contains a malformed CLUT segment (type 0x12, payload .malformed)
        let malformedCLUTSegment = DVBSubtitleSegment(
            type: 0x12,
            pageID: 1,
            length: 5,
            payload: .malformed(type: 0x12, reason: "Truncated", data: Data([0x01]))
        )
        let pes = DVBSubtitlePESPacket(pts90k: 990000, payload: Data())
        let ds2 = DVBSubtitleDisplaySet(
            pesPacket: pes,
            segments: [malformedCLUTSegment],
            compositionPageID: 1,
            ancillaryPageID: 1
        )
        let snap2 = context.process(displaySet: ds2)!

        #expect(snap2.regions.count == 1)
        #expect(snap2.regions[0].clut?.entries[0].y == 150, "Existing valid CLUT 1 must remain intact")
    }

    @Test("Live Sequence Test: Mode Change -> Normal Update -> CLUT Update -> Object Update -> Empty Clear")
    func liveMultiDisplaySetSequence() {
        let context = DVBSubtitlePageContext(compositionPageID: 1, ancillaryPageID: 1)

        // Step 1: Mode Change (Epoch start) at PTS 10.0s: "Hello World" (Object 100 in Region 1 with CLUT 1)
        let ds1 = makeDisplaySet(
            ptsSeconds: 10.0,
            pageTimeOut: 10,
            pageState: .modeChange,
            pageVersion: 1,
            regions: [makeRCS(regionID: 1, version: 1, width: 600, height: 60, clutID: 1, objectIDs: [100])],
            cluts: [makeCLUT(clutID: 1, y: 200)],
            objects: [makeODS(objectID: 100, topData: Data([0x01, 0x02]))]
        )
        let snap1 = context.process(displaySet: ds1)!
        #expect(snap1.isVisible == true)
        #expect(snap1.pageVersion == 1)
        #expect(snap1.regions[0].objects[0].objectID == 100)
        #expect(snap1.regions[0].objects[0].objectData.topFieldData == Data([0x01, 0x02]))

        // Step 2: Normal Update at PTS 13.0s: Change text to "Next Subtitle Line" (Object 101 in Region 1)
        let ds2 = makeDisplaySet(
            ptsSeconds: 13.0,
            pageTimeOut: 10,
            pageState: .normalCase,
            pageVersion: 2,
            regions: [makeRCS(regionID: 1, version: 2, width: 600, height: 60, clutID: 1, objectIDs: [101])],
            cluts: [],
            objects: [makeODS(objectID: 101, topData: Data([0x03, 0x04]))]
        )
        let snap2 = context.process(displaySet: ds2)!
        #expect(snap2.isVisible == true)
        #expect(snap2.pageVersion == 2)
        #expect(snap2.regions[0].objects[0].objectID == 101)
        #expect(snap2.regions[0].clut?.entries[0].y == 200, "CLUT 1 preserved from step 1")

        // Step 3: CLUT Update at PTS 15.0s: Color change on CLUT 1 (Y=240, Yellow)
        let ds3 = makeDisplaySet(
            ptsSeconds: 15.0,
            pageTimeOut: 10,
            pageState: .normalCase,
            pageVersion: 3,
            regions: [],
            cluts: [makeCLUT(clutID: 1, y: 240)],
            objects: []
        )
        let snap3 = context.process(displaySet: ds3)!
        #expect(snap3.regions[0].clut?.entries[0].y == 240)
        #expect(snap3.regions[0].objects[0].objectID == 101)

        // Step 4: Empty PCS at PTS 18.0s: Broadcaster clears subtitle
        let ds4 = makeDisplaySet(
            ptsSeconds: 18.0,
            pageTimeOut: 10,
            pageState: .normalCase,
            pageVersion: 4,
            regions: [],
            cluts: [],
            objects: [],
            pcsRegionIDs: []
        )
        let snap4 = context.process(displaySet: ds4)!
        #expect(snap4.isVisible == false)
        #expect(snap4.regions.isEmpty)
        #expect(snap4.presentationPTS?.seconds == 18.0)
    }
}

// MARK: - Test Helpers

private func makeDisplaySet(
    ptsSeconds: Double,
    pageTimeOut: UInt8 = 10,
    pageState: DVBPageState,
    pageVersion: UInt8,
    regions: [DVBRegionCompositionSegment],
    cluts: [DVBCLUTDefinitionSegment],
    objects: [DVBObjectDataSegment],
    pcsRegionIDs: [UInt8]? = nil
) -> DVBSubtitleDisplaySet {
    let pts90k = UInt64(ptsSeconds * 90000)
    let pts = CMTime(value: CMTimeValue(pts90k), timescale: 90000)
    let pes = DVBSubtitlePESPacket(pts90k: pts90k, pts: pts, payload: Data())

    let regionIDs = pcsRegionIDs ?? (regions.isEmpty ? [1] : regions.map(\.regionID))
    let regionRefs = regionIDs.map { DVBPageRegionReference(regionID: $0, horizontalAddress: 100, verticalAddress: 800) }

    let pcs = DVBPageCompositionSegment(
        pageTimeOut: pageTimeOut,
        versionNumber: pageVersion,
        state: pageState,
        regions: regionRefs
    )

    var segments: [DVBSubtitleSegment] = []
    segments.append(DVBSubtitleSegment(type: 0x10, pageID: 1, length: 10, payload: .pageComposition(pcs)))

    for rcs in regions {
        segments.append(DVBSubtitleSegment(type: 0x11, pageID: 1, length: 10, payload: .regionComposition(rcs)))
    }
    for clut in cluts {
        segments.append(DVBSubtitleSegment(type: 0x12, pageID: 1, length: 10, payload: .clutDefinition(clut)))
    }
    for ods in objects {
        segments.append(DVBSubtitleSegment(type: 0x13, pageID: 1, length: 10, payload: .objectData(ods)))
    }
    segments.append(DVBSubtitleSegment(type: 0x80, pageID: 1, length: 0, payload: .endOfDisplaySet))

    return DVBSubtitleDisplaySet(pesPacket: pes, segments: segments, compositionPageID: 1, ancillaryPageID: 1)
}

private func makeRCS(
    regionID: UInt8,
    version: UInt8 = 1,
    width: Int = 720,
    height: Int = 60,
    clutID: UInt8 = 1,
    objectIDs: [UInt16]
) -> DVBRegionCompositionSegment {
    let objRefs = objectIDs.enumerated().map { idx, id in
        DVBRegionObjectReference(
            objectID: id,
            objectType: 0x00,
            objectProviderFlag: 0x00,
            horizontalPosition: idx * 50,
            verticalPosition: 0
        )
    }

    return DVBRegionCompositionSegment(
        regionID: regionID,
        versionNumber: version,
        fillFlag: true,
        width: width,
        height: height,
        compatibilityLevel: 1,
        depth: 2,
        clutID: clutID,
        pixelCode8Bit: 0,
        pixelCode4Bit: 0,
        pixelCode2Bit: 0,
        objects: objRefs
    )
}

private func makeCLUT(clutID: UInt8, y: UInt8 = 255) -> DVBCLUTDefinitionSegment {
    let entry = DVBCLUTEntry(
        entryID: 0,
        entry2BitCLUTFlag: true,
        entry4BitCLUTFlag: true,
        entry8BitCLUTFlag: true,
        y: y,
        cr: 128,
        cb: 128,
        t: 0
    )
    return DVBCLUTDefinitionSegment(clutID: clutID, versionNumber: 1, entries: [entry])
}

private func makeODS(objectID: UInt16, topData: Data = Data([0x11, 0x22])) -> DVBObjectDataSegment {
    return DVBObjectDataSegment(
        objectID: objectID,
        versionNumber: 1,
        codingMethod: .pixels,
        nonModifyingColorFlag: false,
        topFieldData: topData,
        bottomFieldData: nil
    )
}
