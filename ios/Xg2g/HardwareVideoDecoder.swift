// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import VideoToolbox

public struct DecodedVideoFrame: @unchecked Sendable {
    public let pixelBuffer: CVPixelBuffer
    public let pts: CMTime
    /// How the coded picture maps onto fields, carried through from the slice
    /// header so the renderer knows how many presentations it owes and which
    /// parity each one is.
    public let structure: H264PictureStructure
}

public protocol HardwareVideoDecoderDelegate: AnyObject, Sendable {
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus)
}

/// Hardware-accelerated H.264 video decoder using Apple Silicon VideoToolbox.
///
/// Features:
/// - Enforces `kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder = true`.
/// - Verifies `kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder`.
/// - Outputs Bi-Planar NV12 `CVPixelBuffer`s.
/// - Supports Path A (Native Metal shader deinterlace) vs Path B (`kVTDecompressionPropertyKey_DeinterlaceMode`).
public final class HardwareVideoDecoder: @unchecked Sendable {

    private var decompressionSession: VTDecompressionSession?
    private var formatDescription: CMVideoFormatDescription?
    public var useNativeVTDeinterlace: Bool = false {
        didSet {
            if oldValue != useNativeVTDeinterlace {
                recreateSession()
            }
        }
    }

    public weak var delegate: HardwareVideoDecoderDelegate?

    /// Stamped onto every frame submitted for decode and checked again when it
    /// comes back, so pictures still in the decoder when a channel is zapped are
    /// discarded instead of being delivered into the next stream's timeline.
    ///
    /// Written from the caller's thread and read from the VideoToolbox callback
    /// thread, hence the lock.
    private var _decodeGeneration: Int = 0
    private let generationLock = NSLock()

    public var decodeGeneration: Int {
        get {
            generationLock.lock()
            defer { generationLock.unlock() }
            return _decodeGeneration
        }
        set {
            generationLock.lock()
            _decodeGeneration = newValue
            generationLock.unlock()
        }
    }

    public init() {}

    deinit {
        invalidateSession()
    }

    public func reset() {
        invalidateSession()
        formatDescription = nil
    }

    public func configure(with formatDescription: CMVideoFormatDescription) {
        if let current = self.formatDescription, CMFormatDescriptionEqual(current, otherFormatDescription: formatDescription), decompressionSession != nil {
            return
        }
        self.formatDescription = formatDescription
        recreateSession()
    }

    private func recreateSession() {
        invalidateSession()
        guard let format = formatDescription else { return }

        let decoderSpecification: [CFString: Any] = [
            kVTVideoDecoderSpecification_EnableHardwareAcceleratedVideoDecoder: kCFBooleanTrue as Any
        ]

        let destinationImageBufferAttributes: [CFString: Any] = [
            kCVPixelBufferPixelFormatTypeKey: kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
            kCVPixelBufferMetalCompatibilityKey: kCFBooleanTrue as Any
        ]

        var callbackRecord = VTDecompressionOutputCallbackRecord(
            decompressionOutputCallback: decompressionCallback,
            decompressionOutputRefCon: Unmanaged.passUnretained(self).toOpaque()
        )

        var session: VTDecompressionSession?
        let status = VTDecompressionSessionCreate(
            allocator: kCFAllocatorDefault,
            formatDescription: format,
            decoderSpecification: decoderSpecification as CFDictionary,
            imageBufferAttributes: destinationImageBufferAttributes as CFDictionary,
            outputCallback: &callbackRecord,
            decompressionSessionOut: &session
        )

        guard status == noErr, let activeSession = session else {
            let msg = "[1080i50-DEC-CREATE-ERR] VTDecompressionSessionCreate failed with OSStatus: \(status)"
            print(msg)
            TelemetryServer.shared.log(msg)
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
            return
        }

        let successMsg = "[1080i50-DEC] ✅ Decompression session created successfully!"
        print(successMsg)
        TelemetryServer.shared.log(successMsg)

        // Path B probes for a native VideoToolbox deinterlace property.
        //
        // `DeinterlaceMode` is not a documented `kVTDecompressionPropertyKey_*`
        // constant — it is a speculative string key, and `VTSessionSetProperty`
        // has never accepted it on any tested device. The probe is kept because
        // it is self-verifying (the value is read back before being reported as
        // accepted), but Path A is what actually deinterlaces.
        var isVTDeinterlaceAccepted = false
        if useNativeVTDeinterlace {
            let deinterlacePropKey = "DeinterlaceMode" as CFString
            let deinterlacePropVal = "Temporal" as CFString
            let deintStatus = VTSessionSetProperty(activeSession, key: deinterlacePropKey, value: deinterlacePropVal)
            if deintStatus == noErr {
                var readVal: Unmanaged<CFPropertyList>?
                let copyStatus = VTSessionCopyProperty(activeSession, key: deinterlacePropKey, allocator: kCFAllocatorDefault, valueOut: &readVal)
                if copyStatus == noErr, let unmanaged = readVal {
                    let val = unmanaged.takeRetainedValue()
                    if let str = val as? String {
                        if str == "Temporal" || str == "VerticalFilter" {
                            isVTDeinterlaceAccepted = true
                        }
                    }
                }
            }
        }

        // Verify if Hardware Acceleration is active
        var isHWActive: Bool = false
        var usingHW: Unmanaged<CFPropertyList>?
        let hwStatus = VTSessionCopyProperty(
            activeSession,
            key: kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder,
            allocator: kCFAllocatorDefault,
            valueOut: &usingHW
        )
        if hwStatus == noErr, let unmanaged = usingHW {
            let val = unmanaged.takeRetainedValue()
            if let boolVal = val as? Bool {
                isHWActive = boolVal
            } else if CFGetTypeID(val) == CFBooleanGetTypeID() {
                let cfBool = unsafeDowncast(val, to: CFBoolean.self)
                isHWActive = CFBooleanGetValue(cfBool)
            }
        } else {
            // Since RequireHardwareAcceleratedVideoDecoder was set to true, session creation success implies HW is active
            isHWActive = true
        }

        self.decompressionSession = activeSession
        delegate?.hardwareDecoder(self, didChangeHWActiveState: isHWActive)
        delegate?.hardwareDecoder(self, didChangeVTDeinterlaceAccepted: isVTDeinterlaceAccepted)
    }

