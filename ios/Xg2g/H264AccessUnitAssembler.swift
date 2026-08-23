// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import QuartzCore
import VideoToolbox

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "h264")

public struct H264DecodedInfo: Sendable {
    public let width: Int
    public let height: Int
    public let isInterlaced: Bool
    public let isTopFieldFirst: Bool
}

/// How a coded picture maps onto fields, read from the slice header.
///
/// A PAFF broadcast switches between frame and field pictures per picture, and
/// the two need a different number of presentations — a frame carries both
/// fields and is bobbed into two, a field picture is one. Inferring that from
/// PTS spacing works only until the spacing wobbles, so it is read instead.
public struct H264PictureStructure: Sendable {
    /// True when this coded picture carries a single field.
    public let isFieldPicture: Bool
    /// Which field it carries. Only meaningful when `isFieldPicture` is true.
    public let isBottomField: Bool
    /// For a frame picture, which of its two fields is temporally first.
    public let isTopFieldFirst: Bool
    /// True when the picture/sequence is interlaced.
    public let isInterlaced: Bool

    public init(isFieldPicture: Bool, isBottomField: Bool, isTopFieldFirst: Bool, isInterlaced: Bool = true) {
        self.isFieldPicture = isFieldPicture
        self.isBottomField = isBottomField
        self.isTopFieldFirst = isTopFieldFirst
        self.isInterlaced = isInterlaced
    }

    /// DVB 1080i frame picture: both fields woven, top field first.
    public static let wovenTopFieldFirst = H264PictureStructure(
        isFieldPicture: false, isBottomField: false, isTopFieldFirst: true, isInterlaced: true
    )

    /// Progressive frame picture (e.g. 720p50, 1080p50).
    public static let progressive = H264PictureStructure(
        isFieldPicture: false, isBottomField: false, isTopFieldFirst: true, isInterlaced: false
    )
}

/// Thread-safe persistent cache for SPS / PPS parameter sets per channel URL or stream key.
public final class H264ParameterSetCache: @unchecked Sendable {
    public static let shared = H264ParameterSetCache()
    private var cache: [String: (sps: Data, pps: Data)] = [:]
    private let lock = NSLock()
    private let userDefaultsKey = "io.github.manugh.xg2g.h264_sps_pps_cache"

    public init() {
        loadFromStorage()
    }

    private func loadFromStorage() {
        guard let saved = UserDefaults.standard.dictionary(forKey: userDefaultsKey) as? [String: [String: Data]] else {
            return
        }
        for (key, dict) in saved {
            if let sps = dict["sps"], let pps = dict["pps"] {
                cache[key] = (sps, pps)
            }
        }
    }

    private func persistToStorage() {
        var toSave: [String: [String: Data]] = [:]
        for (key, val) in cache {
            toSave[key] = ["sps": val.sps, "pps": val.pps]
        }
        UserDefaults.standard.set(toSave, forKey: userDefaultsKey)
    }

    public func setParameterSets(sps: Data, pps: Data, for key: String) {
        lock.lock()
        let old = cache[key]
        cache[key] = (sps, pps)
        if old?.sps != sps || old?.pps != pps {
            persistToStorage()
        }
        lock.unlock()
    }

    public func parameterSets(for key: String) -> (sps: Data, pps: Data)? {
        lock.lock()
        defer { lock.unlock() }
        return cache[key]
    }

    public func clear() {
        lock.lock()
        cache.removeAll()
        UserDefaults.standard.removeObject(forKey: userDefaultsKey)
        lock.unlock()
    }
}

public protocol H264AccessUnitAssemblerDelegate: AnyObject, Sendable {
    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo)
    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure)
    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didDiscoverAFD afd: VideoGeometry.ActiveFormatDescription)
}

public extension H264AccessUnitAssemblerDelegate {
    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didDiscoverAFD afd: VideoGeometry.ActiveFormatDescription) {}
}

/// Stream-level Annex-B H.264 Access Unit Assembler.
///
/// Features:
/// - Ingests arbitrary continuous Elementary Stream chunks from PES packets.
/// - Buffers across packet boundaries and splits Annex-B NAL units (`00 00 01` / `00 00 00 01`).
/// - Implements ITU-T H.264 Access Unit (AU) boundary detection (AUD, SPS/PPS/SEI transitions, `first_mb_in_slice == 0`).
/// - Synchronizes PTS/DTS timestamps with Access Unit start boundaries.
/// - Converts Annex-B NALs into 4-byte AVCC format and constructs valid `CMSampleBuffer`s for VideoToolbox.
public final class H264AccessUnitAssembler: @unchecked Sendable {

