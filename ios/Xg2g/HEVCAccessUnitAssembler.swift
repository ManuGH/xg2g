// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import VideoToolbox

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "hevc")

/// What every video assembler here reports, whatever the codec underneath.
///
/// The three assemblers differ entirely on the inside — HEVC splits NAL units,
/// MPEG-2 splits start codes, and what they hand VideoToolbox is not even the
/// same shape — but the pipeline downstream only ever needs a format and a
/// sample buffer. Sharing the outlet is what lets the pipeline swap assemblers
/// on the codec its PMT names instead of knowing about any of them.
public protocol VideoAccessUnitAssemblerDelegate: AnyObject, Sendable {
    func videoAssembler(didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo)
    func videoAssembler(didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure)
}

/// An assembler that turns an elementary stream into decodable access units.
public protocol VideoAccessUnitAssembling: AnyObject, Sendable {
    var assemblerDelegate: VideoAccessUnitAssemblerDelegate? { get set }
    func feed(payload: PESVideoData)
    func reset()
    func flush()
}

/// Stream-level Annex-B HEVC access unit assembler.
///
/// Structurally the same job as the H.264 assembler and deliberately built to
/// match it: split Annex-B, collect the NAL units of one picture, convert to
/// length-prefixed form, hand VideoToolbox a sample buffer. What differs is
/// where the information sits.
///
/// HEVC's NAL header is two bytes rather than one, and the type is six bits
/// starting at bit 1 of the first — so `nal & 0x1F` reads nothing meaningful
/// here. Parameter sets are three rather than two, VPS having no H.264
/// counterpart, and a picture starts wherever a VCL unit sets
/// `first_slice_segment_in_pic_flag`.
public final class HEVCAccessUnitAssembler: @unchecked Sendable, VideoAccessUnitAssembling {

    // MARK: NAL unit types (ITU-T H.265 Table 7-1)

    private enum NALType {
        static let vpsNut: UInt8 = 32
        static let spsNut: UInt8 = 33
        static let ppsNut: UInt8 = 34
        static let audNut: UInt8 = 35
        static let eosNut: UInt8 = 36
        static let eobNut: UInt8 = 37
        static let fdNut: UInt8 = 38

        /// Video coding layer — the units that carry actual picture data.
        static func isVCL(_ type: UInt8) -> Bool { type <= 31 }

        /// Intra random access point: a picture a decoder can be started on.
        /// Covers BLA, IDR and CRA alike, which is what "can start here" means
        /// for a broadcast that may never send an IDR.
        static func isIRAP(_ type: UInt8) -> Bool { (16...23).contains(type) }

        /// Dropped from the sample buffer: parameter sets travel in the format
        /// description, and these carry nothing a decoder needs per picture.
        static func isDroppedFromSample(_ type: UInt8) -> Bool {
            type == vpsNut || type == spsNut || type == ppsNut
                || type == audNut || type == eosNut || type == eobNut || type == fdNut
        }
    }

    public weak var assemblerDelegate: VideoAccessUnitAssemblerDelegate?

    private var streamBuffer = Data()
    private var currentAUNALs: [Data] = []
    private var currentAUPTS: CMTime = .invalid
    private var currentAUDTS: CMTime?
    private var pendingPTS: CMTime = .invalid
    private var pendingDTS: CMTime?
    private var currentAUHasVCL = false
    private var currentAUIsIRAP = false

    private var vpsData: Data?
    private var spsData: Data?
    private var ppsData: Data?
    private var currentFormatDescription: CMVideoFormatDescription?
    private var decodedInfo: H264DecodedInfo?

    public init() {}

    public func reset() {
        streamBuffer.removeAll(keepingCapacity: true)
        currentAUNALs.removeAll(keepingCapacity: true)
        currentAUPTS = .invalid
        currentAUDTS = nil
        pendingPTS = .invalid
        pendingDTS = nil
        currentAUHasVCL = false
        currentAUIsIRAP = false
        vpsData = nil
        spsData = nil
        ppsData = nil
        currentFormatDescription = nil
        decodedInfo = nil
    }

