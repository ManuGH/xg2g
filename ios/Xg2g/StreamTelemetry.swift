// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Comprehensive live telemetry metrics for the native DVB-TS VideoToolbox + Metal pipeline.
public final class StreamTelemetry: ObservableObject, @unchecked Sendable {

    // MARK: - INPUT Metrics
    @Published public var tsBitrateKbps: Double = 0.0
    @Published public var videoPID: UInt16 = 0
    @Published public var continuityErrors: Int = 0
    @Published public var pesErrors: Int = 0

    // MARK: - CODEC Metrics
    @Published public var codec: String = "H.264"
    @Published public var videoWidth: Int = 0
    @Published public var videoHeight: Int = 0
    @Published public var isInterlaced: Bool = false
    @Published public var fieldOrder: String = "Unknown" // "TFF", "BFF", "Progressive"
    @Published public var sourceFrameRate: Double = 0.0  // ~25 fps
    @Published public var sourceFieldRate: Double = 0.0  // ~50 fields/s

    // MARK: - DECODER Metrics
    @Published public var vtSessionActive: Bool = false
    @Published public var hwDecodeRequired: Bool = true
    @Published public var hwDecodeActive: Bool = false
    @Published public var decodedFramesPerSec: Double = 0.0
    @Published public var decodeErrors: Int = 0
    @Published public var activeDecoderMode: String = "Metal Shader (Path A)" // or "VideoToolbox Native (Path B)"

    // MARK: - RENDER Metrics
    @Published public var generatedFieldsPerSec: Double = 0.0
    @Published public var presentedFramesPerSec: Double = 0.0
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
        tsBitrateKbps = 0
        videoPID = 0
        continuityErrors = 0
        pesErrors = 0
        videoWidth = 0
        videoHeight = 0
        isInterlaced = false
        fieldOrder = "Unknown"
        sourceFrameRate = 0
        sourceFieldRate = 0
        vtSessionActive = false
        hwDecodeActive = false
        decodedFramesPerSec = 0
        decodeErrors = 0
        generatedFieldsPerSec = 0
        presentedFramesPerSec = 0
        displayCallbacksPerSec = 0
        droppedFrames = 0
        lateFrames = 0
        duplicatePresentations = 0
        presentationJitterMs = 0
    }
}