    private var streamBuffer = Data()
    private var currentAUNALs: [Data] = []
    private var currentAUPTS: CMTime = .invalid
    private var currentAUDTS: CMTime? = nil
    private var pendingPTS: CMTime = .invalid
    private var pendingDTS: CMTime? = nil
    private var currentAUHasVCL: Bool = false
    private var currentAUHasIDR: Bool = false
    /// Cleared by the first slice of this access unit that codes anything other
    /// than intra macroblocks. See `parseSliceIsIntra`.
    private var currentAUIsIntraOnly: Bool = true
    private var currentAUStructure: H264PictureStructure = .wovenTopFieldFirst
    private var lastEmittedPTS: CMTime = .invalid

    // Slice-header geometry, needed to reach `field_pic_flag`. Captured from the
    // active SPS; the defaults describe a progressive frame-only stream.
    private var log2MaxFrameNum: Int = 4
    private var separateColourPlaneFlag: Bool = false
    private var frameMbsOnlyFlag: Bool = true

    /// Every distinct parameter set the stream has offered, not just the newest.
    ///
    /// A single `spsData`/`ppsData` pair silently assumed one of each. ZDF sends
    /// **two** different PPS — 44 PPS NALs against 22 SPS in 15 s — and keeping
    /// only the last one broke the channel twice over: slices referencing the
    /// overwritten PPS became undecodable (`kVTVideoDecoderBadDataErr`, -12909),
    /// and each swap changed the format description, which tore down and rebuilt
    /// the VideoToolbox session. Throughput collapsed to 1.3 access units per
    /// second on a 50 fps source.
    ///
    /// `CMVideoFormatDescriptionCreateFromH264ParameterSets` takes as many
    /// parameter sets as the stream has, so all of them are handed over and the
    /// decoder resolves each slice against the one it names.
    private var spsVariants: [Data] = []
    private var ppsVariants: [Data] = []

    private var spsData: Data? { spsVariants.first }
    private var ppsData: Data? { ppsVariants.first }
    private var currentFormatDescription: CMVideoFormatDescription?
    public private(set) var decodedInfo: H264DecodedInfo?
    public var channelKey: String?

    public weak var delegate: H264AccessUnitAssemblerDelegate?

    // MARK: - Startup instrumentation
    //
    // `ttfpParamSetsMs` says how long the wait for SPS/PPS was, never why. The
    // broadcast repeats parameter sets every ~1.0 s (measured on the wire), yet
    // cold tunes have taken 742, 1962, 2796 and 6813 ms to reach them. That gap
    // is inside this class, and nothing here reported enough to locate it: how
    // much elementary stream was consumed, how many NALs were seen, and which
    // types came past before the first SPS.
    private var startupClock: CFTimeInterval = 0
    private var bytesFedBeforeParams: Int = 0
    private var nalsSeenBeforeParams: Int = 0
    private var nalTypeHistogram: [UInt8: Int] = [:]
    private var hasReportedParamArrival = false

    public init() {}

    /// Primes the assembler with pre-cached SPS and PPS parameter sets for immediate decoding.
    ///
    /// Only the first variant of each is cached, so a channel carrying several
    /// still starts on this pair and picks the rest up as they arrive.
    public func primeWithParameterSets(sps: Data, pps: Data) {
        spsVariants = [sps]
        ppsVariants = [pps]
        parseSPS(sps)
        updateFormatDescriptionIfNeeded()
    }

    public func reset() {
        streamBuffer.removeAll(keepingCapacity: true)
        currentAUNALs.removeAll(keepingCapacity: true)
        currentAUPTS = .invalid
        currentAUDTS = nil
        pendingPTS = .invalid
        pendingDTS = nil
        currentAUHasVCL = false
        currentAUHasIDR = false
        currentAUIsIntraOnly = true
        currentAUStructure = .wovenTopFieldFirst
        log2MaxFrameNum = 4
        separateColourPlaneFlag = false
        frameMbsOnlyFlag = true
        lastEmittedPTS = .invalid
        spsVariants.removeAll(keepingCapacity: true)
        ppsVariants.removeAll(keepingCapacity: true)
        currentFormatDescription = nil
        decodedInfo = nil

        startupClock = 0
        bytesFedBeforeParams = 0
        nalsSeenBeforeParams = 0
        nalTypeHistogram.removeAll(keepingCapacity: true)
        hasReportedParamArrival = false
    }

