// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Comprehensive live telemetry metrics for the native DVB-TS VideoToolbox + Metal pipeline.
public final class StreamTelemetry: ObservableObject, @unchecked Sendable {

    // MARK: - TTFP (Time-to-First-Picture) & STARTUP GATES
    @Published public var ttfpTotalMs: Double = 0.0               // t0 -> t6: Request -> Metal Present Submitted
    @Published public var ttfpGpuCompletedMs: Double = 0.0        // t0 -> t6_gpu: Request -> GPU Present Completed
    @Published public var ttfpRating: String = "Pending…"
    @Published public var isFirstPicturePresented: Bool = false
    @Published public var ttfpNetworkMs: Double = 0.0      // t0 -> t1: Request sent -> First TS packet
    @Published public var ttfpPsiMs: Double = 0.0          // t1 -> t2: First TS -> PAT/PMT/Video PID known
    @Published public var ttfpParamSetsMs: Double = 0.0    // t2 -> t3: Video PID -> SPS/PPS ready
    @Published public var ttfpIdrMs: Double = 0.0          // t3 -> t4: SPS/PPS -> First complete IDR AU
    @Published public var ttfpDecodeMs: Double = 0.0       // t4 -> t5: IDR AU -> VideoToolbox decoded frame
    @Published public var ttfpRenderMs: Double = 0.0       // t5 -> t6: Decoded frame -> Metal Display Submit
    @Published public var earlyStabilityIssues: Int = 0    // Drops or glitches in first 2 seconds

    // MARK: - INPUT Metrics
    @Published public var tsBitrateKbps: Double = 0.0
    @Published public var videoPID: UInt16 = 0
    @Published public var continuityErrors: Int = 0
    @Published public var pesErrors: Int = 0

    // MARK: - CODEC Metrics & Direct Source Validation
    @Published public var codec: String = "H.264"
    @Published public var videoWidth: Int = 0
    @Published public var videoHeight: Int = 0
    @Published public var isInterlaced: Bool = false
    @Published public var fieldOrder: String = "Unknown" // "TFF", "BFF", "Progressive"
    @Published public var sourceFrameRate: Double = 0.0  // ~25 fps
    @Published public var sourceFieldRate: Double = 0.0  // ~50 fields/s
    @Published public var isDirect1080iVerified: Bool = false
    @Published public var validationWarning: String? = nil

    // MARK: - DECODER Metrics
    @Published public var vtSessionActive: Bool = false
    @Published public var hwDecodeRequired: Bool = true
    @Published public var hwDecodeActive: Bool = false
    @Published public var vtDeinterlaceAccepted: Bool = false
    @Published public var decodedFramesPerSec: Double = 0.0
    @Published public var ptsProgressionMs: Double = 0.0 // ~40.0 ms per coded frame in 1080i50
    @Published public var decodeErrors: Int = 0
    @Published public var activeDecoderMode: String = "Metal Shader (Path A)" // or "VideoToolbox Native (Path B)"

    // MARK: - RENDER (Independent Field Presentation Counters)
    @Published public var topFieldsPerSec: Double = 0.0        // ~25.0 /s (Top Fields)
    @Published public var bottomFieldsPerSec: Double = 0.0     // ~25.0 /s (Bottom Fields)
    @Published public var repeatedFieldsPerSec: Double = 0.0   // 0.0 /s (Repeated fields)
    @Published public var repeatedFieldCount: Int = 0          // Cumulative repeats
    @Published public var fieldsSubmittedPerSec: Double = 0.0  // ~50.0 fps total
    @Published public var generatedFieldsPerSec: Double = 0.0
    @Published public var presentedFramesPerSec: Double = 0.0
    @Published public var fieldCadenceMs: Double = 0.0         // ~20.0 ms between field presentations
    @Published public var displayCallbacksPerSec: Double = 0.0
    @Published public var droppedFrames: Int = 0
    @Published public var lateFrames: Int = 0
    @Published public var duplicatePresentations: Int = 0
    @Published public var presentationJitterMs: Double = 0.0

    // MARK: - SYSTEM Metrics
    @Published public var cpuUsagePercent: Double = 0.0
    @Published public var gpuTimePerFrameMs: Double = 0.0
    @Published public var thermalState: String = "Nominal"
    @Published public var memoryUsageMB: Double = 0.0

    public init() {}

    public func reset() {
        ttfpTotalMs = 0
        ttfpGpuCompletedMs = 0
        ttfpRating = "Pending…"
        isFirstPicturePresented = false
        ttfpNetworkMs = 0
        ttfpPsiMs = 0
        ttfpParamSetsMs = 0
        ttfpIdrMs = 0
        ttfpDecodeMs = 0
        ttfpRenderMs = 0
        earlyStabilityIssues = 0
        tsBitrateKbps = 0
        videoPID = 0
        continuityErrors = 0
        pesErrors = 0
        videoWidth = 0
        videoHeight = 0
        isInterlaced = false
        fieldOrder = "Unknown"
        isDirect1080iVerified = false
        validationWarning = nil
        sourceFrameRate = 0
        sourceFieldRate = 0
        vtSessionActive = false
        hwDecodeActive = false
        vtDeinterlaceAccepted = false
        decodedFramesPerSec = 0
        ptsProgressionMs = 0
        decodeErrors = 0
        topFieldsPerSec = 0
        bottomFieldsPerSec = 0
        repeatedFieldsPerSec = 0
        repeatedFieldCount = 0
        fieldsSubmittedPerSec = 0
        generatedFieldsPerSec = 0
        presentedFramesPerSec = 0
        fieldCadenceMs = 0
        displayCallbacksPerSec = 0
        droppedFrames = 0
        lateFrames = 0
        duplicatePresentations = 0
        presentationJitterMs = 0
    }
}
