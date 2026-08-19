// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AudioToolbox
import CoreMedia
import Foundation

public protocol AudioSampleBufferAssemblerDelegate: AnyObject, Sendable {
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, codec: AudioStreamCodec, sampleRate: Int, channels: Int, bitrateKbps: Int)
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, codec: AudioStreamCodec, duration: CMTime)
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String)
}

/// Converts parsed AC-3, E-AC-3, and AAC audio syncframes into valid CoreMedia `CMSampleBuffer`s.
public final class AudioSampleBufferAssembler: @unchecked Sendable, AC3FrameParserDelegate, AACFrameParserDelegate {

    public private(set) var currentFormatDescription: CMAudioFormatDescription?
    public private(set) var currentAC3FrameInfo: AC3FrameInfo?
    public private(set) var currentAACFrameInfo: AACFrameInfo?

    public weak var delegate: AudioSampleBufferAssemblerDelegate?

    public init() {}

    public func reset() {
        currentFormatDescription = nil
        currentAC3FrameInfo = nil
        currentAACFrameInfo = nil
    }

    // MARK: - AC3FrameParserDelegate

    public func ac3FrameParser(_ parser: AC3FrameParser, didEmitFrame frame: ParsedAudioFrame) {
        let info = frame.info

        // Update format description if audio properties changed
        if currentFormatDescription == nil || currentAC3FrameInfo != info {
            if let newFormat = createAC3FormatDescription(for: info) {
                self.currentFormatDescription = newFormat
                self.currentAC3FrameInfo = info
                self.currentAACFrameInfo = nil
                let codec: AudioStreamCodec = info.isEnhanced ? .eac3 : .ac3
                delegate?.audioSampleBufferAssembler(self, didUpdateFormat: newFormat, codec: codec, sampleRate: info.sampleRate, channels: info.channelCount, bitrateKbps: info.bitrateKbps)
            } else {
                delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMAudioFormatDescription for \(info.isEnhanced ? "E-AC-3" : "AC-3")")
                return
            }
        }

        guard let formatDesc = currentFormatDescription else { return }

        emitSampleBuffer(data: frame.data, formatDescription: formatDesc, pts: frame.pts, codec: info.isEnhanced ? .eac3 : .ac3, duration: info.duration)
    }

    public func ac3FrameParser(_ parser: AC3FrameParser, didEncounterError reason: String) {
        delegate?.audioSampleBufferAssembler(self, didEncounterError: reason)
    }

    // MARK: - AACFrameParserDelegate

    public func aacFrameParser(_ parser: AACADTSFrameParser, didEmitFrame frame: ParsedAACFrame) {
        let info = frame.info

        if currentFormatDescription == nil || currentAACFrameInfo != info {
            if let newFormat = createAACFormatDescription(for: info) {
                self.currentFormatDescription = newFormat
                self.currentAACFrameInfo = info
                self.currentAC3FrameInfo = nil
                delegate?.audioSampleBufferAssembler(self, didUpdateFormat: newFormat, codec: .aac, sampleRate: info.sampleRate, channels: info.channelCount, bitrateKbps: 0)
            } else {
                delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMAudioFormatDescription for AAC")
                return
            }
        }

        guard let formatDesc = currentFormatDescription else { return }

        emitSampleBuffer(data: frame.rawPayload, formatDescription: formatDesc, pts: frame.pts, codec: .aac, duration: info.duration)
    }

    public func aacFrameParser(_ parser: AACADTSFrameParser, didEncounterError reason: String) {
        delegate?.audioSampleBufferAssembler(self, didEncounterError: reason)
    }

    // MARK: - SampleBuffer Construction

