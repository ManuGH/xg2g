// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Every live metric of the native DVB-TS pipeline as one plain value.
///
/// A struct, not 51 `@Published` properties: the pipeline touches these from
/// the TS parse queue, the decoder callback queue and the display link, dozens
/// of times per second. Individual published properties turned each of those
/// into its own main-queue hop and its own SwiftUI invalidation.
public struct TelemetryValues: Sendable {

    // MARK: - TTFP (Time-to-First-Picture) & STARTUP GATES
    public var ttfpTotalMs: Double = 0.0               // t0 -> t6: Request -> Metal Present Submitted
    public var ttfpGpuCompletedMs: Double = 0.0        // t0 -> t6_gpu: Request -> GPU Present Completed
    public var ttfpRating: String = "Pending…"
    public var isFirstPicturePresented: Bool = false
    /// Asked to tune, to the request actually leaving.
    ///
    /// The previous stream's teardown and the audio session activation, both of
    /// which used to be charged to the network stage and made the receiver look
    /// slower than it is.
    public var ttfpSetupMs: Double = 0.0

    /// The teardown's own share of the setup: cancelling the old session and
    /// draining the parse queue behind a barrier.
    public var ttfpTeardownMs: Double = 0.0

    /// The audio session's own share of the setup. Synchronous, and talking to
    /// another process.
    public var ttfpAudioSessionMs: Double = 0.0

    public var ttfpNetworkMs: Double = 0.0      // t1 -> t2: Request sent -> First TS packet
    public var ttfpPsiMs: Double = 0.0          // t1 -> t2: First TS -> PAT/PMT/Video PID known
    public var ttfpParamSetsMs: Double = 0.0    // t2 -> t3: Video PID -> SPS/PPS ready
    public var ttfpIdrMs: Double = 0.0          // t3 -> t4: SPS/PPS -> First complete IDR AU
    public var ttfpDecodeMs: Double = 0.0       // t4 -> t5: IDR AU -> VideoToolbox decoded frame
    public var ttfpRenderMs: Double = 0.0       // t5 -> t6: Decoded frame -> Metal Display Submit

    /// Request to the moment the picture is actually on screen.
    ///
    /// The number the viewer experiences, and the one nothing was reporting.
    /// `ttfpTotalMs` ends when the pipeline has produced its first field; under
    /// system presentation the display layer then holds it until the master
    /// clock reaches its timestamp, and that clock does not start until the
    /// audio pre-roll is buffered. The gap between the two is the part of tuning
    /// latency that pipeline tuning cannot touch.
    public var ttfpVisibleMs: Double = 0.0

    /// Request to the moment playback actually moves.
    ///
    /// The picture appears before this — the first field carries
    /// `DisplayImmediately` and does not wait for the clock — so between the two
    /// the viewer is looking at a still frame. That gap was invisible to every
    /// figure here: `ttfpVisibleMs` used to mean "the clock reached the first
    /// field", which was the same instant as motion, and once the attachment
    /// moved the picture forward the two came apart with nothing measuring the
    /// distance. It is the part of tuning a viewer is most likely to read as a
    /// hang, because a frozen picture looks like a fault in a way a black screen
    /// does not.
    public var ttfpMotionMs: Double = 0.0

    /// Faults the pipeline recognised in itself, each recorded once.
    ///
    /// Everything else here is a number someone has to interpret. These are the
    /// states where the pipeline knows it is not doing its job — and it has been
    /// able to know for a long time without saying so: a channel that froze
    /// reported 50 fps decoded, zero continuity errors, zero PES errors, zero
    /// decode errors and healthy memory, because every counter measured a part
    /// and nothing watched the shape.
    public var pipelineWarnings: [String] = []
    public var earlyStabilityIssues: Int = 0    // Drops or glitches in first 2 seconds
    public var sampleBuffersEmittedCount: Int = 0
    public var sampleBuffersDecodedCount: Int = 0

    // MARK: - INPUT Metrics
    public var tsBitrateKbps: Double = 0.0
    public var videoPID: UInt16 = 0
    public var audioPID: UInt16 = 0
    public var audioCodec: String = "Searching…"
    public var audioLanguage: String = "und"

    /// Streams the PMT named that this build could not classify as audio.
    ///
    /// Non-zero with `audioPID == 0` is the silent-channel case: the service
    /// announces sound this build cannot identify, so no track was ever selected.
    public var unclassifiedAudioStreams: Int = 0
    public var audioChannels: Int = 0
    public var audioSampleRate: Int = 0
    public var audioBitrateKbps: Int = 0
    public var isAudioMasterClockActive: Bool = false
    public var continuityErrors: Int = 0
    public var pesErrors: Int = 0