    public func feed(payload: PESVideoData) {
        if let pts = payload.pts, pts.isValid {
            pendingPTS = pts
            pendingDTS = payload.dts
        }
        streamBuffer.append(payload.data)
        processStreamBuffer()
    }

    public func feed(data: Data, pts: CMTime? = nil, dts: CMTime? = nil) {
        feed(payload: PESVideoData(data: data, pts: pts, dts: dts))
    }

    /// Ends the stream: the NAL unit still buffered is complete now, since
    /// nothing further can arrive to extend it.
    ///
    /// Without this the last picture was dropped — a NAL is only handed on once
    /// the *next* start code bounds it, and at the end there is no next.
    public func flush() {
        if let cursor = findStartCode(in: streamBuffer, from: 0) {
            let base = streamBuffer.startIndex
            let nalStart = base + cursor.offset + cursor.prefixLength
            if nalStart < streamBuffer.endIndex {
                let nal = streamBuffer.subdata(in: nalStart..<streamBuffer.endIndex)
                streamBuffer.removeAll(keepingCapacity: true)
                handleNAL(nal)
            }
        }
        emitCurrentAccessUnit()
    }

    // MARK: - Annex-B splitting

    /// Consumes whole NAL units, keeping the trailing partial one buffered.
    ///
    /// A NAL is only complete once the *next* start code has been seen, so the
    /// last one always stays behind until more data arrives — a picture emitted
    /// from a half-received slice decodes into garbage.
    /// Drops consumed bytes and re-bases the buffer.
    ///
    /// `Data.removeFirst` slides `startIndex` forward instead of re-basing, so
    /// the buffer stops being zero-based while `findStartCode` — which reads
    /// through `withUnsafeBytes` — keeps returning zero-based offsets. Feeding
    /// twice was then enough to index outside the slice and trap. Re-basing
    /// costs a copy of what is left, which is a partial access unit at most.
    private func consume(_ count: Int) {
        guard count > 0 else { return }
        streamBuffer = Data(streamBuffer.dropFirst(count))
    }

    private func processStreamBuffer() {
        guard var cursor = findStartCode(in: streamBuffer, from: 0) else {
            // No start code at all yet. Keep a short tail so a prefix split
            // across two PES payloads is still found next time.
            if streamBuffer.count > 4 {
                consume(streamBuffer.count - 3)
            }
            return
        }

        // Collected before anything is handed on: `handleNAL` can emit, and an
        // emit must not run while the buffer is being walked.
        var nals: [Data] = []
        while let next = findStartCode(in: streamBuffer, from: cursor.offset + cursor.prefixLength) {
            let base = streamBuffer.startIndex
            let nalStart = base + cursor.offset + cursor.prefixLength
            let nal = streamBuffer.subdata(in: nalStart..<(base + next.offset))
            if !nal.isEmpty {
                nals.append(nal)
            }
            cursor = next
        }

        // Everything before the last start code has been consumed.
        consume(cursor.offset)

        for nal in nals {
            handleNAL(nal)
        }
    }

    private func findStartCode(in data: Data, from offset: Int) -> (offset: Int, prefixLength: Int)? {
        let count = data.count
        guard count >= 3, offset <= count - 3 else { return nil }

        return data.withUnsafeBytes { raw -> (Int, Int)? in
            guard let ptr = raw.baseAddress?.assumingMemoryBound(to: UInt8.self) else { return nil }
            var i = offset
            while i < count - 2 {
                if ptr[i] == 0 && ptr[i + 1] == 0 {
                    if ptr[i + 2] == 1 {
                        return (i, 3)
                    } else if i < count - 3 && ptr[i + 2] == 0 && ptr[i + 3] == 1 {
                        return (i, 4)
                    }
                }
                i += 1
            }
            return nil
        }
    }

    // MARK: - Access unit assembly