    /// Ingests elementary stream data from a PES packet with optional PTS/DTS.
    public func feed(payload: PESVideoData) {
        if let pts = payload.pts, pts.isValid {
            pendingPTS = pts
            pendingDTS = payload.dts
        }

        if startupClock == 0 {
            startupClock = CACurrentMediaTime()
        }
        if !hasReportedParamArrival {
            bytesFedBeforeParams += payload.data.count
        }

        streamBuffer.append(payload.data)
        processStreamBuffer()
    }

    /// Convenience feed for direct data / tests.
    public func feed(data: Data, pts: CMTime? = nil, dts: CMTime? = nil) {
        let payload = PESVideoData(data: data, pts: pts, dts: dts)
        feed(payload: payload)
    }

    /// Flushes any pending Access Unit in the buffer (e.g. at end of stream).
    public func flush() {
        // Extract any remaining trailing NAL unit in streamBuffer
        if let (startCodeOffset, prefixLen) = findNextStartCode(in: streamBuffer, from: 0) {
            let nalStart = startCodeOffset + prefixLen
            if nalStart < streamBuffer.count {
                var nalData = Data(streamBuffer[nalStart..<streamBuffer.count])
                while nalData.count > 0 && nalData.last == 0 {
                    nalData.removeLast()
                }
                if !nalData.isEmpty {
                    handleNALUnit(nalData)
                }
            }
            streamBuffer.removeAll(keepingCapacity: true)
        }

        if !currentAUNALs.isEmpty && currentAUHasVCL {
            emitCurrentAccessUnit()
        }
    }

    // MARK: - Stream & NAL Parsing

    private func processStreamBuffer() {
        while true {
            guard let (startCodeOffset, prefixLen) = findNextStartCode(in: streamBuffer, from: 0) else {
                // Keep only the last 3 bytes in case a start code is split across incoming chunks
                if streamBuffer.count > 3 {
                    streamBuffer = Data(streamBuffer.suffix(3))
                }
                break
            }

            let nalStart = startCodeOffset + prefixLen
            guard let (nextStartCodeOffset, _) = findNextStartCode(in: streamBuffer, from: nalStart) else {
                // Incomplete trailing NAL in buffer; keep buffer from startCodeOffset for next chunk
                if startCodeOffset > 0 {
                    streamBuffer = Data(streamBuffer.dropFirst(startCodeOffset))
                }
                break
            }

            var nalData = Data(streamBuffer[nalStart..<nextStartCodeOffset])
            while nalData.count > 0 && nalData.last == 0 {
                nalData.removeLast()
            }
            if !nalData.isEmpty {
                handleNALUnit(nalData)
            }

            streamBuffer = Data(streamBuffer.dropFirst(nextStartCodeOffset))
        }
    }