    /// Packets dropped on the played video or audio PID because the receiver
    /// had not descrambled them yet.
    ///
    /// Expected to be non-zero for a moment at tune-in on an encrypted service
    /// and then to stop. A count that keeps climbing means the receiver is not
    /// descrambling the programme being watched, which is a subscription or CA
    /// problem rather than anything this app can fix — and which used to show
    /// up only as a rising PES error count that implicated the parser instead.
    ///
    /// Restricted to those two PIDs on purpose: counting the whole transport
    /// reached 24512 on a tune that played perfectly, because the other
    /// services in the mux stay encrypted and are none of our business.
    public var scrambledPackets: Int = 0

    /// The same observation split by elementary stream.
    ///
    /// The aggregate cannot tell "video is through, audio is not" apart from
    /// "nothing is through", and those are different faults: the first plays a
    /// silent picture, the second plays nothing. Readiness is decided per stream,
    /// so it has to be measured per stream.
    public var scrambledVideoPackets: Int = 0
    public var scrambledAudioPackets: Int = 0

    /// Clear packets seen since the last scrambled one on the played video PID.
    ///
    /// Descrambling was measured coming up intermittently on the reference
    /// receiver - 733 scrambled packets interleaved with clear ones on one tune -
    /// so a single clear packet is not evidence that the stream is clear. The run
    /// length is.
    public var videoClearRun: Int = 0

    /// Times the stream's timeline moved under playback and everything had to be
    /// re-anchored onto it.
    ///
    /// Distinct from a socket stall, which this used to be confused with:
    /// buffered data keeps its timestamps, so a gap in arrival is not a gap in
    /// presentation. A rising count here means the source changed what it was
    /// sending — an encoder restart, a splice, or the receiver being retuned
    /// underneath the stream.
    public var ptsDiscontinuities: Int = 0

    // MARK: - AUDIO FLOW
    //
    // How far the audio handed to the renderer runs ahead of the playback clock.
    // Dropouts were reported by ear and argued about from the video counters,
    // because nothing here described the audio path's own health.
    public var audioUnderruns: Int = 0      // Times the lead fell under 50 ms
    public var audioLeadMs: Double = 0.0    // Current cushion
    public var audioMinLeadMs: Double = 0.0 // Worst cushion in the last window

    /// Access units emitted without a PES timestamp. A healthy broadcast stream
    /// carries one PTS per coded picture, so a rising count means the assembler
    /// is splitting pictures into multiple access units.
    public var accessUnitsWithoutPTS: Int = 0

    /// Access units discarded at tune-in because no sync sample had arrived yet.
    ///
    /// Expected to be non-zero on every cold tune — a broadcast GOP is entered
    /// wherever the socket opens — and to stop rising the moment the gate opens.
    /// A count that keeps climbing means no intra picture is being recognised.
    public var gatedAccessUnits: Int = 0

    /// Bytes accepted from the socket but not yet parsed. Stays near zero when
    /// the parse chain keeps up; a standing backlog means decode is the
    /// bottleneck and picture delivery will arrive in bursts.
    public var ingestBacklogBytes: Int = 0

    // MARK: - CODEC Metrics & Direct Source Validation
    public var codec: String = "H.264"
    public var videoWidth: Int = 0
    public var videoHeight: Int = 0
    public var isInterlaced: Bool = false
    public var fieldOrder: String = "Unknown" // "TFF", "BFF", "Progressive"
    public var sourceFrameRate: Double = 0.0  // ~25 fps
    public var sourceFieldRate: Double = 0.0  // ~50 fields/s
    public var isDirect1080iVerified: Bool = false
    public var validationWarning: String?

    // MARK: - ASPECT RATIO & GEOMETRY Metrics
    public var sarNumerator: Int = 1
    public var sarDenominator: Int = 1
    public var sarSignaled: Bool = false
    public var effectiveDAR: Double = 16.0 / 9.0
    public var darDescription: String = "16:9"
    public var videoScanSummary: String = "1080i50"
    public var presentationModeName: String = "Standard"

    /// Set when the PMT names a video codec this pipeline cannot turn into
    /// pictures, carrying the viewer-facing name of that format.
    ///
    /// Without it the failure is a black screen and nothing else: the stream
    /// arrives, the parser runs, and the H.264 assembler finds nothing it
    /// recognises in an MPEG-2 elementary stream.
    public var unplayableVideoCodec: String?