    private func handleNAL(_ nal: Data) {
        guard nal.count >= 2 else { return }
        let type = (nal[nal.startIndex] >> 1) & 0x3F

        switch type {
        case NALType.vpsNut:
            vpsData = nal
            rebuildFormatDescription()
        case NALType.spsNut:
            spsData = nal
            rebuildFormatDescription()
        case NALType.ppsNut:
            ppsData = nal
            rebuildFormatDescription()
        default:
            break
        }

        if NALType.isVCL(type) {
            // A new picture starts here, so whatever was collected belongs to
            // the previous one and has to go out before this unit is added.
            if startsNewPicture(nal), currentAUHasVCL {
                emitCurrentAccessUnit()
            }
            if !currentAUHasVCL {
                currentAUPTS = pendingPTS
                currentAUDTS = pendingDTS
            }
            currentAUHasVCL = true
            if NALType.isIRAP(type) {
                currentAUIsIRAP = true
            }
        }

        currentAUNALs.append(nal)
    }

    /// Whether this VCL unit opens a picture.
    ///
    /// `first_slice_segment_in_pic_flag` is the first bit of the slice segment
    /// header, immediately after the two-byte NAL header.
    private func startsNewPicture(_ nal: Data) -> Bool {
        guard nal.count >= 3 else { return false }
        let headerByte = nal[nal.index(nal.startIndex, offsetBy: 2)]
        return (headerByte & 0x80) != 0
    }

    private func emitCurrentAccessUnit() {
        defer {
            currentAUNALs.removeAll(keepingCapacity: true)
            currentAUHasVCL = false
            currentAUIsIRAP = false
            currentAUPTS = .invalid
            currentAUDTS = nil
        }

        guard currentAUHasVCL,
              !currentAUNALs.isEmpty,
              let formatDesc = currentFormatDescription else { return }

        // Length-prefixed form, four-byte big-endian, matching the
        // `nalUnitHeaderLength: 4` the format description was built with.
        var sample = Data()
        for nal in currentAUNALs {
            guard nal.count >= 2 else { continue }
            let type = (nal[nal.startIndex] >> 1) & 0x3F
            if NALType.isDroppedFromSample(type) { continue }
            var length = UInt32(nal.count).bigEndian
            sample.append(Data(bytes: &length, count: 4))
            sample.append(nal)
        }
        guard !sample.isEmpty else { return }

        guard let sampleBuffer = Self.makeSampleBuffer(
            from: sample,
            formatDescription: formatDesc,
            pts: currentAUPTS,
            dts: currentAUDTS,
            isSyncSample: currentAUIsIRAP
        ) else { return }

        assemblerDelegate?.videoAssembler(
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: currentAUIsIRAP,
            // Broadcast HEVC is progressive; there is no interlaced signalling
            // to carry through, so the woven structure the pipeline expects for
            // a full frame is the honest answer.
            structure: .wovenTopFieldFirst
        )
    }

    // MARK: - Format description

    /// Builds the format description once all three parameter sets are known.
    ///
    /// VPS is required as well as SPS and PPS. Unlike H.264, where two sets
    /// suffice, HEVC carries sequence-wide parameters in the VPS, and
    /// VideoToolbox rejects the set without it.
    private func rebuildFormatDescription() {
        guard let vps = vpsData, let sps = spsData, let pps = ppsData else { return }

        let sets = [vps, sps, pps]
        let sizes = sets.map { $0.count }
        let total = sizes.reduce(0, +)

        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: total)
        defer { buffer.deallocate() }

        var pointers: [UnsafePointer<UInt8>] = []
        var offset = 0
        for set in sets {
            set.copyBytes(to: buffer.advanced(by: offset), count: set.count)
            pointers.append(UnsafePointer(buffer.advanced(by: offset)))
            offset += set.count
        }

        var formatDesc: CMVideoFormatDescription?
        let status = CMVideoFormatDescriptionCreateFromHEVCParameterSets(
            allocator: kCFAllocatorDefault,
            parameterSetCount: pointers.count,
            parameterSetPointers: pointers,
            parameterSetSizes: sizes,
            nalUnitHeaderLength: 4,
            extensions: nil,
            formatDescriptionOut: &formatDesc
        )