    private func findNextStartCode(in data: Data, from offset: Int) -> (Int, Int)? {
        let count = data.count
        guard count >= 3 && offset <= count - 3 else { return nil }

        var i = offset
        return data.withUnsafeBytes { rawBuffer -> (Int, Int)? in
            guard let ptr = rawBuffer.baseAddress?.assumingMemoryBound(to: UInt8.self) else { return nil }

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

    // MARK: - Access Unit Boundary Detection (Broadcast DVB Optimized)

    private func handleNALUnit(_ nal: Data) {
        guard !nal.isEmpty else { return }
        let nalType = nal[0] & 0x1F

        if !hasReportedParamArrival {
            nalsSeenBeforeParams += 1
            nalTypeHistogram[nalType, default: 0] += 1
        }

        var isNewAccessUnit = false

        switch nalType {
        case 9: // AUD (Access Unit Delimiter)
            if currentAUHasVCL {
                isNewAccessUnit = true
            }

        case 7, 8, 6: // SPS, PPS, SEI
            // If non-VCL parameter sets arrive after VCL, a new picture boundary is marked
            if currentAUHasVCL {
                isNewAccessUnit = true
            }

        case 1, 5: // VCL Slice (Non-IDR or IDR)
            let firstMB = parseFirstMBInSlice(nal)
            if currentAUHasVCL && firstMB == 0 {
                // First macroblock in slice == 0 marks start of next coded picture
                isNewAccessUnit = true
            }

        default:
            break
        }

        if isNewAccessUnit {
            emitCurrentAccessUnit()
        }

        // Adopt pending timestamp if this starts an Access Unit
        if currentAUNALs.isEmpty {
            if pendingPTS.isValid {
                currentAUPTS = pendingPTS
                currentAUDTS = pendingDTS
                pendingPTS = .invalid
                pendingDTS = nil
            }
        }

        // Process parameter sets or slices
        switch nalType {
        case 7: // SPS
            if rememberParameterSet(nal, in: &spsVariants) {
                parseSPS(nal)
                updateFormatDescriptionIfNeeded()
            }

        case 8: // PPS
            if rememberParameterSet(nal, in: &ppsVariants) {
                updateFormatDescriptionIfNeeded()
            }

        case 5: // IDR Slice
            // Only the AU's first slice is parsed; later slices of the same
            // picture repeat these fields, and their `first_mb_in_slice` is
            // large enough to push the header past the bytes read here.
            if !currentAUHasVCL {
                currentAUStructure = parseSliceStructure(from: nal)
            }
            currentAUHasVCL = true
            currentAUHasIDR = true

        case 1: // Non-IDR Slice
            if !currentAUHasVCL {
                currentAUStructure = parseSliceStructure(from: nal)
            }
            // Checked per slice, not once per picture: a single predicted slice
            // makes the whole access unit depend on a reference frame.
            if !parseSliceIsIntra(nal) {
                currentAUIsIntraOnly = false
            }
            currentAUHasVCL = true

        case 6: // SEI (Supplemental Enhancement Information)
            parseSEINal(nal)

        default:
            break
        }

        currentAUNALs.append(nal)
    }

    /// Inspects SEI NAL units for broadcast metadata like Active Format Description (AFD).
    private func parseSEINal(_ nal: Data) {
        guard nal.count > 1 else { return }
        let rbsp = removeEmulationPreventionBytes(from: Data(nal.dropFirst(1)))
        if let afd = VideoGeometry.parseActiveFormatDescription(fromSEIPayload: rbsp) {
            delegate?.accessUnitAssembler(self, didDiscoverAFD: afd)
        }
    }

    /// True when this slice codes only intra macroblocks (`slice_type` I or SI).
    ///
    /// An access unit made entirely of these needs no reference picture, so it is
    /// a legitimate place to start decoding even without an IDR — which matters
    /// because a fair number of broadcast encoders signal random access with a
    /// recovery-point SEI and plain I slices and never send an IDR at all.
    /// Gating only on NAL type 5 would leave those channels black.
    private func parseSliceIsIntra(_ nal: Data) -> Bool {
        guard nal.count > 1 else { return false }
        let sliceHeader = Data(nal.dropFirst(1).prefix(8))
        let rbsp = removeEmulationPreventionBytes(from: sliceHeader)
        var reader = BitReader(data: rbsp)
        _ = reader.readExpGolomb()             // first_mb_in_slice
        let sliceType = reader.readExpGolomb()
        // A header that ran out mid-read says nothing; treat it as predicted
        // rather than letting a truncated parse open the gate.
        guard !reader.isExhausted else { return false }
        // 5...9 repeat 0...4 with the added promise that every slice in the
        // picture has that type. Either way the family is what matters here.
        let family = sliceType % 5
        return family == 2 || family == 4      // I or SI
    }

    private func parseFirstMBInSlice(_ nal: Data) -> Int {
        guard nal.count > 1 else { return 0 }
        let sliceHeader = Data(nal.dropFirst(1).prefix(5))
        let rbsp = removeEmulationPreventionBytes(from: sliceHeader)
        var reader = BitReader(data: rbsp)
        return reader.readExpGolomb()
    }

    private func emitCurrentAccessUnit() {
        defer {
            currentAUNALs.removeAll(keepingCapacity: true)
            currentAUHasVCL = false
            currentAUHasIDR = false
            currentAUIsIntraOnly = true
            currentAUStructure = .wovenTopFieldFirst
            currentAUPTS = .invalid
            currentAUDTS = nil
        }

        guard !currentAUNALs.isEmpty, currentAUHasVCL, let formatDesc = currentFormatDescription else { return }

        // Whether a decoder can be started on this picture. An IDR always can;
        // so can a picture whose every slice is intra-coded, which is what
        // reaches the gate on channels that never send one.
        let isSyncSample = currentAUHasIDR || currentAUIsIntraOnly

        // Assemble AVCC buffer with 4-byte big endian length prefixes
        var avccBuffer = Data()
        for nal in currentAUNALs {
            let nalType = nal[0] & 0x1F
            if nalType == 7 || nalType == 8 || nalType == 9 || nalType == 12 {
                // SPS (7), PPS (8), AUD (9), Filler (12) are omitted from AVCC sample buffers
                continue
            }
            var length = UInt32(nal.count).bigEndian
            avccBuffer.append(Data(bytes: &length, count: 4))
            avccBuffer.append(nal)
        }

        guard !avccBuffer.isEmpty else { return }

        // Construct CMBlockBuffer with safe native allocation
        var blockBuffer: CMBlockBuffer?
        let blockStatus = CMBlockBufferCreateWithMemoryBlock(
            allocator: kCFAllocatorDefault,
            memoryBlock: nil,
            blockLength: avccBuffer.count,
            blockAllocator: kCFAllocatorDefault,
            customBlockSource: nil,
            offsetToData: 0,
            dataLength: avccBuffer.count,
            flags: 0,
            blockBufferOut: &blockBuffer
        )

        guard blockStatus == kCMBlockBufferNoErr, let block = blockBuffer else { return }

        let copyStatus = avccBuffer.withUnsafeBytes { rawBytes -> OSStatus in
            guard let ptr = rawBytes.baseAddress else { return -1 }
            return CMBlockBufferReplaceDataBytes(
                with: ptr,
                blockBuffer: block,
                offsetIntoDestination: 0,
                dataLength: avccBuffer.count
            )
        }

        guard copyStatus == kCMBlockBufferNoErr else { return }

        // Compute coded frame duration:
        // In 1080i50, each coded frame represents 2 fields (40 ms = 1/25s).
        // If consecutive PTS timestamps are available, derive exact duration from PTS delta.
        var frameDuration: CMTime
        if currentAUPTS.isValid, lastEmittedPTS.isValid {
            let delta = CMTimeSubtract(currentAUPTS, lastEmittedPTS)
            if delta.isValid && delta.seconds >= 0.01 && delta.seconds <= 0.2 {
                frameDuration = delta
            } else {
                frameDuration = (decodedInfo?.isInterlaced == false) ? CMTime(value: 1, timescale: 50) : CMTime(value: 1, timescale: 25)
            }
        } else {
            frameDuration = (decodedInfo?.isInterlaced == false) ? CMTime(value: 1, timescale: 50) : CMTime(value: 1, timescale: 25)
        }

        if currentAUPTS.isValid {
            lastEmittedPTS = currentAUPTS
        }

        var timing = CMSampleTimingInfo(
            duration: frameDuration,
            presentationTimeStamp: currentAUPTS.isValid ? currentAUPTS : .invalid,
            decodeTimeStamp: currentAUDTS ?? .invalid
        )

        var sampleBuffer: CMSampleBuffer?
        var sampleSize = avccBuffer.count

        let sampleStatus = CMSampleBufferCreateReady(
            allocator: kCFAllocatorDefault,
            dataBuffer: block,
            formatDescription: formatDesc,
            sampleCount: 1,
            sampleTimingEntryCount: 1,
            sampleTimingArray: &timing,
            sampleSizeEntryCount: 1,
            sampleSizeArray: &sampleSize,
            sampleBufferOut: &sampleBuffer
        )

        guard sampleStatus == noErr, let sb = sampleBuffer else { return }

        // Attach sample attachments for field order / sync
        if let attachments = CMSampleBufferGetSampleAttachmentsArray(sb, createIfNecessary: true) {
            let dict = unsafeBitCast(CFArrayGetValueAtIndex(attachments, 0), to: CFMutableDictionary.self)
            if isSyncSample {
                CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(), Unmanaged.passUnretained(kCFBooleanFalse).toOpaque())
            } else {
                CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
            }
            CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
        }

        delegate?.accessUnitAssembler(self, didEmitSampleBuffer: sb, isSyncSample: isSyncSample, structure: currentAUStructure)
    }

    /// Reports what it took to reach the first usable parameter set.
    ///
    /// The elapsed time alone cannot separate the two candidate explanations —
    /// elementary stream arriving late, or arriving on time and being scanned
    /// past — so the byte count and the NAL types seen on the way are reported
    /// with it. A stream carrying SPS every second should reach this within one
    /// interval, on the order of a hundred KiB.
    private func reportParamArrival() {
        guard !hasReportedParamArrival else { return }
        hasReportedParamArrival = true

        let elapsed = startupClock > 0 ? (CACurrentMediaTime() - startupClock) * 1000.0 : 0
        let types = nalTypeHistogram
            .sorted { $0.key < $1.key }
            .map { "t\($0.key):\($0.value)" }
            .joined(separator: " ")
        let msg = "[1080i50-PARAMS] First SPS+PPS after \(String(format: "%.0f", elapsed))ms of ES | \(bytesFedBeforeParams / 1024) KiB fed | \(nalsSeenBeforeParams) NALs | types: \(types)"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    /// Records a parameter set the first time it is seen. Returns true when it is
    /// new, so an unchanged set repeated every GOP does not rebuild anything.
    ///
    /// Identity is the NAL's own bytes rather than its parsed id: two sets that
    /// differ anywhere have to be offered to the decoder separately, and a set
    /// retransmitted verbatim must not count as a change.
    /// Plausible size range for a parameter set NAL.
    ///
    /// A real SPS runs to tens of bytes and a PPS to a handful; anything near a
    /// kilobyte is a slice whose start code was mis-split and whose first byte
    /// happens to read as type 7 or 8. Keeping only the newest set hid that —
    /// the next genuine SPS overwrote the garbage — but collecting every variant
    /// makes a single bad parse permanent, and a 16 KB "parameter set" made
    /// `CMVideoFormatDescriptionCreateFromH264ParameterSets` reject the whole
    /// set (-12712) for the rest of the stream.
    private static let parameterSetSizeLimit = 1024

    private func rememberParameterSet(_ nal: Data, in variants: inout [Data]) -> Bool {
        guard nal.count >= 2, nal.count <= Self.parameterSetSizeLimit else { return false }
        guard !variants.contains(nal) else { return false }
        // Bounded so a stream that mutates its parameter sets endlessly cannot
        // grow this without limit; broadcast carries a handful at most.
        if variants.count >= 8 {
            variants.removeFirst()
        }
        variants.append(nal)
        return true
    }

    private func updateFormatDescriptionIfNeeded() {
        guard !spsVariants.isEmpty, !ppsVariants.isEmpty else { return }

        // SPS first, then PPS — the order CMVideoFormatDescription expects.
        let sets = spsVariants + ppsVariants
        let sizes = sets.map { $0.count }

        // All parameter sets are copied into one contiguous allocation, so every
        // pointer handed to CoreMedia stays valid for the whole call.
        // `withUnsafeBufferPointer` per set would need the scopes to nest, and a
        // pointer collected in a loop outlives the scope that produced it —
        // which produced no format description at all and left the channel
        // undecodable.
        let total = sizes.reduce(0, +)
        guard total > 0 else { return }

        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: total)
        defer { buffer.deallocate() }

        var pointers: [UnsafePointer<UInt8>] = []
        pointers.reserveCapacity(sets.count)
        var offset = 0
        for set in sets {
            set.copyBytes(to: buffer.advanced(by: offset), count: set.count)
            pointers.append(UnsafePointer(buffer.advanced(by: offset)))
            offset += set.count
        }

        var formatDesc: CMVideoFormatDescription?
        let status = CMVideoFormatDescriptionCreateFromH264ParameterSets(
            allocator: kCFAllocatorDefault,
            parameterSetCount: pointers.count,
            parameterSetPointers: pointers,
            parameterSetSizes: sizes,
            nalUnitHeaderLength: 4,
            formatDescriptionOut: &formatDesc
        )

        if status == noErr, let fd = formatDesc {
            let isDifferent: Bool
            if let existing = self.currentFormatDescription {
                isDifferent = !CMFormatDescriptionEqual(existing, otherFormatDescription: fd)
            } else {
                isDifferent = true
            }

            self.currentFormatDescription = fd
            if let sps = spsData, let pps = ppsData, let key = channelKey {
                H264ParameterSetCache.shared.setParameterSets(sps: sps, pps: pps, for: key)
            }

            if isDifferent, let info = self.decodedInfo {
                reportParamArrival()
                delegate?.accessUnitAssembler(self, didUpdateFormat: fd, info: info)
            }
        }
    }

    // MARK: - SPS & Field Order Parsing

    private func removeEmulationPreventionBytes(from data: Data) -> Data {
        var rbsp = Data()
        var zeroCount = 0
        for byte in data {
            if zeroCount >= 2 && byte == 0x03 {
                zeroCount = 0
                continue
            }
            rbsp.append(byte)
            if byte == 0x00 {
                zeroCount += 1
            } else {
                zeroCount = 0
            }
        }
        return rbsp
    }

    private func parseSPS(_ nal: Data) {
        guard nal.count >= 4 else { return }
        let rawSPS = Data(nal.dropFirst(1))
        let rbsp = removeEmulationPreventionBytes(from: rawSPS)
        var reader = BitReader(data: rbsp)

        let profileIDC = reader.readBits(8)
        _ = reader.readBits(8) // constraint flags
        _ = reader.readBits(8) // level_idc
        _ = reader.readExpGolomb() // seq_parameter_set_id

        var chromaFormatIDC = 1
        var separateColourPlane = false
        if profileIDC == 100 || profileIDC == 110 || profileIDC == 122 || profileIDC == 244 || profileIDC == 44 || profileIDC == 83 || profileIDC == 86 || profileIDC == 118 || profileIDC == 128 {
            chromaFormatIDC = reader.readExpGolomb()
            if chromaFormatIDC == 3 {
                separateColourPlane = reader.readBits(1) == 1
            }
            _ = reader.readExpGolomb() // bit_depth_luma_minus8
            _ = reader.readExpGolomb() // bit_depth_chroma_minus8
            _ = reader.readBits(1) // qpprime_y_zero_transform_bypass_flag
            let seqScalingMatrixPresent = reader.readBits(1) == 1
            if seqScalingMatrixPresent {
                let count = (chromaFormatIDC != 3) ? 8 : 12
                for i in 0..<count {
                    let present = reader.readBits(1) == 1
                    if present {
                        let size = (i < 6) ? 16 : 64
                        var lastScale = 8
                        var nextScale = 8
                        for _ in 0..<size {
                            if nextScale != 0 {
                                let deltaScale = reader.readSignedExpGolomb()
                                nextScale = (lastScale + deltaScale + 256) % 256
                            }
                            lastScale = (nextScale == 0) ? lastScale : nextScale
                        }
                    }
                }
            }
        }

        let log2MaxFrameNumMinus4 = reader.readExpGolomb()
        let picOrderCntType = reader.readExpGolomb()
        if picOrderCntType == 0 {
            _ = reader.readExpGolomb() // log2_max_pic_order_cnt_lsb_minus4
        } else if picOrderCntType == 1 {
            _ = reader.readBits(1) // delta_pic_order_always_zero_flag
            _ = reader.readSignedExpGolomb() // offset_for_non_ref_pic
            _ = reader.readSignedExpGolomb() // offset_for_top_to_bottom_field
            let numRefFramesInPicOrderCntCycle = reader.readExpGolomb()
            for _ in 0..<numRefFramesInPicOrderCntCycle {
                _ = reader.readSignedExpGolomb()
            }
        }

        _ = reader.readExpGolomb() // max_num_ref_frames
        _ = reader.readBits(1) // gaps_in_frame_num_value_allowed_flag
        let picWidthInMbsMinus1 = reader.readExpGolomb()
        let picHeightInMapUnitsMinus1 = reader.readExpGolomb()
        let frameMbsOnlyFlag = reader.readBits(1) == 1
        var mbAdaptiveFrameFieldFlag = false
        if !frameMbsOnlyFlag {
            mbAdaptiveFrameFieldFlag = reader.readBits(1) == 1
        }
        _ = reader.readBits(1) // direct_8x8_inference_flag

        let isInterlaced = !frameMbsOnlyFlag || mbAdaptiveFrameFieldFlag

        // Retained for slice-header parsing: reaching `field_pic_flag` requires
        // knowing the width of `frame_num` and whether `colour_plane_id` is present.
        self.log2MaxFrameNum = min(max(log2MaxFrameNumMinus4 + 4, 4), 32)
        self.separateColourPlaneFlag = separateColourPlane
        self.frameMbsOnlyFlag = frameMbsOnlyFlag

        var width = (picWidthInMbsMinus1 + 1) * 16
        var height = (picHeightInMapUnitsMinus1 + 1) * 16 * (frameMbsOnlyFlag ? 1 : 2)

        let frameCroppingFlag = reader.readBits(1) == 1
        if frameCroppingFlag {
            let cropLeft = reader.readExpGolomb()
            let cropRight = reader.readExpGolomb()
            let cropTop = reader.readExpGolomb()
            let cropBottom = reader.readExpGolomb()

            let cropUnitX = (chromaFormatIDC == 0) ? 1 : (chromaFormatIDC == 3 ? 1 : 2)
            let cropUnitY = (chromaFormatIDC == 0) ? (2 - (frameMbsOnlyFlag ? 1 : 0)) : (chromaFormatIDC == 3 ? (2 - (frameMbsOnlyFlag ? 1 : 0)) : (2 * (2 - (frameMbsOnlyFlag ? 1 : 0))))
            width -= (cropLeft + cropRight) * cropUnitX
            height -= (cropTop + cropBottom) * cropUnitY
        }

        self.decodedInfo = H264DecodedInfo(
            width: width,
            height: height,
            isInterlaced: isInterlaced,
            isTopFieldFirst: true // Default for DVB 1080i broadcast is Top Field First
        )
    }

    /// Reads `field_pic_flag` and `bottom_field_flag` from a slice header
    /// (ITU-T H.264 §7.3.3).
    ///
    /// Field order for a *frame* picture is not in the slice header — it lives in
    /// an SEI `pic_struct` this assembler does not parse — so TFF stays the
    /// documented assumption for DVB 1080i there. A *field* picture states its
    /// own parity, and that is what gets read.
    private func parseSliceStructure(from nal: Data) -> H264PictureStructure {
        // frame_mbs_only_flag == 1 means field pictures cannot occur, and
        // the stream is purely progressive.
        guard !frameMbsOnlyFlag, nal.count > 1 else {
            return frameMbsOnlyFlag ? .progressive : .wovenTopFieldFirst
        }

        // 16 bytes of RBSP comfortably covers the header up to bottom_field_flag
        // for a slice with first_mb_in_slice == 0, which is the only kind parsed.
        let rbsp = removeEmulationPreventionBytes(from: Data(nal.dropFirst(1).prefix(24)))
        var reader = BitReader(data: rbsp)

        _ = reader.readExpGolomb()      // first_mb_in_slice
        _ = reader.readExpGolomb()      // slice_type
        _ = reader.readExpGolomb()      // pic_parameter_set_id
        if separateColourPlaneFlag {
            _ = reader.readBits(2)      // colour_plane_id
        }
        _ = reader.readBits(log2MaxFrameNum)   // frame_num

        guard !reader.isExhausted else { return .wovenTopFieldFirst }

        let isFieldPicture = reader.readBits(1) == 1
        let isBottomField = isFieldPicture ? (reader.readBits(1) == 1) : false

        // A header that ran out mid-read yields values that cannot be trusted.
        guard !reader.isExhausted || !isFieldPicture else { return .wovenTopFieldFirst }

        return H264PictureStructure(
            isFieldPicture: isFieldPicture,
            isBottomField: isBottomField,
            isTopFieldFirst: true,
            isInterlaced: true
        )
    }
}

// MARK: - BitReader for SPS exponential golomb parsing

private struct BitReader {
    private let data: [UInt8]
    private var byteOffset: Int = 0
    private var bitOffset: Int = 0