    // MARK: - DECODER Metrics
    public var vtSessionActive: Bool = false
    public var hwDecodeActive: Bool = false
    public var vtDeinterlaceAccepted: Bool = false
    public var decodedFramesPerSec: Double = 0.0
    public var ptsProgressionMs: Double = 0.0 // ~40.0 ms per coded frame in 1080i50
    public var decodeErrors: Int = 0
    public var decoderRecoveries: Int = 0
    public var activeDecoderMode: String = "Metal Shader (Path A)" // or "VideoToolbox Native (Path B)"

    // MARK: - RENDER (Field Presentation Counters)
    //
    // A "field" here is a distinct bob-deinterlaced presentation, counted once
    // by the parity actually rendered. Re-drawing the same field because
    // 50 fields/s does not divide into the display refresh rate counts under
    // `repeatedFieldsPerSec`, never as a new field.
    public var topFieldsPerSec: Double = 0.0        // ~25.0 /s
    public var bottomFieldsPerSec: Double = 0.0     // ~25.0 /s
    public var repeatedFieldsPerSec: Double = 0.0   // ~10 /s at 60 Hz, ~70 /s at 120 Hz
    public var repeatedFieldCount: Int = 0          // Cumulative repeats
    public var fieldsSubmittedPerSec: Double = 0.0  // top + bottom, ~50.0 /s
    public var generatedFieldsPerSec: Double = 0.0
    public var presentedFramesPerSec: Double = 0.0  // Draw calls, i.e. display refresh rate
    public var fieldCadenceMs: Double = 0.0         // ~20.0 ms between distinct fields
    public var displayCallbacksPerSec: Double = 0.0
    public var droppedFrames: Int = 0               // Reorder overflow + texture failures
    public var lateFrames: Int = 0                  // Became due and were superseded unseen
    public var presentationJitterMs: Double = 0.0   // |actual - ideal| field interval
    public var queuedFieldCount: Int = 0            // Current scheduler backlog

    // MARK: - SYSTEM Metrics
    //
    // CPU and GPU-time counters used to live here and were never measured — they
    // reported a constant 0.0 as if that were a reading. Removed rather than
    // left in place; add them back with an actual sampler behind them.
    public var thermalState: String = "Nominal"
    public var memoryUsageMB: Double = 0.0
    public var peakMemoryFootprintMB: Double = 0.0
    public var processCpuUsagePercent: Double = 0.0
    public var vtInFlightFrames: Int = 0

    public init() {}

    public func toDictionary() -> [String: Any] {
        return [
            "ttfp_total_ms": ttfpTotalMs,
            "ttfp_gpu_completed_ms": ttfpGpuCompletedMs,
            "ttfp_motion_ms": ttfpMotionMs,
            "pipeline_warnings": pipelineWarnings,
            "ttfp_setup_ms": ttfpSetupMs,
            "ttfp_teardown_ms": ttfpTeardownMs,
            "ttfp_audio_session_ms": ttfpAudioSessionMs,
            "ttfp_rating": ttfpRating,
            "ttfp_network_ms": ttfpNetworkMs,
            "ttfp_psi_ms": ttfpPsiMs,
            "ttfp_param_sets_ms": ttfpParamSetsMs,
            "ttfp_idr_ms": ttfpIdrMs,
            "ttfp_decode_ms": ttfpDecodeMs,
            "ttfp_render_ms": ttfpRenderMs,
            "sample_buffers_emitted": sampleBuffersEmittedCount,
            "sample_buffers_decoded": sampleBuffersDecodedCount,
            "ts_bitrate_kbps": tsBitrateKbps,
            "video_pid": videoPID,
            "audio_pid": audioPID,
            "audio_codec": audioCodec,
            "audio_language": audioLanguage,
            "audio_channels": audioChannels,
            "audio_sample_rate": audioSampleRate,
            "audio_master_clock_active": isAudioMasterClockActive,
            "audio_underruns": audioUnderruns,
            "audio_lead_ms": audioLeadMs,
            "audio_min_lead_ms": audioMinLeadMs,
            "continuity_errors": continuityErrors,
            "pes_errors": pesErrors,
            "codec": codec,
            "resolution": "\(videoWidth)x\(videoHeight)",
            "is_interlaced": isInterlaced,
            "field_order": fieldOrder,
            "sar": "\(sarNumerator):\(sarDenominator)",
            "sar_signaled": sarSignaled,
            "effective_dar": effectiveDAR,
            "dar_description": darDescription,
            "video_scan_summary": videoScanSummary,
            "presentation_mode": presentationModeName,
            "source_frame_rate": sourceFrameRate,
            "source_field_rate": sourceFieldRate,
            "hw_decode_active": hwDecodeActive,
            "vt_session_active": vtSessionActive,
            "vt_deinterlace_accepted": vtDeinterlaceAccepted,
            "decoded_fps": decodedFramesPerSec,
            "pts_delta_ms": ptsProgressionMs,
            "decode_errors": decodeErrors,
            "decoder_recoveries": decoderRecoveries,
            "top_fields_per_sec": topFieldsPerSec,
            "bottom_fields_per_sec": bottomFieldsPerSec,
            "fields_submitted_per_sec": fieldsSubmittedPerSec,
            "repeated_fields_per_sec": repeatedFieldsPerSec,
            "presented_frames_per_sec": presentedFramesPerSec,
            "display_callbacks_per_sec": displayCallbacksPerSec,
            "field_cadence_ms": fieldCadenceMs,
            "presentation_jitter_ms": presentationJitterMs,
            "repeated_field_count": repeatedFieldCount,
            "queued_field_count": queuedFieldCount,
            "dropped_frames": droppedFrames,
            "late_frames": lateFrames,
            "ttfp_visible_ms": ttfpVisibleMs,
            "access_units_without_pts": accessUnitsWithoutPTS,
            "gated_access_units": gatedAccessUnits,
            "pts_discontinuities": ptsDiscontinuities,
            "scrambled_packets": scrambledPackets,
            "scrambled_video_packets": scrambledVideoPackets,
            "scrambled_audio_packets": scrambledAudioPackets,
            "video_clear_run": videoClearRun,
            "ingest_backlog_bytes": ingestBacklogBytes,
            "thermal_state": thermalState,
            "memory_usage_mb": memoryUsageMB,
            "peak_memory_footprint_mb": peakMemoryFootprintMB,
            "process_cpu_usage_percent": processCpuUsagePercent,
            "vt_in_flight_frames": vtInFlightFrames
        ]
    }
}

