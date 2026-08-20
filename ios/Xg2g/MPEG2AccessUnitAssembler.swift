// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import VideoToolbox

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "mpeg2")

/// Stream-level MPEG-2 video access unit assembler (ISO/IEC 13818-2).
///
/// Same contract as the H.264 and HEVC assemblers, different in almost every
/// detail beneath it. MPEG-2 has no NAL units: the elementary stream is a run
/// of start codes, and what separates one picture from the next is a picture
/// start code rather than a flag inside a slice header.
///
/// It also hands VideoToolbox a different shape. H.264 and HEVC samples are
/// length-prefixed with their parameter sets stripped out; an MPEG-2 sample is
/// the elementary stream verbatim, start codes and sequence header included,
/// because the decoder reads its parameters from the stream itself rather than
/// from the format description.
///
/// A picture's headers precede it, so a sequence header or GOP header closes
/// the *previous* access unit and opens the next — appending it to the picture
/// it follows would hand the decoder a sequence header describing a picture it
/// has already been given.
public final class MPEG2AccessUnitAssembler: @unchecked Sendable, VideoAccessUnitAssembling {

    // MARK: Start codes (ISO/IEC 13818-2 Table 6-1)

    private enum StartCode {
        static let picture: UInt8 = 0x00
        static let sequenceHeader: UInt8 = 0xB3
        static let sequenceError: UInt8 = 0xB4
        static let extensionStart: UInt8 = 0xB5
        static let sequenceEnd: UInt8 = 0xB7
        static let groupStart: UInt8 = 0xB8

        /// Slice start codes run 0x01…0xAF and carry the coded macroblocks.
        static func isSlice(_ code: UInt8) -> Bool { code >= 0x01 && code <= 0xAF }
    }

    /// Extension identifiers, in the four bits following an extension start code.
    private enum ExtensionID {
        static let sequence: UInt8 = 1
        static let pictureCoding: UInt8 = 8
    }

    /// `picture_coding_type`, ISO/IEC 13818-2 Table 6-12.
    private enum PictureCodingType {
        static let intra: UInt8 = 1
    }

    /// `picture_structure`, ISO/IEC 13818-2 Table 6-14.
    private enum PictureStructure {
        static let topField: UInt8 = 1
        static let bottomField: UInt8 = 2
        static let frame: UInt8 = 3
    }

    public weak var assemblerDelegate: VideoAccessUnitAssemblerDelegate?

    private var streamBuffer = Data()

    /// Bytes of the access unit being assembled, elementary stream verbatim.
    private var currentAU = Data()
    private var currentAUHasPicture = false
    private var currentAUIsIntra = false
    private var currentAUPTS: CMTime = .invalid
    private var currentAUDTS: CMTime?
    private var pendingPTS: CMTime = .invalid
    private var pendingDTS: CMTime?
    private var currentStructure: H264PictureStructure = .wovenTopFieldFirst

    private var width = 0
    private var height = 0
    private var isProgressiveSequence = false
    private var currentFormatDescription: CMVideoFormatDescription?

    public init() {}

    public func reset() {
        streamBuffer.removeAll(keepingCapacity: true)
        currentAU.removeAll(keepingCapacity: true)
        currentAUHasPicture = false
        currentAUIsIntra = false
        currentAUPTS = .invalid
        currentAUDTS = nil
        pendingPTS = .invalid
        pendingDTS = nil
        currentStructure = .wovenTopFieldFirst
        width = 0
        height = 0
        isProgressiveSequence = false
        currentFormatDescription = nil
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

    /// Ends the stream: the segment still buffered is complete now, since
    /// nothing further can arrive to extend it.
    ///
    /// Without this the last picture of a stream was dropped — a segment is
    /// only handed on once the *next* start code bounds it, and at the end
    /// there is no next.
    public func flush() {
        if let cursor = findStartCode(in: streamBuffer, from: 0) {
            let base = streamBuffer.startIndex
            let segment = streamBuffer.subdata(in: (base + cursor.offset)..<streamBuffer.endIndex)
            streamBuffer.removeAll(keepingCapacity: true)
            handleSegment(segment, prefixLength: cursor.prefixLength)
        }
        emitCurrentAccessUnit()
    }

    // MARK: - Start code splitting

    /// Consumes complete start-code segments, keeping the trailing partial one.
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
            if streamBuffer.count > 4 {
                consume(streamBuffer.count - 3)
            }
            return
        }

