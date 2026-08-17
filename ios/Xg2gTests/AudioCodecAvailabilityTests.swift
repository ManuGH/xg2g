// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AudioToolbox
import CoreMedia
import Testing

/// Which audio formats this platform can actually decode.
///
/// The satellite feed carries AC-3 on every channel checked (Sky Sport Top Event
/// PIDs 770/772, PULS 24 HD PID 1283), so whether the native player can have
/// sound at all depends on the system owning an AC-3 decoder. Asserting that
/// from documentation is not good enough — the answer decides whether audio can
/// come straight from the receiver or has to be transcoded upstream.
struct AudioCodecAvailabilityTests {

    private func decodableFormatIDs() -> Set<AudioFormatID> {
        var size: UInt32 = 0
        guard AudioFormatGetPropertyInfo(
            kAudioFormatProperty_DecodeFormatIDs, 0, nil, &size
        ) == noErr, size > 0 else {
            return []
        }

        var ids = [AudioFormatID](repeating: 0, count: Int(size) / MemoryLayout<AudioFormatID>.size)
        guard AudioFormatGetProperty(
            kAudioFormatProperty_DecodeFormatIDs, 0, nil, &size, &ids
        ) == noErr else {
            return []
        }
        return Set(ids)
    }

    private func fourCC(_ id: AudioFormatID) -> String {
        let bytes = [UInt8((id >> 24) & 0xFF), UInt8((id >> 16) & 0xFF),
                     UInt8((id >> 8) & 0xFF), UInt8(id & 0xFF)]
        return String(bytes: bytes, encoding: .ascii) ?? "\(id)"
    }

    @Test func reportsWhichBroadcastAudioFormatsDecode() {
        let ids = decodableFormatIDs()
        #expect(!ids.isEmpty, "platform reported no decodable formats at all")

        let candidates: [(String, AudioFormatID)] = [
            ("AC-3", kAudioFormatAC3),
            ("AC-3 (MPEG-TS variant)", kAudioFormatEnhancedAC3),
            ("AAC", kAudioFormatMPEG4AAC),
            ("MPEG Layer II", kAudioFormatMPEGLayer2),
            ("MPEG Layer III", kAudioFormatMPEGLayer3)
        ]

        for (name, id) in candidates {
            print("[audio-codec] \(name) (\(fourCC(id))): \(ids.contains(id) ? "DECODABLE" : "not available")")
        }
        print("[audio-codec] full list: \(ids.sorted().map(fourCC).joined(separator: " "))")
    }

    /// A format description is what AVSampleBufferAudioRenderer is handed, so it
    /// has to be constructible for AC-3 before anything downstream can work.
    @Test func canBuildAnAC3FormatDescription() {
        var asbd = AudioStreamBasicDescription(
            mSampleRate: 48000,
            mFormatID: kAudioFormatAC3,
            mFormatFlags: 0,
            mBytesPerPacket: 0,
            mFramesPerPacket: 1536,   // AC-3 syncframe
            mBytesPerFrame: 0,
            mChannelsPerFrame: 2,
            mBitsPerChannel: 0,
            mReserved: 0
        )

        var format: CMAudioFormatDescription?
        let status = CMAudioFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            asbd: &asbd,
            layoutSize: 0,
            layout: nil,
            magicCookieSize: 0,
            magicCookie: nil,
            extensions: nil,
            formatDescriptionOut: &format
        )

        print("[audio-codec] CMAudioFormatDescriptionCreate(AC-3) -> \(status)")
        #expect(status == noErr, "could not describe AC-3 to CoreMedia")
        #expect(format != nil)
    }
}
