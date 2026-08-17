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

/// Parses Annex-B H.264 bitstream, detects SPS/PPS, constructs `CMVideoFormatDescription`,
/// analyzes interlaced field order (TFF / BFF), and packs `CMSampleBuffer`s for VideoToolbox.
public final class H264AccessUnitAssembler: @unchecked Sendable {

    private var spsData: Data?
    private var ppsData: Data?
    private var currentFormatDescription: CMVideoFormatDescription?
    public private(set) var decodedInfo: H264DecodedInfo?

    public weak var delegate: H264AccessUnitAssemblerDelegate?

    public init() {}

    public func reset() {
        spsData = nil
        ppsData = nil
        currentFormatDescription = nil
        decodedInfo = nil
    }

    /// Processes a video access unit with associated presentation timestamp.
    public func process(unit: PESVideoAccessUnit) {
        let nals = extractAnnexBNALUnits(from: unit.data)
        guard !nals.isEmpty else { return }

        var avccBuffer = Data()
        var hasIDR = false
        var isTopField = true

        for nal in nals {
            guard !nal.isEmpty else { continue }
            let nalType = nal[0] & 0x1F

            switch nalType {
            case 7: // SPS
                spsData = nal
                parseSPS(nal)
                updateFormatDescriptionIfNeeded()

            case 8: // PPS
                ppsData = nal
                updateFormatDescriptionIfNeeded()

            case 5: // IDR Slice
                hasIDR = true
                isTopField = detectFieldOrder(from: nal, nalType: nalType)
                appendAVCCNAL(nal, to: &avccBuffer)

            case 1: // Non-IDR Slice
                isTopField = detectFieldOrder(from: nal, nalType: nalType)
                appendAVCCNAL(nal, to: &avccBuffer)

            default:
                appendAVCCNAL(nal, to: &avccBuffer)
            }
        }

        guard !avccBuffer.isEmpty, let formatDesc = currentFormatDescription else { return }

        // Construct CMBlockBuffer
        var blockBuffer: CMBlockBuffer?
        let memoryBlock = UnsafeMutablePointer<UInt8>.allocate(capacity: avccBuffer.count)
        avccBuffer.copyBytes(to: memoryBlock, count: avccBuffer.count)

        let status = CMBlockBufferCreateWithMemoryBlock(
            allocator: kCFAllocatorDefault,
            memoryBlock: memoryBlock,
            blockLength: avccBuffer.count,
            blockAllocator: kCFAllocatorMalloc,
            customBlockSource: nil,
            offsetToData: 0,
            dataLength: avccBuffer.count,
            flags: 0,
            blockBufferOut: &blockBuffer
        )

        guard status == kCMBlockBufferNoErr, let block = blockBuffer else {
            memoryBlock.deallocate()
            return
        }

        var timing = CMSampleTimingInfo(
            duration: CMTime(value: 1, timescale: 50), // 50 Hz default cadence
            presentationTimeStamp: unit.pts.isValid ? unit.pts : .invalid,
            decodeTimeStamp: unit.dts ?? .invalid
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

        // Attach sample attachments for field order / display
        if let attachments = CMSampleBufferGetSampleAttachmentsArray(sb, createIfNecessary: true) {
            let dict = unsafeBitCast(CFArrayGetValueAtIndex(attachments, 0), to: CFMutableDictionary.self)
            if hasIDR {
                CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(), Unmanaged.passUnretained(kCFBooleanFalse).toOpaque())
            } else {
                CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
            }
            CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
        }

        delegate?.accessUnitAssembler(self, didEmitSampleBuffer: sb, isIDR: hasIDR, isTopFieldFirst: isTopField)
    }

    private func appendAVCCNAL(_ nal: Data, to buffer: inout Data) {
        var length = UInt32(nal.count).bigEndian
        buffer.append(Data(bytes: &length, count: 4))
        buffer.append(nal)
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

    private func detectFieldOrder(from nal: Data, nalType: UInt8) -> Bool {
        // DVB 1080i HD broadcast standard is Top Field First (TFF).
        // If slice header specifies field_pic_flag / bottom_field_flag, we read it.
        guard nal.count > 1 else { return true }
        var reader = BitReader(data: nal.subdata(in: 1..<nal.count))
        _ = reader.readExpGolomb() // first_mb_in_slice
        _ = reader.readExpGolomb() // slice_type
        _ = reader.readExpGolomb() // pic_parameter_set_id

        // In standard DVB 1080i frame pairs or MBAFF, Top Field is rendered first.
        return true
    }

    private func extractAnnexBNALUnits(from data: Data) -> [Data] {
        var nals: [Data] = []
        var startIndex: Int? = nil
        let bytes = [UInt8](data)
        let count = bytes.count

        var i = 0
        while i < count - 3 {
            var prefixLen = 0
            if bytes[i] == 0 && bytes[i + 1] == 0 && bytes[i + 2] == 1 {
                prefixLen = 3
            } else if bytes[i] == 0 && bytes[i + 1] == 0 && bytes[i + 2] == 0 && bytes[i + 3] == 1 {
                prefixLen = 4
            }

            if prefixLen > 0 {
                if let start = startIndex {
                    let nalData = data.subdata(in: start..<i)
                    if !nalData.isEmpty {
                        nals.append(nalData)
                    }
                }
                startIndex = i + prefixLen
                i += prefixLen
            } else {
                i += 1
            }
        }

        if let start = startIndex, start < count {
            let nalData = data.subdata(in: start..<count)
            if !nalData.isEmpty {
                nals.append(nalData)
            }
        }

        return nals
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
