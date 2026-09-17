// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import VideoToolbox

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "video-decoders")

/// What VideoToolbox will actually decode here, asked rather than assumed.
///
/// The codec constants exist on every platform; the decoders behind them do
/// not. `kCMVideoCodecType_MPEG2Video` compiles fine on iOS and no MPEG-2
/// decoder has ever shipped with it, while HEVC is present on every device
/// since the A9 and absent from the Simulator. A table written from either
/// observation would be wrong on the other.
///
/// So the question is put to VideoToolbox: open a decompression session for the
/// codec and see whether it succeeds. Cached, because that is not a cheap call
/// and the answer cannot change while the app runs.
enum VideoDecoderAvailability {

    /// Cache and its lock, held together so the mutable state has one owner.
    private final class Store: @unchecked Sendable {
        private let lock = NSLock()
        private var answers: [CMVideoCodecType: Bool] = [:]

        func cached(_ codecType: CMVideoCodecType) -> Bool? {
            lock.lock()
            defer { lock.unlock() }
            return answers[codecType]
        }

        func remember(_ supported: Bool, for codecType: CMVideoCodecType) {
            lock.lock()
            answers[codecType] = supported
            lock.unlock()
        }
    }

    private static let store = Store()

    /// Whether a decompression session can be opened for this codec.
    static func canDecode(_ codecType: CMVideoCodecType) -> Bool {
        if let cached = store.cached(codecType) { return cached }

        let supported = probe(codecType)
        store.remember(supported, for: codecType)

        let msg = "[VideoDecoders] \(fourCC(codecType)): \(supported ? "available" : "unavailable")"
        logger.notice("\(msg, privacy: .public)")
        return supported
    }

    /// Opens and immediately discards a session, which is the only reliable
    /// answer: `VTIsHardwareDecodeSupported` reports hardware alone and says
    /// nothing about a software decoder that would serve just as well.
    private static func probe(_ codecType: CMVideoCodecType) -> Bool {
        var format: CMVideoFormatDescription?
        guard CMVideoFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            codecType: codecType,
            width: 1920,
            height: 1080,
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

    private static func fourCC(_ code: CMVideoCodecType) -> String {
        let bytes = [
            UInt8((code >> 24) & 0xFF),
            UInt8((code >> 16) & 0xFF),
            UInt8((code >> 8) & 0xFF),
            UInt8(code & 0xFF)
        ]
        return String(bytes: bytes, encoding: .ascii) ?? String(code)
    }
}
