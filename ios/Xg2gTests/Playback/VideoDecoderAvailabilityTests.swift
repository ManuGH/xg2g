// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Testing
import VideoToolbox

/// Which video formats this platform can actually decode.
///
/// Written for the same reason as `AudioCodecAvailabilityTests`: an assembler
/// that cuts a stream into perfect access units is worthless if VideoToolbox
/// has no decoder to hand them to, and the failure looks identical to a broken
/// assembler from the outside. Asserting it from documentation is not good
/// enough — Apple ships an MPEG-2 decoder on macOS and the same constant exists
/// on iOS, which says nothing about whether a session can be opened here.
struct VideoDecoderAvailabilityTests {

    /// Whether a decompression session can be opened for a codec at all.
    private func canOpenSession(codecType: CMVideoCodecType, width: Int32 = 720, height: Int32 = 576) -> Bool {
        var format: CMVideoFormatDescription?
        guard CMVideoFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            codecType: codecType,
            width: width,
            height: height,
            extensions: nil,
            formatDescriptionOut: &format
        ) == noErr, let format else {
            return false
        }

        var session: VTDecompressionSession?
        let status = VTDecompressionSessionCreate(
            allocator: kCFAllocatorDefault,
            formatDescription: format,
            decoderSpecification: nil,
            imageBufferAttributes: nil,
            outputCallback: nil,
            decompressionSessionOut: &session
        )
        if let session {
            VTDecompressionSessionInvalidate(session)
        }
        return status == noErr
    }

    /// Reports what the platform offers, so the answer is on the record rather
    /// than inferred from whether playback happened to work.
    @Test("Report decoder availability for the broadcast video codecs")
    func reportAvailability() {
        let mpeg2 = canOpenSession(codecType: kCMVideoCodecType_MPEG2Video)
        let h264 = canOpenSession(codecType: kCMVideoCodecType_H264)
        let hevc = canOpenSession(codecType: kCMVideoCodecType_HEVC, width: 1920, height: 1080)

        print("[VideoDecoderAvailability] MPEG-2: \(mpeg2) | H.264: \(h264) | HEVC: \(hevc)")
        print("[VideoDecoderAvailability] hardware — MPEG-2: \(VTIsHardwareDecodeSupported(kCMVideoCodecType_MPEG2Video)) | HEVC: \(VTIsHardwareDecodeSupported(kCMVideoCodecType_HEVC))")

        // H.264 is the one certainty: the whole native path is built on it.
        #expect(h264)
    }
}