/// Thread-safe holder for `TelemetryValues` that publishes a coalesced copy to SwiftUI.
///
/// Producers call `mutate` from whatever thread they are already on — no main-queue
/// hop, no per-metric invalidation. The HUD binds to `display`, which is refreshed on
/// the main thread at most every `publishInterval` seconds however hot the pipeline runs.
public final class StreamTelemetry: ObservableObject, @unchecked Sendable {

    /// Coalesced main-thread mirror. This is what SwiftUI observes.
    @Published public private(set) var display = TelemetryValues()

    private let lock = NSLock()
    private var values = TelemetryValues()
    private var publishScheduled = false

    /// 2 Hz: fast enough to read as live, with minimal main thread and SwiftUI invalidation overhead.
    private static let publishInterval: TimeInterval = 0.50

    public init() {}

    /// Apply a mutation under the lock. Safe from any thread.
    @discardableResult
    public func mutate<T>(_ body: (inout TelemetryValues) -> T) -> T {
        lock.lock()
        let result = body(&values)
        let needsSchedule = !publishScheduled
        publishScheduled = true
        lock.unlock()

        if needsSchedule {
            DispatchQueue.main.asyncAfter(deadline: .now() + Self.publishInterval) { [weak self] in
                self?.publishNow()
            }
        }
        return result
    }

    /// Current values, consistent as of this call. Safe from any thread.
    public func snapshot() -> TelemetryValues {
        lock.lock()
        defer { lock.unlock() }
        return values
    }

    /// Clears per-stream metrics for a channel zap.
    ///
    /// Deliberately keeps the values that describe the app rather than the stream —
    /// decoder mode, codec, thermal and memory readings, and the cumulative sample
    /// buffer counters — matching the pre-refactor behaviour.
    public func reset() {
        lock.lock()
        var fresh = TelemetryValues()
        fresh.codec = values.codec
        fresh.activeDecoderMode = values.activeDecoderMode
        fresh.thermalState = values.thermalState
        fresh.memoryUsageMB = values.memoryUsageMB
        fresh.peakMemoryFootprintMB = values.memoryUsageMB
        fresh.processCpuUsagePercent = values.processCpuUsagePercent
        fresh.sampleBuffersEmittedCount = values.sampleBuffersEmittedCount
        fresh.sampleBuffersDecodedCount = values.sampleBuffersDecodedCount
        values = fresh
        lock.unlock()

        publishNow()
    }

    public func toDictionary() -> [String: Any] {
        snapshot().toDictionary()
    }

    private func publishNow() {
        lock.lock()
        let current = values
        publishScheduled = false
        lock.unlock()

        if Thread.isMainThread {
            display = current
        } else {
            DispatchQueue.main.async { [weak self] in
                self?.display = current
            }
        }
    }
}