    private func emitSampleBuffer(data: Data, formatDescription: CMAudioFormatDescription, pts: CMTime?, codec: AudioStreamCodec, duration: CMTime) {
        var blockBuffer: CMBlockBuffer?
        let status = CMBlockBufferCreateWithMemoryBlock(
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

        guard status == noErr, let buffer = blockBuffer else {
            delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMBlockBuffer (status \(status))")
            return
        }

        data.withUnsafeBytes { rawBuffer in
            if let base = rawBuffer.baseAddress {
                CMBlockBufferReplaceDataBytes(
                    with: base,
                    blockBuffer: buffer,
                    offsetIntoDestination: 0,
                    dataLength: data.count
                )
            }
        }

        var packetDesc = AudioStreamPacketDescription(
            mStartOffset: 0,
            mVariableFramesInPacket: 0,
            mDataByteSize: UInt32(data.count)
        )

        let presentationPTS = pts ?? .invalid
        var sampleBuffer: CMSampleBuffer?
        let sampleBufferStatus = withUnsafePointer(to: &packetDesc) { descPtr in
            CMAudioSampleBufferCreateWithPacketDescriptions(
                allocator: kCFAllocatorDefault,
                dataBuffer: buffer,
                dataReady: true,
                makeDataReadyCallback: nil,
                refcon: nil,
                formatDescription: formatDescription,
                sampleCount: 1,
                presentationTimeStamp: presentationPTS,
                packetDescriptions: descPtr,
                sampleBufferOut: &sampleBuffer
            )
        }

        guard sampleBufferStatus == noErr, let sb = sampleBuffer else {
            delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMSampleBuffer (status \(sampleBufferStatus))")
            return
        }

        delegate?.audioSampleBufferAssembler(self, didEmitSampleBuffer: sb, codec: codec, duration: duration)
    }

    // MARK: - Format Description Helpers

    /// The CoreAudio channel layout for a bitstream's `acmod` / `lfeon` pair.
    ///
    /// AC-3 delivers its channels in bitstream order — L, C, R, Ls, Rs, LFE —
    /// which is what the `AC3_*` and `MPEG_*_C` tags describe. Without a layout
    /// the renderer knows the channel *count* and nothing else, so a 5.1
    /// broadcast folded down to a two-channel route (any Bluetooth headphone,
    /// AirPods included) has no way to tell which channel carries dialogue and
    /// which carries the LFE rumble. That fold-down is where the audible damage
    /// happens; the built-in speaker mostly hides it.
    ///
    /// Returns `nil` for the three combinations CoreAudio has no tag for
    /// (dual-mono+LFE, 2/0+LFE, 2/2+LFE), which fall back to discrete channels.
    static func channelLayoutTag(acmod: Int, lfeOn: Bool) -> AudioChannelLayoutTag? {
        switch (acmod, lfeOn) {
        case (0, false): return kAudioChannelLayoutTag_Stereo        // 1+1 dual mono
        case (1, false): return kAudioChannelLayoutTag_Mono          // 1/0 C
        case (1, true):  return kAudioChannelLayoutTag_AC3_1_0_1     // C, LFE
        case (2, false): return kAudioChannelLayoutTag_Stereo        // 2/0 L, R
        case (3, false): return kAudioChannelLayoutTag_AC3_3_0       // L, C, R
        case (3, true):  return kAudioChannelLayoutTag_AC3_3_0_1     // L, C, R, LFE
        case (4, false): return kAudioChannelLayoutTag_ITU_2_1       // L, R, Cs
        case (4, true):  return kAudioChannelLayoutTag_AC3_2_1_1     // L, R, Cs, LFE
        case (5, false): return kAudioChannelLayoutTag_AC3_3_1       // L, C, R, Cs
        case (5, true):  return kAudioChannelLayoutTag_AC3_3_1_1     // L, C, R, Cs, LFE
        case (6, false): return kAudioChannelLayoutTag_Quadraphonic  // L, R, Ls, Rs
        case (7, false): return kAudioChannelLayoutTag_MPEG_5_0_C    // L, C, R, Ls, Rs
        case (7, true):  return kAudioChannelLayoutTag_MPEG_5_1_C    // L, C, R, Ls, Rs, LFE
        default:         return nil
        }
    }

    private func createAC3FormatDescription(for info: AC3FrameInfo) -> CMAudioFormatDescription? {
        let formatID: AudioFormatID = info.isEnhanced ? kAudioFormatEnhancedAC3 : kAudioFormatAC3

        var asbd = AudioStreamBasicDescription(
            mSampleRate: Float64(info.sampleRate),
            mFormatID: formatID,
            mFormatFlags: 0,
            mBytesPerPacket: 0,
            mFramesPerPacket: UInt32(info.samplesPerFrame),
            mBytesPerFrame: 0,
            mChannelsPerFrame: UInt32(info.channelCount),
            mBitsPerChannel: 0,
            mReserved: 0
        )

        var layout = AudioChannelLayout()
        layout.mChannelLayoutTag = Self.channelLayoutTag(acmod: info.acmod, lfeOn: info.isLFEOn)
            ?? (kAudioChannelLayoutTag_DiscreteInOrder | UInt32(info.channelCount))

        var format: CMAudioFormatDescription?
        // Tag-based layouts carry no channel descriptions, so the bare struct
        // size is the whole layout.
        let status = CMAudioFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            asbd: &asbd,
            layoutSize: MemoryLayout<AudioChannelLayout>.size,
            layout: &layout,
            magicCookieSize: 0,
            magicCookie: nil,
            extensions: nil,
            formatDescriptionOut: &format
        )

        return status == noErr ? format : nil
    }

    private func createAACFormatDescription(for info: AACFrameInfo) -> CMAudioFormatDescription? {
        var asbd = AudioStreamBasicDescription(
            mSampleRate: Float64(info.sampleRate),
            mFormatID: kAudioFormatMPEG4AAC,
            mFormatFlags: AudioFormatFlags(MPEG4ObjectID.AAC_LC.rawValue),
            mBytesPerPacket: 0,
            mFramesPerPacket: UInt32(info.samplesPerFrame),
            mBytesPerFrame: 0,
            mChannelsPerFrame: UInt32(info.channelCount),
            mBitsPerChannel: 0,
            mReserved: 0
        )

        var format: CMAudioFormatDescription?
        let cookieData = info.audioSpecificConfig
        let status = cookieData.withUnsafeBytes { rawCookie in
            CMAudioFormatDescriptionCreate(
                allocator: kCFAllocatorDefault,
                asbd: &asbd,
                layoutSize: 0,
                layout: nil,
                magicCookieSize: cookieData.count,
                magicCookie: rawCookie.baseAddress,
                extensions: nil,
                formatDescriptionOut: &format
            )
        }

        return status == noErr ? format : nil
    }
}
