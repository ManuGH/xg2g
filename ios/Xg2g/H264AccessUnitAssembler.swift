// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import VideoToolbox

public struct H264DecodedInfo: Sendable {
    public let width: Int
    public let height: Int
    public let isInterlaced: Bool
    public let isTopFieldFirst: Bool
}

public protocol H264AccessUnitAssemblerDelegate: AnyObject, Sendable {
    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo)
    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isIDR: Bool, isTopFieldFirst: Bool)
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
    private var currentAUIsTopFieldFirst: Bool = true
    private var lastEmittedPTS: CMTime = .invalid

    private var spsData: Data?
    private var ppsData: Data?
    private var currentFormatDescription: CMVideoFormatDescription?
    public private(set) var decodedInfo: H264DecodedInfo?

    public weak var delegate: H264AccessUnitAssemblerDelegate?

    public init() {}

    public func reset() {
        streamBuffer.removeAll(keepingCapacity: true)
        currentAUNALs.removeAll(keepingCapacity: true)
        currentAUPTS = .invalid
        currentAUDTS = nil
        pendingPTS = .invalid
        pendingDTS = nil
        currentAUHasVCL = false
        currentAUHasIDR = false
        currentAUIsTopFieldFirst = true
        lastEmittedPTS = .invalid
        spsData = nil
        ppsData = nil
        currentFormatDescription = nil
        decodedInfo = nil
    }

    /// Ingests elementary stream data from a PES packet with optional PTS/DTS.
    public func feed(payload: PESVideoData) {
        if let pts = payload.pts, pts.isValid {
            pendingPTS = pts
            pendingDTS = payload.dts
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
                let nalData = streamBuffer.subdata(in: nalStart..<streamBuffer.count)
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
                    let keepFrom = streamBuffer.count - 3
                    streamBuffer = streamBuffer.subdata(in: keepFrom..<streamBuffer.count)
                }
                break
            }

            let nalStart = startCodeOffset + prefixLen
            guard let (nextStartCodeOffset, _) = findNextStartCode(in: streamBuffer, from: nalStart) else {
                // Incomplete trailing NAL in buffer; keep buffer from startCodeOffset for next chunk
                if startCodeOffset > 0 {
                    streamBuffer = streamBuffer.subdata(in: startCodeOffset..<streamBuffer.count)
                }
                break
            }

            let nalData = streamBuffer.subdata(in: nalStart..<nextStartCodeOffset)
            if !nalData.isEmpty {
                handleNALUnit(nalData)
            }

            streamBuffer = streamBuffer.subdata(in: nextStartCodeOffset..<streamBuffer.count)
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
            spsData = nal
            parseSPS(nal)
            updateFormatDescriptionIfNeeded()

        case 8: // PPS
            ppsData = nal
            updateFormatDescriptionIfNeeded()

        case 5: // IDR Slice
            currentAUHasVCL = true
            currentAUHasIDR = true
            currentAUIsTopFieldFirst = detectFieldOrder(from: nal)

        case 1: // Non-IDR Slice
            currentAUHasVCL = true
            currentAUIsTopFieldFirst = detectFieldOrder(from: nal)

        default:
            break
        }

        currentAUNALs.append(nal)
    }

    private func parseFirstMBInSlice(_ nal: Data) -> Int {
        guard nal.count > 1 else { return 0 }
        let rbsp = removeEmulationPreventionBytes(from: nal.subdata(in: 1..<min(nal.count, 6)))
        var reader = BitReader(data: rbsp)
        return reader.readExpGolomb()
    }

    private func emitCurrentAccessUnit() {
        defer {
            currentAUNALs.removeAll(keepingCapacity: true)
            currentAUHasVCL = false
            currentAUHasIDR = false
            currentAUPTS = .invalid
            currentAUDTS = nil
        }

        guard !currentAUNALs.isEmpty, currentAUHasVCL, let formatDesc = currentFormatDescription else { return }

        // Assemble AVCC buffer with 4-byte big endian length prefixes
        var avccBuffer = Data()
        for nal in currentAUNALs {
            let nalType = nal[0] & 0x1F
            if nalType == 7 || nalType == 8 {
                // SPS and PPS parameter sets are already provided in the CMVideoFormatDescription
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
            if currentAUHasIDR {
                CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(), Unmanaged.passUnretained(kCFBooleanFalse).toOpaque())
            } else {
                CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
            }
            CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
        }

        delegate?.accessUnitAssembler(self, didEmitSampleBuffer: sb, isIDR: currentAUHasIDR, isTopFieldFirst: currentAUIsTopFieldFirst)
    }

    private func updateFormatDescriptionIfNeeded() {
        guard let sps = spsData, let pps = ppsData else { return }

        let spsArray = [UInt8](sps)
        let ppsArray = [UInt8](pps)

        var formatDesc: CMVideoFormatDescription?
        let status = spsArray.withUnsafeBufferPointer { spsPtr in
            ppsArray.withUnsafeBufferPointer { ppsPtr in
                let pointers = [spsPtr.baseAddress!, ppsPtr.baseAddress!]
                let sizes = [spsArray.count, ppsArray.count]
                return CMVideoFormatDescriptionCreateFromH264ParameterSets(
                    allocator: kCFAllocatorDefault,
                    parameterSetCount: 2,
                    parameterSetPointers: pointers,
                    parameterSetSizes: sizes,
                    nalUnitHeaderLength: 4,
                    formatDescriptionOut: &formatDesc
                )
            }
        }

        if status == noErr, let fd = formatDesc {
            self.currentFormatDescription = fd
            if let info = self.decodedInfo {
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
        let rbsp = removeEmulationPreventionBytes(from: nal.subdata(in: 1..<nal.count))
        var reader = BitReader(data: rbsp)

        let profileIDC = reader.readBits(8)
        _ = reader.readBits(8) // constraint flags
        _ = reader.readBits(8) // level_idc
        _ = reader.readExpGolomb() // seq_parameter_set_id

        var chromaFormatIDC = 1
        if profileIDC == 100 || profileIDC == 110 || profileIDC == 122 || profileIDC == 244 || profileIDC == 44 || profileIDC == 83 || profileIDC == 86 || profileIDC == 118 || profileIDC == 128 {
            chromaFormatIDC = reader.readExpGolomb()
            if chromaFormatIDC == 3 {
                _ = reader.readBits(1) // separate_colour_plane_flag
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

        _ = reader.readExpGolomb() // log2_max_frame_num_minus4
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

    private func detectFieldOrder(from nal: Data) -> Bool {
        // DVB 1080i HD broadcast standard is Top Field First (TFF).
        return true
    }
}

// MARK: - BitReader for SPS exponential golomb parsing

private struct BitReader {
    private let data: Data
    private var byteOffset: Int = 0
    private var bitOffset: Int = 0

    init(data: Data) {
        self.data = data
    }

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