        // Segments are collected before anything is handed on: `handleSegment`
        // can emit, and an emit must not run while the buffer is being walked.
        var segments: [(data: Data, prefixLength: Int)] = []
        while let next = findStartCode(in: streamBuffer, from: cursor.offset + cursor.prefixLength) {
            let base = streamBuffer.startIndex
            let segment = streamBuffer.subdata(in: (base + cursor.offset)..<(base + next.offset))
            segments.append((segment, cursor.prefixLength))
            cursor = next
        }

        consume(cursor.offset)

        for segment in segments {
            handleSegment(segment.data, prefixLength: segment.prefixLength)
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

    /// Handles one start-code segment: the start code, its identifier, and the
    /// payload up to the next start code.
    private func handleSegment(_ segment: Data, prefixLength: Int) {
        guard segment.count > prefixLength else { return }
        let code = segment[segment.startIndex + prefixLength]
        let payload = segment.subdata(in: (segment.startIndex + prefixLength + 1)..<segment.endIndex)

        // Sequence and GOP headers introduce what follows them, so they close
        // the picture already in hand rather than joining it.
        let opensNewAccessUnit = code == StartCode.picture
            || code == StartCode.sequenceHeader
            || code == StartCode.groupStart

        if opensNewAccessUnit && currentAUHasPicture {
            emitCurrentAccessUnit()
        }

        switch code {
        case StartCode.sequenceHeader:
            parseSequenceHeader(payload)
        case StartCode.extensionStart:
            parseExtension(payload)
        case StartCode.picture:
            parsePictureHeader(payload)
            if !currentAUHasPicture {
                currentAUPTS = pendingPTS
                currentAUDTS = pendingDTS
            }
            currentAUHasPicture = true
        case StartCode.sequenceEnd, StartCode.sequenceError:
            break
        default:
            break
        }

        currentAU.append(segment)
    }

    private func emitCurrentAccessUnit() {
        defer {
            currentAU.removeAll(keepingCapacity: true)
            currentAUHasPicture = false
            currentAUIsIntra = false
            currentAUPTS = .invalid
            currentAUDTS = nil
            currentStructure = .wovenTopFieldFirst
        }

        guard currentAUHasPicture, !currentAU.isEmpty else { return }
        guard let formatDesc = ensureFormatDescription() else { return }

        guard let sampleBuffer = HEVCAccessUnitAssembler.makeSampleBuffer(
            from: currentAU,
            formatDescription: formatDesc,
            pts: currentAUPTS,
            dts: currentAUDTS,
            isSyncSample: currentAUIsIntra
        ) else { return }

        assemblerDelegate?.videoAssembler(
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: currentAUIsIntra,
            structure: currentStructure
        )
    }

    // MARK: - Header parsing

    /// `sequence_header`: 12 bits horizontal size, 12 bits vertical size.
    private func parseSequenceHeader(_ payload: Data) {
        guard payload.count >= 3 else { return }
        let b = [UInt8](payload.prefix(3))
        let horizontal = (Int(b[0]) << 4) | (Int(b[1]) >> 4)
        let vertical = ((Int(b[1]) & 0x0F) << 8) | Int(b[2])
        guard horizontal > 0, vertical > 0 else { return }

        if horizontal != width || vertical != height {
            width = horizontal
            height = vertical
            // Dimensions decide the format description, so it has to be rebuilt
            // rather than kept describing the previous picture size.
            currentFormatDescription = nil
        }
    }

    /// Extensions share one start code and are told apart by their first four bits.
    private func parseExtension(_ payload: Data) {
        guard let first = payload.first else { return }
        let identifier = (first >> 4) & 0x0F

        switch identifier {
        case ExtensionID.sequence:
            // sequence_extension: progressive_sequence is bit 3 of the second byte,
            // after profile_and_level_indication spans the byte boundary.
            guard payload.count >= 2 else { return }
            isProgressiveSequence = (payload[payload.startIndex + 1] >> 3) & 0x01 == 1
        case ExtensionID.pictureCoding:
            parsePictureCodingExtension(payload)
        default:
            break
        }
    }

    /// `picture_coding_extension`: picture structure and field order.
    ///
    /// Read from three bytes, with the fourth taken only when it is there.
    /// MPEG-2 allows any number of leading zero bytes before a start code, so a
    /// trailing zero in this extension is indistinguishable from stuffing and is
    /// consumed as part of the next start code — which is correct, and would
    /// otherwise cost the picture structure for every bottom-field picture,
    /// since `top_field_first` is zero there and the byte carrying it vanishes.
    private func parsePictureCodingExtension(_ payload: Data) {
        guard payload.count >= 3 else { return }
        let structureBits = payload[payload.startIndex + 2] & 0x03
        let topFieldFirst = payload.count >= 4
            ? (payload[payload.startIndex + 3] >> 7) & 0x01 == 1
            : false

        switch structureBits {
        case PictureStructure.topField:
            currentStructure = H264PictureStructure(
                isFieldPicture: true, isBottomField: false, isTopFieldFirst: true
            )
        case PictureStructure.bottomField:
            currentStructure = H264PictureStructure(
                isFieldPicture: true, isBottomField: true, isTopFieldFirst: false
            )
        case PictureStructure.frame:
            currentStructure = H264PictureStructure(
                isFieldPicture: false, isBottomField: false, isTopFieldFirst: topFieldFirst
            )
        default:
            currentStructure = .wovenTopFieldFirst
        }
    }

    /// `picture_header`: 10 bits temporal reference, then 3 bits coding type.
    private func parsePictureHeader(_ payload: Data) {
        guard payload.count >= 2 else { return }
        let codingType = (payload[payload.startIndex + 1] >> 3) & 0x07
        currentAUIsIntra = codingType == PictureCodingType.intra
    }

    // MARK: - Format description

    /// Builds the format description from the sequence header's dimensions.
    ///
    /// Nothing else is needed: an MPEG-2 decoder reads its parameters out of
    /// the elementary stream it is handed, so unlike H.264 and HEVC there are
    /// no parameter sets to carry here.
    private func ensureFormatDescription() -> CMVideoFormatDescription? {
        if let existing = currentFormatDescription { return existing }
        guard width > 0, height > 0 else { return nil }

        var formatDesc: CMVideoFormatDescription?
        let status = CMVideoFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            codecType: kCMVideoCodecType_MPEG2Video,
            width: Int32(width),
            height: Int32(height),
            extensions: nil,
            formatDescriptionOut: &formatDesc
        )

        guard status == noErr, let fd = formatDesc else {
            logger.error("[MPEG2] format description rejected: \(status)")
            return nil
        }

        currentFormatDescription = fd
        let info = H264DecodedInfo(
            width: width,
            height: height,
            isInterlaced: !isProgressiveSequence,
            isTopFieldFirst: currentStructure.isTopFieldFirst
        )
        let msg = "[MPEG2] format \(width)x\(height) \(isProgressiveSequence ? "progressive" : "interlaced")"
        logger.notice("\(msg, privacy: .public)")
        assemblerDelegate?.videoAssembler(didUpdateFormat: fd, info: info)
        return fd
    }
}