        guard status == noErr, let fd = formatDesc else {
            logger.error("[HEVC] parameter sets rejected: \(status)")
            return
        }

        let isDifferent = currentFormatDescription.map {
            !CMFormatDescriptionEqual($0, otherFormatDescription: fd)
        } ?? true

        currentFormatDescription = fd

        // Dimensions come from the format description rather than from parsing
        // the SPS: profile_tier_level makes the field positions conditional, and
        // VideoToolbox has already done the work correctly.
        let dimensions = CMVideoFormatDescriptionGetDimensions(fd)
        let info = H264DecodedInfo(
            width: Int(dimensions.width),
            height: Int(dimensions.height),
            isInterlaced: false,
            isTopFieldFirst: true
        )
        decodedInfo = info

        if isDifferent {
            let msg = "[HEVC] format \(dimensions.width)x\(dimensions.height)"
            logger.notice("\(msg, privacy: .public)")
            assemblerDelegate?.videoAssembler(didUpdateFormat: fd, info: info)
        }
    }

    // MARK: - Shared sample buffer construction

    /// Wraps assembled bytes in a `CMSampleBuffer` VideoToolbox will accept.
    ///
    /// Shared with the MPEG-2 assembler: the two produce entirely different
    /// payloads but wrap them identically, and the sync attachment is subtle
    /// enough to be worth writing once — it is `NotSync`, so a key picture sets
    /// it to false.
    static func makeSampleBuffer(
        from data: Data,
        formatDescription: CMVideoFormatDescription,
        pts: CMTime,
        dts: CMTime?,
        isSyncSample: Bool,
        duration: CMTime = .invalid
    ) -> CMSampleBuffer? {
        var blockBuffer: CMBlockBuffer?
        let createStatus = CMBlockBufferCreateWithMemoryBlock(
            allocator: kCFAllocatorDefault,
            memoryBlock: nil,
            blockLength: data.count,
            blockAllocator: kCFAllocatorDefault,
            customBlockSource: nil,
            offsetToData: 0,
            dataLength: data.count,
            flags: 0,
            blockBufferOut: &blockBuffer
        )
        guard createStatus == kCMBlockBufferNoErr, let block = blockBuffer else { return nil }

        let copyStatus = data.withUnsafeBytes { raw -> OSStatus in
            guard let base = raw.baseAddress else { return -1 }
            return CMBlockBufferReplaceDataBytes(
                with: base,
                blockBuffer: block,
                offsetIntoDestination: 0,
                dataLength: data.count
            )
        }
        guard copyStatus == kCMBlockBufferNoErr else { return nil }

        var timing = CMSampleTimingInfo(
            duration: duration,
            presentationTimeStamp: pts.isValid ? pts : .invalid,
            decodeTimeStamp: dts ?? .invalid
        )
        var sampleSize = data.count
        var sampleBuffer: CMSampleBuffer?

        let sampleStatus = CMSampleBufferCreateReady(
            allocator: kCFAllocatorDefault,
            dataBuffer: block,
            formatDescription: formatDescription,
            sampleCount: 1,
            sampleTimingEntryCount: 1,
            sampleTimingArray: &timing,
            sampleSizeEntryCount: 1,
            sampleSizeArray: &sampleSize,
            sampleBufferOut: &sampleBuffer
        )
        guard sampleStatus == noErr, let sb = sampleBuffer else { return nil }

        if let attachments = CMSampleBufferGetSampleAttachmentsArray(sb, createIfNecessary: true) {
            let dict = unsafeBitCast(CFArrayGetValueAtIndex(attachments, 0), to: CFMutableDictionary.self)
            CFDictionarySetValue(
                dict,
                Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(),
                Unmanaged.passUnretained(isSyncSample ? kCFBooleanFalse : kCFBooleanTrue).toOpaque()
            )
            CFDictionarySetValue(
                dict,
                Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(),
                Unmanaged.passUnretained(kCFBooleanTrue).toOpaque()
            )
        }

        return sb
    }
}