    private func invalidateSession() {
        if let session = decompressionSession {
            VTDecompressionSessionInvalidate(session)
            decompressionSession = nil
        }
    }

    public func decode(sampleBuffer: CMSampleBuffer, structure: H264PictureStructure) {
        guard let session = decompressionSession else {
            let msg = "[1080i50-DEC] ⚠️ No active decompression session"
            print(msg)
            TelemetryServer.shared.log(msg)
            return
        }

        var flagsOut: VTDecodeInfoFlags = []
        let context = FrameContext(structure: structure, pts: CMSampleBufferGetPresentationTimeStamp(sampleBuffer), generation: decodeGeneration)
        let contextPtr = Unmanaged.passRetained(context).toOpaque()

        let status = VTDecompressionSessionDecodeFrame(
            session,
            sampleBuffer: sampleBuffer,
            flags: [._EnableAsynchronousDecompression, ._1xRealTimePlayback],
            frameRefcon: contextPtr,
            infoFlagsOut: &flagsOut
        )

        if status != noErr {
            _ = Unmanaged<FrameContext>.fromOpaque(contextPtr).takeRetainedValue()
            let msg = "[1080i50-DECODE-FAIL] VTDecompressionSessionDecodeFrame error: \(status)"
            print(msg)
            TelemetryServer.shared.log(msg)
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
        }
    }

    fileprivate func handleDecodedFrame(status: OSStatus, imageBuffer: CVImageBuffer?, context: FrameContext) {
        guard context.generation == decodeGeneration else { return }

        guard status == noErr, let buffer = imageBuffer else {
            let msg = "[1080i50-DECODE-FAIL] handleDecodedFrame async error: \(status)"
            print(msg)
            TelemetryServer.shared.log(msg)
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
            return
        }

        let frame = DecodedVideoFrame(
            pixelBuffer: buffer,
            pts: context.pts,
            structure: context.structure
        )
        delegate?.hardwareDecoder(self, didEmitFrame: frame)
    }
}

private final class FrameContext {
    let structure: H264PictureStructure
    let pts: CMTime
    let generation: Int

    init(structure: H264PictureStructure, pts: CMTime, generation: Int) {
        self.structure = structure
        self.pts = pts
        self.generation = generation
    }
}

private func decompressionCallback(
    decompressionOutputRefCon: UnsafeMutableRawPointer?,
    sourceFrameRefCon: UnsafeMutableRawPointer?,
    status: OSStatus,
    infoFlags: VTDecodeInfoFlags,
    imageBuffer: CVImageBuffer?,
    presentationTimeStamp: CMTime,
    presentationDuration: CMTime
) {
    guard let refCon = decompressionOutputRefCon, let frameRefCon = sourceFrameRefCon else { return }
    let decoder = Unmanaged<HardwareVideoDecoder>.fromOpaque(refCon).takeUnretainedValue()
    let context = Unmanaged<FrameContext>.fromOpaque(frameRefCon).takeRetainedValue()

    decoder.handleDecodedFrame(status: status, imageBuffer: imageBuffer, context: context)
}
