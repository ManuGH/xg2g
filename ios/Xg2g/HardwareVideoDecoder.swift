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

public enum VTErrorCategory: Sendable, Equatable {
    case ok
    case bitstream
    case sessionDead
    case reconfigureOrResource
}

public protocol HardwareVideoDecoderDelegate: AnyObject, Sendable {
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didInvalidateSessionWithFatalError error: OSStatus)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didRequestSessionReconfiguration error: OSStatus)
}

/// Hardware-accelerated H.264 video decoder using Apple Silicon VideoToolbox.
///
/// Features:
/// - Enforces `kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder = true`.
/// - Verifies `kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder`.
/// - Outputs Bi-Planar NV12 `CVPixelBuffer`s.
/// - Supports Path A (Native Metal shader deinterlace) vs Path B (`kVTDecompressionPropertyKey_DeinterlaceMode`).
/// - 3-Way Error Classification:
///   * Bitstream defects (-12350, missing refs) -> session retained, logged to telemetry.
///   * Dead session (-12903, -12911) -> atomic generation bump, session invalidated, strict IDR re-gating.
///   * Reconfiguration / Resource (-12908, -12913) -> controlled rebuild, generation bump, IDR re-gating without malfunction penalty.
/// - Atomic synchronization domain eliminating Submit-vs-Invalidate race conditions.
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
    /// Synchronized under `sessionLock`.
    private var _decodeGeneration: Int = 0

    /// Guards `decompressionSession` and `formatDescription`, and is held
    /// across the VideoToolbox calls that act on them.
    private let sessionLock = NSLock()

    /// Guards `_decodeGeneration` alone.
    ///
    /// Deliberately separate from `sessionLock`. The output callback needs the
    /// generation to decide whether a frame is stale, and VideoToolbox may run
    /// that callback synchronously on the thread that submitted the frame — the
    /// thread already holding `sessionLock`. Sharing one lock therefore forced a
    /// choice between two failures: hold it across the submission and the
    /// callback deadlocks against it, or release it and a concurrent
    /// invalidation frees the session mid-submission. With the generation on its
    /// own lock the callback never reaches for `sessionLock`, so the submission
    /// can stay protected and neither failure is possible.
    ///
    /// Lock ordering, where both are needed: `sessionLock` first, never the
    /// reverse.
    private let generationLock = NSLock()

    /// Serialises session teardown requested from the output callback.
    ///
    /// The callback must not tear the session down inline: it can be running
    /// inside `VTDecompressionSessionDecodeFrame` with `sessionLock` held by
    /// the caller below it on the same thread.
    private let recoveryQueue = DispatchQueue(label: "io.github.manugh.xg2g.decoder.recovery")

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
        sessionLock.lock()
        defer { sessionLock.unlock() }
        return decompressionSession != nil
    }

    public var isConfigured: Bool {
        sessionLock.lock()
        defer { sessionLock.unlock() }
        return formatDescription != nil
    }

    public init() {}

    deinit {
        invalidateSession()
    }

    public func reset() {
        sessionLock.lock()
        let stale = detachSessionLocked()
        formatDescription = nil
        sessionLock.unlock()
        invalidateDetached(stale)
    }

    public func classifyError(_ status: OSStatus) -> VTErrorCategory {
        if status == noErr { return .ok }

        switch status {
        case kVTInvalidSessionErr, kVTVideoDecoderMalfunctionErr:
            return .sessionDead
        // The "could not find / could not create" family reports that the
        // system had no decoder to give right now — a resource condition that
        // clears on its own. Left to the `default` branch below they counted as
        // a dead session, which tore the session down and advanced the
        // generation for something a plain re-creation resolves.
        //
        // `kVTCouldNotFindVideoEncoderErr` sits here for completeness of that
        // family; a decode path cannot actually raise it.
        case kVTFormatDescriptionChangeNotSupportedErr, kVTVideoDecoderNotAvailableNowErr,
             kVTCouldNotFindVideoDecoderErr, kVTCouldNotCreateInstanceErr,
             kVTCouldNotFindVideoEncoderErr:
            return .reconfigureOrResource
        case kVTVideoDecoderBadDataErr, kVTVideoDecoderReferenceMissingErr, kVTParameterErr, -12350, -12909, -12904, -8969:
            return .bitstream
        default:
            // Any unknown negative VT error in the CoreMedia/VideoToolbox range
            if status < -12000 && status > -13000 {
                return .sessionDead
            }
            return .bitstream
        }
    }

    public func isFatalSessionError(_ status: OSStatus) -> Bool {
        return classifyError(status) == .sessionDead
    }

    public func isReconfigurationError(_ status: OSStatus) -> Bool {
        return classifyError(status) == .reconfigureOrResource
    }

    public func isBitstreamError(_ status: OSStatus) -> Bool {
        return classifyError(status) == .bitstream
    }

    /// Detaches the session and advances the generation, handing the old
    /// session back for the caller to invalidate *after* dropping the lock.
    ///
    /// `VTDecompressionSessionInvalidate` completes frames still in flight
    /// through the output callback, and `handleDecodedFrame` takes this same
    /// lock. Invalidating while holding it left the caller waiting on a lock it
    /// already owned — `NSLock` is not recursive, so the thread never woke up.
    /// Detaching under the lock keeps the state change atomic; the teardown
    /// itself needs no lock, because nothing else can still reach the session.
    private func detachAndAdvanceGenerationLocked() -> (stale: VTDecompressionSession?, generation: Int) {
        let stale = decompressionSession
        decompressionSession = nil
        generationLock.lock()
        _decodeGeneration += 1
        let generation = _decodeGeneration
        generationLock.unlock()
        return (stale, generation)
    }

    /// Detaches the session without touching the generation.
    private func detachSessionLocked() -> VTDecompressionSession? {
        let stale = decompressionSession
        decompressionSession = nil
        return stale
    }

    /// Tears a detached session down. Must run with `sessionLock` released.
    private func invalidateDetached(_ session: VTDecompressionSession?) {
        guard let session else { return }
        VTDecompressionSessionInvalidate(session)
    }

    public func forceSessionRecovery(reason: String) {
        sessionLock.lock()
        let (stale, newGen) = detachAndAdvanceGenerationLocked()
        sessionLock.unlock()
        invalidateDetached(stale)

        let msg = "[1080i50-DEC-FORCE] 🛑 Forced session recovery (\(reason)) -> advanced to gen \(newGen)"
        print(msg)
        TelemetryServer.shared.log(msg)

        delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
        delegate?.hardwareDecoder(self, didInvalidateSessionWithFatalError: kVTInvalidSessionErr)
    }

    public func invalidateSessionForBackground() {
        sessionLock.lock()
        let (stale, _) = detachAndAdvanceGenerationLocked()
        sessionLock.unlock()
        invalidateDetached(stale)
        delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
    }

    public func configure(with formatDescription: CMVideoFormatDescription) {
        sessionLock.lock()
        let shouldSkip = self.formatDescription != nil &&
                         CMFormatDescriptionEqual(self.formatDescription!, otherFormatDescription: formatDescription) &&
                         decompressionSession != nil
        if shouldSkip {
            sessionLock.unlock()
            return
        }
        self.formatDescription = formatDescription
        sessionLock.unlock()
        recreateSession()
    }

    public func recreateSession() {
        sessionLock.lock()
        let stale = detachSessionLocked()
        guard let format = formatDescription else {
            sessionLock.unlock()
            invalidateDetached(stale)
            return
        }
        sessionLock.unlock()
        invalidateDetached(stale)

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
            isHWActive = true
        }

        sessionLock.lock()
        self.decompressionSession = activeSession
        sessionLock.unlock()

        delegate?.hardwareDecoder(self, didChangeHWActiveState: isHWActive)
        delegate?.hardwareDecoder(self, didChangeVTDeinterlaceAccepted: isVTDeinterlaceAccepted)
    }

    public func ensureSession() -> Bool {
        if hasActiveSession { return true }
        guard isConfigured else { return false }
        recreateSession()
        return hasActiveSession
    }

    private func invalidateSession() {
        sessionLock.lock()
        let stale = detachSessionLocked()
        sessionLock.unlock()
        invalidateDetached(stale)
    }

    public func decode(sampleBuffer: CMSampleBuffer, structure: H264PictureStructure) {
        // `sessionLock` is held across the submission on purpose: releasing it
        // first let a concurrent `invalidateSessionForBackground()` tear the
        // session down while VideoToolbox was still working on it, which
        // crashed rather than returning an error. Holding it is only safe
        // because the output callback now takes `generationLock` instead, so a
        // synchronous callback on this very thread cannot block on this lock.
        sessionLock.lock()
        guard let session = decompressionSession else {
            sessionLock.unlock()
            // In recovery state waiting for IDR session re-creation — drop silently without log flood
            return
        }

        generationLock.lock()
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

        guard status != noErr else {
            sessionLock.unlock()
            return
        }

        _ = Unmanaged<FrameContext>.fromOpaque(contextPtr).takeRetainedValue()
        let category = classifyError(status)

        // The state change happens under the lock; the teardown, the logging and
        // the delegate calls happen after it, where they cannot block a decode.
        var stale: VTDecompressionSession?
        var newGen = currentGen
        switch category {
        case .sessionDead, .reconfigureOrResource:
            (stale, newGen) = detachAndAdvanceGenerationLocked()
        case .bitstream, .ok:
            break
        }
        sessionLock.unlock()
        invalidateDetached(stale)

        switch category {
        case .sessionDead:
            let msg = "[1080i50-DEC-FATAL] 🛑 VTDecompressionSession invalidated due to fatal error: \(status) (advanced to gen \(newGen))"
            print(msg)
            TelemetryServer.shared.log(msg)
            delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
            delegate?.hardwareDecoder(self, didInvalidateSessionWithFatalError: status)
        case .reconfigureOrResource:
            let msg = "[1080i50-DEC-RECONFIG] 🔄 VTDecompressionSession invalidated for reconfiguration/resource: \(status) (advanced to gen \(newGen))"
            print(msg)
            TelemetryServer.shared.log(msg)
            delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
            delegate?.hardwareDecoder(self, didRequestSessionReconfiguration: status)
        case .bitstream, .ok:
            let msg = "[1080i50-DECODE-FAIL] VTDecompressionSessionDecodeFrame bitstream error: \(status)"
            print(msg)
            TelemetryServer.shared.log(msg)
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
        }
    }

    fileprivate func handleDecodedFrame(status: OSStatus, imageBuffer: CVImageBuffer?, context: FrameContext) {
        generationLock.lock()
        let currentGen = _decodeGeneration
        generationLock.unlock()

        guard context.generation == currentGen else {
            // Frame from a prior generation (e.g. from an invalidated dead session) is discarded
            return
        }

        guard status == noErr, let buffer = imageBuffer else {
            let category = classifyError(status)
            switch category {
            // Off this thread, always: this callback can be running inside
            // `VTDecompressionSessionDecodeFrame`, underneath a caller that
            // holds `sessionLock`. Taking that lock here would be taking a lock
            // the same thread already owns.
            case .sessionDead:
                recoveryQueue.async { [weak self] in
                    guard let self else { return }
                    self.sessionLock.lock()
                    let (stale, newGen) = self.detachAndAdvanceGenerationLocked()
                    self.sessionLock.unlock()
                    self.invalidateDetached(stale)
                    let msg = "[1080i50-DEC-FATAL] 🛑 VTDecompressionSession async fatal error: \(status) (advanced to gen \(newGen))"
                    print(msg)
                    TelemetryServer.shared.log(msg)
                    self.delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
                    self.delegate?.hardwareDecoder(self, didInvalidateSessionWithFatalError: status)
                }
            case .reconfigureOrResource:
                recoveryQueue.async { [weak self] in
                    guard let self else { return }
                    self.sessionLock.lock()
                    let (stale, newGen) = self.detachAndAdvanceGenerationLocked()
                    self.sessionLock.unlock()
                    self.invalidateDetached(stale)
                    let msg = "[1080i50-DEC-RECONFIG] 🔄 VTDecompressionSession async reconfig error: \(status) (advanced to gen \(newGen))"
                    print(msg)
                    TelemetryServer.shared.log(msg)
                    self.delegate?.hardwareDecoder(self, didChangeHWActiveState: false)
                    self.delegate?.hardwareDecoder(self, didRequestSessionReconfiguration: status)
                }
            case .bitstream, .ok:
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