    init(data: Data) {
        self.data = [UInt8](data)
    }

    /// True once the reader has run past the end of its data, so callers can
    /// tell a real bit value from a zero returned by exhaustion.
    var isExhausted: Bool { byteOffset >= data.count }

    mutating func readBits(_ count: Int) -> Int {
        var value = 0
        for _ in 0..<count {
            guard byteOffset < data.count else { break }
            let bit = (Int(data[byteOffset]) >> (7 - bitOffset)) & 1
            value = (value << 1) | bit
            bitOffset += 1
            if bitOffset == 8 {
                bitOffset = 0
                byteOffset += 1
            }
        }
        return value
    }

    mutating func readExpGolomb() -> Int {
        var leadingZeros = 0
        while byteOffset < data.count && readBits(1) == 0 {
            leadingZeros += 1
            // A malformed header can otherwise run the prefix past Int's width
            // and trap on the shift below.
            if leadingZeros >= 32 { return 0 }
        }
        if leadingZeros == 0 { return 0 }
        let suffix = readBits(leadingZeros)
        return (1 << leadingZeros) - 1 + suffix
    }

    mutating func readSignedExpGolomb() -> Int {
        let val = readExpGolomb()
        let sign = (val & 1) == 0 ? -1 : 1
        return sign * ((val + 1) / 2)
    }
}
