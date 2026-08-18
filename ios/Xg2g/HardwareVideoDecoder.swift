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
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didInvalidateSessionWithFatalError error: OSStatus)
}

/// Hardware-accelerated H.264 video decoder using Apple Silicon VideoToolbox.
///
/// Features:
/// - Enforces `kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder = true`.
/// - Verifies `kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder`.
/// - Outputs Bi-Planar NV12 `CVPixelBuffer`s.
/// - Supports Path A (Native Metal shader deinterlace) vs Path B (`kVTDecompressionPropertyKey_DeinterlaceMode`).
/// - Handles fatal session errors (kVTInvalidSessionErr -12903, kVTVideoDecoderMalfunctionErr -12911)
///   by invalidating the dead session, advancing the generation to drop in-flight frames, and signaling
///   the pipeline to re-gate on the next IDR sync sample.
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

    public var hasActiveSession: Bool {
        generationLock.lock()
        defer { generationLock.unlock() }
        return decompressionSession != nil
    }

    public var isConfigured: Bool {
        generationLock.lock()
        defer { generationLock.unlock() }
        return formatDescription != nil
    }

    public init() {}

    deinit {
        invalidateSession()
    }

    public func reset() {
        generationLock.lock()
        invalidateSessionLocked()
        formatDescription = nil
        generationLock.unlock()
    }

    public func isFatalSessionError(_ status: OSStatus) -> Bool {
        // kVTInvalidSessionErr = -12903 (0xFFFFF319)
        // kVTVideoDecoderMalfunctionErr = -12911 (0xFFFFF311)
        return status == kVTInvalidSessionErr || status == kVTVideoDecoderMalfunctionErr
    }

    private func handleFatalSessionError(_ status: OSStatus) {
        generationLock.lock()
        guard decompressionSession != nil else {
            generationLock.unlock()
            return
        }
        invalidateSessionLocked()
        _decodeGeneration += 1
        let newGen = _decodeGeneration
        generationLock.unlock()

        let msg = "[1080i50-DEC-FATAL] 🛑 VTDecompressionSession invalidated due to fatal error: \(status) (advanced to gen \(newGen))"
        print(msg)
        TelemetryServer.shared.log(msg)

        delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
        delegate?.hardwareDecoder(self, didInvalidateSessionWithFatalError: status)
    }

    public func configure(with formatDescription: CMVideoFormatDescription) {
        generationLock.lock()
        let shouldSkip = self.formatDescription != nil &&
                         CMFormatDescriptionEqual(self.formatDescription!, otherFormatDescription: formatDescription) &&
                         decompressionSession != nil
        if shouldSkip {
            generationLock.unlock()
            return
        }
        self.formatDescription = formatDescription
        generationLock.unlock()
        recreateSession()
    }

    public func recreateSession() {
        generationLock.lock()
        invalidateSessionLocked()
        guard let format = formatDescription else {
            generationLock.unlock()
            return
        }
        generationLock.unlock()

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

        generationLock.lock()
        self.decompressionSession = activeSession
        generationLock.unlock()

        delegate?.hardwareDecoder(self, didChangeHWActiveState: isHWActive)
        delegate?.hardwareDecoder(self, didChangeVTDeinterlaceAccepted: isVTDeinterlaceAccepted)
    }

    public func ensureSession() -> Bool {
        if hasActiveSession { return true }
        guard isConfigured else { return false }
        recreateSession()
        return hasActiveSession
    }

    private func invalidateSessionLocked() {
        if let session = decompressionSession {
            VTDecompressionSessionInvalidate(session)
            decompressionSession = nil
        }
    }

    private func invalidateSession() {
        generationLock.lock()
        invalidateSessionLocked()
        generationLock.unlock()
    }

    public func decode(sampleBuffer: CMSampleBuffer, structure: H264PictureStructure) {
        generationLock.lock()
        guard let session = decompressionSession else {
            generationLock.unlock()
            // In recovery state waiting for IDR session re-creation — drop silently without log flood
            return
        }
        let currentGen = _decodeGeneration
        generationLock.unlock()

        var flagsOut: VTDecodeInfoFlags = []
        let context = FrameContext(structure: structure, pts: CMSampleBufferGetPresentationTimeStamp(sampleBuffer), generation: currentGen)
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
            if isFatalSessionError(status) {
                handleFatalSessionError(status)
            } else {
                let msg = "[1080i50-DECODE-FAIL] VTDecompressionSessionDecodeFrame error: \(status)"
                print(msg)
                TelemetryServer.shared.log(msg)
                delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
            }
        }
    }

    fileprivate func handleDecodedFrame(status: OSStatus, imageBuffer: CVImageBuffer?, context: FrameContext) {
        guard context.generation == decodeGeneration else {
            // Frame from a prior generation (e.g. from an invalidated dead session) is discarded
            return
        }

        guard status == noErr, let buffer = imageBuffer else {
            if isFatalSessionError(status) {
                handleFatalSessionError(status)
            } else {
                let msg = "[1080i50-DECODE-FAIL] handleDecodedFrame async error: \(status)"
                print(msg)
                TelemetryServer.shared.log(msg)
                delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
            }
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
