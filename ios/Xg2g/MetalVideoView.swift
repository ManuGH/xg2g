// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreImage
import CoreMedia
import Metal
import MetalKit
import OSLog
import QuartzCore
import UIKit

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "metal")

/// Layout must match `DeinterlaceParams` in the shader source below.
private struct DeinterlaceShaderParams {
    var fieldParity: UInt32     // 0 = top field (even frame rows), 1 = bottom field
    var isInterlaced: UInt32
    var lumaHeight: Float
    var chromaHeight: Float
}

/// One field of a decoded frame, scheduled against the stream clock.
///
/// Bob deinterlacing presents each of a coded frame's two fields in turn, so a
/// 25 fps 1080i stream becomes 50 distinct presentations per second. The second
/// field is due half a frame duration after the first.
private struct PendingField {
    let pixelBuffer: CVPixelBuffer
    let parity: UInt32
    let ptsSeconds: Double
}

/// Metal-based video rendering view with per-field bob deinterlacing and
/// PTS-driven presentation scheduling.
@MainActor
public final class MetalVideoView: UIView {

    private let metalDevice: MTLDevice
    private let commandQueue: MTLCommandQueue
    private var deinterlacePipeline: MTLRenderPipelineState?
    private var presentPipeline: MTLRenderPipelineState?
    private var textureCache: CVMetalTextureCache?

    /// Full-resolution deinterlace target. Rendering the field reconstruction at
    /// native size and scaling in a second pass keeps the row-exact field taps
    /// from aliasing when the layer is smaller than the video, and gives
    /// `captureCurrentFrameJPEG` the actual on-screen pixels to read back.
    private var offscreenTexture: MTLTexture?
    private var offscreenSize: (width: Int, height: Int) = (0, 0)

    private var displayLink: CADisplayLink?
    private let reorderBuffer = FrameReorderBuffer()
    private var observers: ObserverRegistration?

    /// Master render synchronizer clock (driven by AVSampleBufferAudioRenderer).
    public var synchronizer: AVSampleBufferRenderSynchronizer?

    // MARK: - Presentation state (main thread only)

    private var fieldQueue: [PendingField] = []
    private var currentField: PendingField?

    /// Forces one draw even without a new field, so a resize repaints the
    /// stale drawable at the new size instead of leaving it stretched.
    private var needsRedraw: Bool = true

    /// Maps stream PTS onto the host clock: `stream = anchorStream + (host - anchorHost)`.
    private var anchorHost: CFTimeInterval = 0
    private var anchorStream: Double = 0
    private var isClockAnchored: Bool = false

    /// Set when the field queue drains. Makes the next arriving field re-anchor
    /// the clock instead of inheriting one that free-ran through the gap.
    private var isStarved: Bool = false

    private var lastReleasedPTS: Double = .nan
    private var observedPTSDeltaMs: Double = 0

    /// Recent frame-picture gaps, newest last. A median over these is used
    /// instead of a running average: an average lets a single outlier — a
    /// discontinuity, a decode hiccup — drag the estimate, and everything
    /// downstream (field spacing, buffer target, drift error) is derived from it,
    /// so a wrong value cascades into a stalled clock.
    private var recentFrameGaps: [Double] = []
    private static let frameGapWindow = 15

    /// Bounds on a plausible broadcast frame duration: 60 fps to 16⅔ fps. The
    /// estimate is clamped rather than trusted, because no legitimate DVB source
    /// sits outside this and a value below it collapses field spacing.
    private static let minFrameDuration: Double = 1.0 / 60.0
    private static let maxFrameDuration: Double = 0.060

    /// Two fields closer together than this cannot land in separate display
    /// ticks even at 120 Hz, so the second would never be seen. Well under a
    /// half-frame on purpose: legitimately tight spacing must pass through
    /// untouched, or the correction takes over the schedule.
    private static let minFieldSeparation: Double = 0.005

    /// Median of `recentFrameGaps`, clamped, defaulting to 1080i50's 40 ms.
    private var estimatedFrameDuration: Double {
        guard !recentFrameGaps.isEmpty else { return 0.04 }
        let sorted = recentFrameGaps.sorted()
        let median = sorted[sorted.count / 2]
        return min(max(median, Self.minFrameDuration), Self.maxFrameDuration)
    }

    // Picture-coding mix, reported per second. A PAFF broadcast switches between
    // the two per picture, and each needs a different number of presentations.
    private var wovenFrameCount: Int = 0
    private var discreteFieldCount: Int = 0

    /// PTS of the last field placed on the timeline, used to keep successive
    /// fields at least half a frame apart.
    private var lastQueuedFieldPTS: Double = .nan
    private var fieldSpacingCorrections: Int = 0
    private var lateFieldDropCount: Int = 0

    // Raw picture-gap distribution, including the zero and negative gaps the
    // `observedPTSDeltaMs` filter discards. Without these a cluster of
    // same-instant timestamps is invisible in the telemetry.
    private var gapClusteredCount: Int = 0     // < 5 ms
    private var gapHalfFrameCount: Int = 0     // 5–30 ms
    private var gapFrameCount: Int = 0         // 30–50 ms
    private var gapWideCount: Int = 0          // > 50 ms
    private var gapNegativeCount: Int = 0

    /// How far ahead of the presentation clock the field queue is kept in video-only fallback.
    private static let targetLatencySeconds: Double = 0.20

    /// Set from the pipeline's format callback on the ingest queue, read on the
    /// display link. A single Bool write is atomic on ARM, and a one-tick-stale
    /// read only affects the first field pair after a format change.
    nonisolated(unsafe) public var sourceIsInterlaced: Bool = true

    // MARK: - Telemetry

    /// `nonisolated(unsafe)` because `enqueueFrame` runs on the decoder's queue.
    /// The reference is wired once during view setup, before frames flow, and
    /// `StreamTelemetry` is internally locked.
    nonisolated(unsafe) public var telemetry: StreamTelemetry?
    public var onFirstFrameRendered: (@MainActor () -> Void)?
    public var onFirstFrameActuallyPresentedOnScreen: (@MainActor (Double) -> Void)?
    private var hasReportedFirstFrame: Bool = false

    private var callbackCount: Int = 0
    private var presentationCount: Int = 0
    private var topFieldCount: Int = 0
    private var bottomFieldCount: Int = 0
    private var repeatedPresentationCount: Int = 0
    private var cumulativeRepeatedFieldCount: Int = 0
    private var skippedLateFieldCount: Int = 0
    private var lastTelemetryUpdate: CFTimeInterval = 0

    // Decisive diagnostics for "fields are queued but not being consumed":
    // how fast the presentation clock actually advances against real time, and
    // how often the starve path re-anchors it.
    private var lastPublishedStreamNow: Double = .nan
    private var starveResyncCount: Int = 0
    private var clockHoldCount: Int = 0

    /// Cadence and jitter are measured between *distinct* field presentations,
    /// not between display-link callbacks: re-showing the same field because
    /// 50 fields/s does not divide into the refresh rate is expected, and must
    /// not be reported as a timing fault.
    private var lastDistinctPresentationHost: CFTimeInterval = 0
    private var fieldIntervalAccumulator: Double = 0
    private var fieldIntervalSamples: Int = 0
    private var jitterAccumulator: Double = 0

    public func resetForChannelZap() {
        reorderBuffer.clear()
        fieldQueue.removeAll(keepingCapacity: true)
        hasReportedFirstFrame = false
        isClockAnchored = false
        isStarved = false
        lastReleasedPTS = .nan
        lastQueuedFieldPTS = .nan
        recentFrameGaps.removeAll(keepingCapacity: true)

        lastDistinctPresentationHost = 0
        // Keep currentField to avoid a black flash between channel switches
    }

    public var metalLayer: CAMetalLayer {
        return layer as! CAMetalLayer
    }

    public override class var layerClass: AnyClass {
        return CAMetalLayer.self
    }

    public override init(frame: CGRect) {
        guard let device = MTLCreateSystemDefaultDevice(),
              let queue = device.makeCommandQueue() else {
            fatalError("Metal is not supported on this device")
        }
        self.metalDevice = device
        self.commandQueue = queue
        super.init(frame: frame)

        setupMetal()
        setupDisplayLink()
        setupLifecycleObservers()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    private func setupLifecycleObservers() {
        let resign = NotificationCenter.default.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.displayLink?.isPaused = true }
        }
        let becomeActive = NotificationCenter.default.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                // The stream clock kept running while suspended; re-anchor
                // instead of trying to catch up on a backlog that is now stale.
                self.isClockAnchored = false
                // The measurement windows have to restart with it. Carrying them
                // across the gap made the first interval after a resume span the
                // whole suspension — a 25-minute pause was reported as 61472 ms
                // of field cadence and jitter.
                self.lastDistinctPresentationHost = 0
                self.lastTelemetryUpdate = CACurrentMediaTime()
                self.lastPublishedStreamNow = .nan
                self.resetPerSecondCounters()
                self.displayLink?.isPaused = false
            }
        }
        observers = ObserverRegistration(tokens: [resign, becomeActive])
    }

    public override func layoutSubviews() {
        super.layoutSubviews()
        needsRedraw = true
    }

    public override func didMoveToWindow() {
        super.didMoveToWindow()
        if window == nil {
            displayLink?.invalidate()
            displayLink = nil
        } else if displayLink == nil {
            setupDisplayLink()
        }
    }

    private func setupMetal() {
        metalLayer.device = metalDevice
        metalLayer.pixelFormat = .bgra8Unorm
        metalLayer.framebufferOnly = true
        metalLayer.contentsScale = UIScreen.main.scale

        CVMetalTextureCacheCreate(kCFAllocatorDefault, nil, metalDevice, nil, &textureCache)

        let shaderSource = """
        #include <metal_stdlib>
        using namespace metal;

        struct VertexOutput {
            float4 position [[position]];
            float2 texCoords;
        };

        struct DeinterlaceParams {
            uint fieldParity;
            uint isInterlaced;
            float lumaHeight;
            float chromaHeight;
        };

        vertex VertexOutput fullscreenVertex(uint vertexID [[vertex_id]]) {
            const float2 positions[4] = {
                float2(-1.0, -1.0),
                float2( 1.0, -1.0),
                float2(-1.0,  1.0),
                float2( 1.0,  1.0)
            };
            const float2 texCoords[4] = {
                float2(0.0, 1.0),
                float2(1.0, 1.0),
                float2(0.0, 0.0),
                float2(1.0, 0.0)
            };
            VertexOutput out;
            out.position = float4(positions[vertexID], 0.0, 1.0);
            out.texCoords = texCoords[vertexID];
            return out;
        }

        // Row coordinates of the two taps used to reconstruct one field.
        //
        // Both taps are always two frame rows apart and share the field's
        // parity, so the lines missing from this field are interpolated from
        // the field being shown -- never blended with the other field, which is
        // 20 ms older and is what produced the combing in the first place.
        static float2 fieldTapRows(float uvY, float h, float parity, thread float &frac) {
            float yPix = uvY * h - 0.5;
            float fieldPos = (yPix - parity) * 0.5;
            float k0 = floor(fieldPos);
            frac = fieldPos - k0;
            float rowsInField = floor((h - parity + 1.0) * 0.5);
            float c0 = clamp(k0,       0.0, rowsInField - 1.0);
            float c1 = clamp(k0 + 1.0, 0.0, rowsInField - 1.0);
            return float2((2.0 * c0 + parity + 0.5) / h,
                          (2.0 * c1 + parity + 0.5) / h);
        }

        fragment float4 deinterlaceFragment(
            VertexOutput in [[stage_in]],
            texture2d<float, access::sample> textureY [[texture(0)]],
            texture2d<float, access::sample> textureCbCr [[texture(1)]],
            constant DeinterlaceParams &params [[buffer(0)]]
        ) {
            constexpr sampler s_point(mag_filter::nearest, min_filter::nearest, address::clamp_to_edge);
            constexpr sampler s_linear(mag_filter::linear, min_filter::linear, address::clamp_to_edge);

            float2 uv = in.texCoords;
            float yRaw;
            float2 cbcrRaw;

            if (params.isInterlaced != 0) {
                float parity = float(params.fieldParity);

                float fracY;
                float2 rowsY = fieldTapRows(uv.y, params.lumaHeight, parity, fracY);
                yRaw = mix(textureY.sample(s_point, float2(uv.x, rowsY.x)).r,
                           textureY.sample(s_point, float2(uv.x, rowsY.y)).r,
                           fracY);

                // 4:2:0 interlaced subsamples each field separately, so the
                // chroma plane carries the same parity interleave as luma.
                float fracC;
                float2 rowsC = fieldTapRows(uv.y, params.chromaHeight, parity, fracC);
                cbcrRaw = mix(textureCbCr.sample(s_point, float2(uv.x, rowsC.x)).rg,
                              textureCbCr.sample(s_point, float2(uv.x, rowsC.y)).rg,
                              fracC);
            } else {
                yRaw = textureY.sample(s_linear, uv).r;
                cbcrRaw = textureCbCr.sample(s_linear, uv).rg;
            }

            // VideoToolbox kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange:
            float yNorm = clamp((yRaw - 0.06275) * 1.16438, 0.0, 1.0);
            float cb = cbcrRaw.r - 0.50196;
            float cr = cbcrRaw.g - 0.50196;

            // ITU-R BT.709 HDTV color matrix for 1080i HD broadcast
            float r = yNorm + 1.79274 * cr;
            float g = yNorm - 0.21325 * cb - 0.53291 * cr;
            float b = yNorm + 2.11240 * cb;

            return float4(clamp(float3(r, g, b), 0.0, 1.0), 1.0);
        }

        fragment float4 presentFragment(
            VertexOutput in [[stage_in]],
            texture2d<float, access::sample> source [[texture(0)]]
        ) {
            constexpr sampler s_linear(mag_filter::linear, min_filter::linear, address::clamp_to_edge);
            return source.sample(s_linear, in.texCoords);
        }
        """

        do {
            let library = try metalDevice.makeLibrary(source: shaderSource, options: nil)
            let vertexFunction = library.makeFunction(name: "fullscreenVertex")

            let deinterlaceDescriptor = MTLRenderPipelineDescriptor()
            deinterlaceDescriptor.vertexFunction = vertexFunction
            deinterlaceDescriptor.fragmentFunction = library.makeFunction(name: "deinterlaceFragment")
            deinterlaceDescriptor.colorAttachments[0].pixelFormat = .bgra8Unorm
            deinterlacePipeline = try metalDevice.makeRenderPipelineState(descriptor: deinterlaceDescriptor)

            let presentDescriptor = MTLRenderPipelineDescriptor()
            presentDescriptor.vertexFunction = vertexFunction
            presentDescriptor.fragmentFunction = library.makeFunction(name: "presentFragment")
            presentDescriptor.colorAttachments[0].pixelFormat = .bgra8Unorm
            presentPipeline = try metalDevice.makeRenderPipelineState(descriptor: presentDescriptor)
        } catch {
            let errorLog = "[1080i50-METAL-FAIL] Shader compile error: \(error)"
            print(errorLog)
            logger.error("\(errorLog, privacy: .public)")
            TelemetryServer.shared.log(errorLog)
        }
    }

    private func setupDisplayLink() {
        let link = CADisplayLink(target: self, selector: #selector(displayLinkFired(_:)))
        if #available(iOS 15.0, *) {
            // ProMotion display range (50-120 Hz, preferred 100/60 Hz).
            // On ProMotion hardware (iPhone 13-16 Pro), setting maximum 50 Hz causes iOS to snap
            // down to 48 Hz, creating a 48 Hz vs 50 Hz cadence mismatch that drops 2 fields/sec.
            // Allowing preferred 100 Hz gives integer 2 ticks per 50 Hz field with zero dropped frames.
            link.preferredFrameRateRange = CAFrameRateRange(minimum: 50, maximum: 120, preferred: 100)
        }
        link.add(to: .main, forMode: .common)
        self.displayLink = link
        self.lastTelemetryUpdate = CACurrentMediaTime()
    }

    nonisolated public func enqueueFrame(_ frame: DecodedVideoFrame) {
        let dropped = reorderBuffer.push(frame)
        if dropped {
            telemetry?.mutate { $0.droppedFrames += 1 }
        }
    }

    // MARK: - Presentation

    @objc private func displayLinkFired(_ link: CADisplayLink) {
        guard UIApplication.shared.applicationState == .active else { return }

        callbackCount += 1
        drainReorderedFrames()

        let targetHost = link.targetTimestamp
        var streamNow: Double

        let syncRate = synchronizer.map { CMTimebaseGetRate($0.timebase) } ?? 0.0
        if syncRate > 0, let sync = synchronizer {
            let baseTime = CMTimebaseGetTime(sync.timebase)
            var currentStreamTime = baseTime.isValid ? baseTime.seconds : 0.0

            // If audio clock and video stream drifted apart (e.g. during initial PSI/SPS lock-in),
            // resync the master clock to the video stream head so playback starts immediately without freezing.
            if let first = fieldQueue.first {
                let drift = first.ptsSeconds - currentStreamTime
                if abs(drift) > 0.200 {
                    CMTimebaseSetTime(sync.timebase, time: CMTime(seconds: first.ptsSeconds, preferredTimescale: 90000))
                    currentStreamTime = first.ptsSeconds
                }
            }

            streamNow = currentStreamTime
            isClockAnchored = true
        } else {
            if !isClockAnchored {
                guard let first = fieldQueue.first, let last = fieldQueue.last,
                      last.ptsSeconds - first.ptsSeconds >= Self.targetLatencySeconds else {
                    return
                }
                anchorHost = targetHost
                anchorStream = first.ptsSeconds
                isClockAnchored = true
            }

            streamNow = anchorStream + (targetHost - anchorHost)

            // Coming out of an underrun, resume from the field that actually
            // arrived. The clock kept running through the gap, so everything
            // buffered during the stall is already past due and would be consumed
            // and discarded in a single tick — that cascade is what held the field
            // rate at 40–49/s and skewed the top/bottom balance, because a burst of
            // two fields always kept the second one.
            if isStarved, let next = fieldQueue.first {
                anchorHost = targetHost
                anchorStream = next.ptsSeconds
                streamNow = next.ptsSeconds
                isStarved = false
                starveResyncCount += 1
            }

            // The decoder and the display clock drift apart, so the anchor is
            // trimmed rather than the interval reset, keeping presentation monotonic.
            if let newest = fieldQueue.last {
                let latency = newest.ptsSeconds - streamNow

                if latency < 0 {
                    // The clock has overrun everything buffered. Hold it where it is
                    // and let the queue refill. Advancing only ages fields that have
                    // not arrived yet, and moving the anchor backwards — which the
                    // previous resync did — re-presents fields already shown.
                    anchorHost = targetHost
                    anchorStream = streamNow
                    clockHoldCount += 1
                } else {
                    let error = latency - Self.targetLatencySeconds
                    // Deliberately a very weak trim. Broadcast PTS advances at real
                    // time, so the clock needs no real rate correction — only a
                    // nudge for buffer level. A stronger gain made the queue's own
                    // burstiness dominate: it sits above target more often than
                    // below, so the mean correction came out positive, the clock ran
                    // 3–5 % fast, and consumption (52 fields/s) outran supply
                    // (median 48 fields/s) until the buffer starved.
                    let tick = link.duration > 0 ? link.duration : 1.0 / 120.0
                    let maxTrim = tick * 0.01
                    let correction = max(-maxTrim, min(maxTrim, error * 0.01))
                    anchorStream += correction
                    streamNow += correction
                }
            }
        }

        // Instant first picture presentation:
        // Display the very first decoded keyframe immediately so the screen lights up instantly!
        if !hasReportedFirstFrame, let first = fieldQueue.first {
            currentField = first
            renderField(first, isFirstFrame: true)
            hasReportedFirstFrame = true
            onFirstFrameRendered?()
        }

        // Memory & Thermal Optimization: Bound field queue depth to prevent buffer ballooning.
        // If the queue exceeds 25 fields (>500ms), prune stale fields behind the current stream clock.
        if fieldQueue.count > 25 {
            while fieldQueue.count > 20, let first = fieldQueue.first, first.ptsSeconds <= streamNow {
                _ = fieldQueue.removeFirst()
            }
        }

        var presentedNewField = false
        while let next = fieldQueue.first, next.ptsSeconds <= streamNow {
            if presentedNewField {
                // If a field was already selected for this display tick, only discard subsequent
                // fields if they are genuinely stale (> 100ms behind master timebase).
                if streamNow - next.ptsSeconds > 0.100 {
                    skippedLateFieldCount += 1
                } else {
                    break
                }
            }
            currentField = fieldQueue.removeFirst()
            presentedNewField = true
        }

        if fieldQueue.isEmpty {
            isStarved = true
        }

        guard let field = currentField else { return }

        if presentedNewField {
            if field.parity == 0 { topFieldCount += 1 } else { bottomFieldCount += 1 }

            if lastDistinctPresentationHost > 0 {
                let interval = targetHost - lastDistinctPresentationHost
                fieldIntervalAccumulator += interval * 1000.0
                let ideal = sourceIsInterlaced ? estimatedFrameDuration * 0.5 : estimatedFrameDuration
                jitterAccumulator += abs(interval - ideal) * 1000.0
                fieldIntervalSamples += 1
            }
            lastDistinctPresentationHost = targetHost
        } else {
            repeatedPresentationCount += 1
            cumulativeRepeatedFieldCount += 1
        }

        // Re-showing a field costs no draw: the drawable already on screen stays
        // there until a new one is presented. Rendering only new fields holds GPU
        // work at the 50 fields/s the source carries rather than at the display
        // refresh rate, which at 120 Hz would be more than twice the work.
        let isFirst = !hasReportedFirstFrame
        if presentedNewField || needsRedraw || isFirst {
            needsRedraw = false
            renderField(field, isFirstFrame: isFirst)
            presentationCount += 1

            if isFirst {
                hasReportedFirstFrame = true
                onFirstFrameRendered?()
            }
        }

        if targetHost - lastTelemetryUpdate >= 1.0 {
            publishRenderTelemetry(now: targetHost, streamNow: streamNow, tick: link.duration)
        }
    }

    /// Moves frames whose reorder window has closed into the field queue, in
    /// presentation order, expanding each into the presentations it carries.
    ///
    /// How many that is depends on how the picture was coded. A frame picture
    /// holds both fields woven together and becomes two presentations 20 ms
    /// apart; a discrete coded field (PAFF, which this broadcast mixes in) is
    /// already one field and becomes one. Splitting the latter as well is what
    /// produced the late-drop bursts and the skewed top/bottom balance.
    private func drainReorderedFrames() {
        for released in reorderBuffer.drainSettled(allowImmediateFirst: !hasReportedFirstFrame) {
            let frame = released.frame
            let pts: Double
            if frame.pts.isValid {
                pts = frame.pts.seconds
            } else if lastReleasedPTS.isFinite {
                // An access unit that carried no PES timestamp. Keep it in the
                // stream's cadence rather than discarding it.
                pts = lastReleasedPTS + estimatedFrameDuration
            } else {
                continue
            }
            lastReleasedPTS = pts

            // Gap to the next picture in presentation order, bucketed before any
            // filtering so clustered and backwards timestamps stay visible.
            let rawGap = released.gapToNext
            if rawGap.isFinite {
                if rawGap < 0 { gapNegativeCount += 1 }
                else if rawGap < 0.005 { gapClusteredCount += 1 }
                else if rawGap < 0.030 { gapHalfFrameCount += 1 }
                else if rawGap < 0.050 { gapFrameCount += 1 }
                else { gapWideCount += 1 }
            }

            // Broadcast PTS is quantised to 90 kHz, so treat only plausible gaps
            // as signal for the duration estimate.
            var gap = rawGap
            if !(gap.isFinite && gap > 0.008 && gap < 0.2) { gap = .nan }
            if gap.isFinite {
                observedPTSDeltaMs = gap * 1000.0
                // Only frame-picture gaps feed the estimate. Field-picture gaps
                // are half a frame and were previously scaled up to compensate,
                // which just fed the scaling error straight into the estimate.
                if !frame.structure.isFieldPicture {
                    recentFrameGaps.append(gap)
                    if recentFrameGaps.count > Self.frameGapWindow {
                        recentFrameGaps.removeFirst(recentFrameGaps.count - Self.frameGapWindow)
                    }
                }
            }

            guard sourceIsInterlaced else {
                fieldQueue.append(PendingField(pixelBuffer: frame.pixelBuffer, parity: 0, ptsSeconds: pts))
                wovenFrameCount += 1
                continue
            }

            // VideoToolbox hands back a full-height buffer either way — the
            // `Buf:` diagnostic reads 1920x1080 for field pictures too — so both
            // cases are bob-reconstructed. What differs is the number of
            // presentations, and that comes from the slice header rather than
            // from PTS spacing, which wobbles enough to misclassify.
            if frame.structure.isFieldPicture {
                let parity: UInt32 = frame.structure.isBottomField ? 1 : 0
                appendField(frame.pixelBuffer, parity: parity, pts: pts)
                discreteFieldCount += 1
            } else {
                let firstParity: UInt32 = frame.structure.isTopFieldFirst ? 0 : 1
                let secondParity: UInt32 = 1 - firstParity
                // Half the measured gap when it is trustworthy, so the second
                // field lands where the stream actually puts it.
                let half = (gap.isFinite ? gap : estimatedFrameDuration) * 0.5
                appendField(frame.pixelBuffer, parity: firstParity, pts: pts)
                appendField(frame.pixelBuffer, parity: secondParity, pts: pts + half)
                wovenFrameCount += 1
            }
        }
    }

    /// Places a field on the presentation timeline, separating only timestamps
    /// that would land in the same display tick.
    ///
    /// This is a safety net, not the schedule: two fields at one instant make
    /// the second invisible by construction, so near-duplicates are pushed
    /// apart. Everything else keeps its own PTS.
    ///
    /// The separation threshold is deliberately tight, and the result is bounded
    /// against the field's own timestamp. An earlier version pushed anything
    /// closer than three quarters of a half-frame, which ratcheted: one
    /// correction moved the timeline ahead, every later field then read as "too
    /// early", and scheduling silently became a fixed metronome that ignored the
    /// stream. Both guards below exist to keep that from recurring.
    private func appendField(_ pixelBuffer: CVPixelBuffer, parity: UInt32, pts: Double) {
        let frameDuration = estimatedFrameDuration
        var placed = pts

        if lastQueuedFieldPTS.isFinite {
            // A jump this large is a stream discontinuity, not sloppy spacing;
            // follow it instead of extending the old timeline forever.
            let isDiscontinuity = abs(pts - lastQueuedFieldPTS) > 1.0

            if !isDiscontinuity {
                // A field whose timestamp sits behind the timeline arrived too
                // late to be shown in order. Dropping it is the only honest
                // option: placing it anyway put it *before* its predecessor,
                // which unsorted the queue and made the scheduler discard the
                // surrounding fields as superseded.
                if pts < lastQueuedFieldPTS - frameDuration * 0.5 {
                    lateFieldDropCount += 1
                    telemetry?.mutate { $0.droppedFrames += 1 }
                    return
                }

                if placed < lastQueuedFieldPTS + Self.minFieldSeparation {
                    // Bounded so a correction cannot compound into a permanent
                    // offset, but never below the previous field: the timeline
                    // has to stay monotonic.
                    placed = min(lastQueuedFieldPTS + frameDuration * 0.5, pts + frameDuration)
                    placed = max(placed, lastQueuedFieldPTS + Self.minFieldSeparation)
                    fieldSpacingCorrections += 1
                }
            }
        }

        lastQueuedFieldPTS = placed
        fieldQueue.append(PendingField(pixelBuffer: pixelBuffer, parity: parity, ptsSeconds: placed))
    }

    private func publishRenderTelemetry(now: CFTimeInterval, streamNow: Double, tick: CFTimeInterval) {
        let elapsed = now - lastTelemetryUpdate
        guard elapsed > 0 else { return }

        // Ratio of stream time consumed to real time elapsed. 1.0 means the
        // presentation clock tracks the wall clock; below 1.0 the clock is being
        // held back and queued fields cannot come due however many are waiting.
        let streamAdvanceRatio = lastPublishedStreamNow.isFinite
            ? (streamNow - lastPublishedStreamNow) / elapsed
            : Double.nan
        lastPublishedStreamNow = streamNow

        // Mean PTS spacing of the fields actually waiting, which should be half
        // a frame. A wider spacing means they were placed wrong, not withheld.
        var queueSpacingMs = Double.nan
        if fieldQueue.count >= 2 {
            let span = fieldQueue[fieldQueue.count - 1].ptsSeconds - fieldQueue[0].ptsSeconds
            queueSpacingMs = (span / Double(fieldQueue.count - 1)) * 1000.0
        }

        let callbacks = Double(callbackCount) / elapsed
        let presentations = Double(presentationCount) / elapsed
        let topFields = Double(topFieldCount) / elapsed
        let bottomFields = Double(bottomFieldCount) / elapsed
        let repeats = Double(repeatedPresentationCount) / elapsed
        let distinctFields = topFields + bottomFields
        let avgCadence = fieldIntervalSamples > 0 ? (fieldIntervalAccumulator / Double(fieldIntervalSamples)) : 0.0
        let avgJitter = fieldIntervalSamples > 0 ? (jitterAccumulator / Double(fieldIntervalSamples)) : 0.0

        let sourceFrames = estimatedFrameDuration > 0 ? 1.0 / estimatedFrameDuration : 0.0
        let sourceFields = sourceIsInterlaced ? sourceFrames * 2.0 : sourceFrames
        let cumulativeRepeats = cumulativeRepeatedFieldCount
        let queuedFields = fieldQueue.count
        let observedDelta = observedPTSDeltaMs
        let lateFieldTotal = skippedLateFieldCount

        telemetry?.mutate {
            $0.displayCallbacksPerSec = callbacks
            $0.presentedFramesPerSec = presentations
            $0.topFieldsPerSec = topFields
            $0.bottomFieldsPerSec = bottomFields
            $0.fieldsSubmittedPerSec = distinctFields
            $0.generatedFieldsPerSec = distinctFields
            $0.repeatedFieldsPerSec = repeats
            $0.repeatedFieldCount = cumulativeRepeats
            $0.fieldCadenceMs = avgCadence
            $0.presentationJitterMs = avgJitter
            $0.lateFrames = lateFieldTotal
            $0.queuedFieldCount = queuedFields
            // Derived from the stream's own PTS cadence, not from how often the
            // renderer happened to draw.
            $0.sourceFrameRate = sourceFrames
            $0.sourceFieldRate = sourceFields
            $0.ptsProgressionMs = observedDelta
        }

        let bufferSize = currentField.map { "\(CVPixelBufferGetWidth($0.pixelBuffer))x\(CVPixelBufferGetHeight($0.pixelBuffer))" } ?? "—"
        let hz = tick > 0 ? 1.0 / tick : 0.0
        let gaps = "clustered:\(gapClusteredCount) half:\(gapHalfFrameCount) frame:\(gapFrameCount) wide:\(gapWideCount) neg:\(gapNegativeCount)"
        let frameDurMs = estimatedFrameDuration * 1000.0
        var minSpacingMs = Double.nan
        if fieldQueue.count >= 2 {
            var smallest = Double.greatestFiniteMagnitude
            for index in 1..<fieldQueue.count {
                smallest = min(smallest, fieldQueue[index].ptsSeconds - fieldQueue[index - 1].ptsSeconds)
            }
            minSpacingMs = smallest * 1000.0
        }
        let perfLog = "[1080i50-PERF] Fields: \(String(format: "%.1f", distinctFields))/s (T\(String(format: "%.1f", topFields))/B\(String(format: "%.1f", bottomFields))) | Callbacks: \(String(format: "%.1f", callbacks))/s @\(String(format: "%.0f", hz))Hz | Draws: \(String(format: "%.1f", presentations))/s | Repeats: \(String(format: "%.1f", repeats))/s | Cadence: \(String(format: "%.2f", avgCadence))ms | Jitter: \(String(format: "%.2f", avgJitter))ms | Queue: \(queuedFields) fields @\(String(format: "%.1f", queueSpacingMs))ms (min \(String(format: "%.1f", minSpacingMs))ms) | FrameDur: \(String(format: "%.1f", frameDurMs))ms | ClockRatio: \(String(format: "%.3f", streamAdvanceRatio)) | StarveResync: \(starveResyncCount) | ClockHold: \(clockHoldCount) | LateDrop: \(skippedLateFieldCount) | Pictures: \(wovenFrameCount) woven / \(discreteFieldCount) field | Gaps: \(gaps) | SpacingFix: \(fieldSpacingCorrections) | OutOfOrder: \(lateFieldDropCount) | Buf: \(bufferSize)"
        print(perfLog)
        logger.notice("\(perfLog, privacy: .public)")
        TelemetryServer.shared.log(perfLog)

        resetPerSecondCounters()
        lastTelemetryUpdate = now
    }

    /// Clears everything accumulated over one reporting window. Shared with the
    /// resume path, which must not carry counters across a suspension.
    private func resetPerSecondCounters() {
        callbackCount = 0
        presentationCount = 0
        topFieldCount = 0
        bottomFieldCount = 0
        repeatedPresentationCount = 0
        fieldIntervalAccumulator = 0
        fieldIntervalSamples = 0
        jitterAccumulator = 0
        wovenFrameCount = 0
        discreteFieldCount = 0
        starveResyncCount = 0
        clockHoldCount = 0
        fieldSpacingCorrections = 0
        lateFieldDropCount = 0
        gapClusteredCount = 0
        gapHalfFrameCount = 0
        gapFrameCount = 0
        gapWideCount = 0
        gapNegativeCount = 0
    }

    // MARK: - Rendering

    private func makeTextures(from pixelBuffer: CVPixelBuffer) -> (MTLTexture, MTLTexture)? {
        guard let cache = textureCache else { return nil }

        let width = CVPixelBufferGetWidth(pixelBuffer)
        let height = CVPixelBufferGetHeight(pixelBuffer)

        var textureY: CVMetalTexture?
        let yStatus = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, cache, pixelBuffer, nil, .r8Unorm, width, height, 0, &textureY
        )

        var textureCbCr: CVMetalTexture?
        let cbcrStatus = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, cache, pixelBuffer, nil, .rg8Unorm, width / 2, height / 2, 1, &textureCbCr
        )

        // Both creations fail under memory pressure and return nil. Force-unwrapping
        // them here used to turn a recoverable dropped frame into a crash.
        guard yStatus == kCVReturnSuccess, cbcrStatus == kCVReturnSuccess,
              let cvTextureY = textureY, let cvTextureCbCr = textureCbCr,
              let yTex = CVMetalTextureGetTexture(cvTextureY),
              let cbcrTex = CVMetalTextureGetTexture(cvTextureCbCr) else {
            return nil
        }
        return (yTex, cbcrTex)
    }

    private func offscreenTarget(width: Int, height: Int) -> MTLTexture? {
        if let existing = offscreenTexture, offscreenSize == (width, height) {
            return existing
        }
        let descriptor = MTLTextureDescriptor.texture2DDescriptor(
            pixelFormat: .bgra8Unorm, width: width, height: height, mipmapped: false
        )
        descriptor.usage = [.renderTarget, .shaderRead]
        descriptor.storageMode = .shared   // read back for /screenshot without a blit
        guard let texture = metalDevice.makeTexture(descriptor: descriptor) else { return nil }
        offscreenTexture = texture
        offscreenSize = (width, height)
        return texture
    }

    /// Encodes the field reconstruction pass into `offscreenTarget`.
    private func encodeDeinterlace(
        _ field: PendingField,
        into target: MTLTexture,
        commandBuffer: MTLCommandBuffer
    ) -> Bool {
        guard let pipeline = deinterlacePipeline,
              let (yTex, cbcrTex) = makeTextures(from: field.pixelBuffer) else {
            return false
        }

        let descriptor = MTLRenderPassDescriptor()
        descriptor.colorAttachments[0].texture = target
        descriptor.colorAttachments[0].loadAction = .dontCare
        descriptor.colorAttachments[0].storeAction = .store

        guard let encoder = commandBuffer.makeRenderCommandEncoder(descriptor: descriptor) else {
            return false
        }

        let height = CVPixelBufferGetHeight(field.pixelBuffer)
        var params = DeinterlaceShaderParams(
            fieldParity: field.parity,
            isInterlaced: sourceIsInterlaced ? 1 : 0,
            lumaHeight: Float(height),
            chromaHeight: Float(height / 2)
        )

        encoder.setRenderPipelineState(pipeline)
        encoder.setFragmentTexture(yTex, index: 0)
        encoder.setFragmentTexture(cbcrTex, index: 1)
        encoder.setFragmentBytes(&params, length: MemoryLayout<DeinterlaceShaderParams>.stride, index: 0)
        encoder.drawPrimitives(type: .triangleStrip, vertexStart: 0, vertexCount: 4)
        encoder.endEncoding()
        return true
    }

    private func renderField(_ field: PendingField, isFirstFrame: Bool) {
        guard UIApplication.shared.applicationState == .active,
              let deinterlacePipeline = deinterlacePipeline,
              let (yTex, cbcrTex) = makeTextures(from: field.pixelBuffer),
              let commandBuffer = commandQueue.makeCommandBuffer() else {
            return
        }

        // Direct single-pass render into the drawable texture:
        // Eliminates 8.3 MB per-field intermediate DRAM round-trip (415 MB/s memory bandwidth),
        // processing bob deinterlacing & BT.709 color conversion entirely in fast on-chip tile SRAM.
        guard let drawable = metalLayer.nextDrawable() else {
            commandBuffer.commit()
            telemetry?.mutate { $0.droppedFrames += 1 }
            return
        }

        let descriptor = MTLRenderPassDescriptor()
        descriptor.colorAttachments[0].texture = drawable.texture
        descriptor.colorAttachments[0].loadAction = .dontCare
        descriptor.colorAttachments[0].storeAction = .store

        guard let encoder = commandBuffer.makeRenderCommandEncoder(descriptor: descriptor) else {
            commandBuffer.commit()
            return
        }

        let height = CVPixelBufferGetHeight(field.pixelBuffer)
        var params = DeinterlaceShaderParams(
            fieldParity: field.parity,
            isInterlaced: sourceIsInterlaced ? 1 : 0,
            lumaHeight: Float(height),
            chromaHeight: Float(height / 2)
        )

        encoder.setRenderPipelineState(deinterlacePipeline)
        encoder.setFragmentTexture(yTex, index: 0)
        encoder.setFragmentTexture(cbcrTex, index: 1)
        encoder.setFragmentBytes(&params, length: MemoryLayout<DeinterlaceShaderParams>.stride, index: 0)
        encoder.drawPrimitives(type: .triangleStrip, vertexStart: 0, vertexCount: 4)
        encoder.endEncoding()

        if isFirstFrame {
            commandBuffer.addCompletedHandler { [weak self] _ in
                let gpuTime = CACurrentMediaTime()
                DispatchQueue.main.async {
                    self?.onFirstFrameActuallyPresentedOnScreen?(gpuTime)
                }
            }
        }

        commandBuffer.present(drawable)
        commandBuffer.commit()
    }

    // MARK: - Screenshot

    /// Renders the current field through the real deinterlace pass and reads it
    /// back, so `/screenshot` shows what the renderer produces rather than the
    /// raw interlaced decoder output.
    public func captureCurrentFrameJPEG() -> Data? {
        if let field = currentField,
           let jpeg = renderFieldToJPEG(field) {
            return jpeg
        }
        if let window = self.window {
            let renderer = UIGraphicsImageRenderer(bounds: window.bounds)
            let image = renderer.image { _ in
                window.drawHierarchy(in: window.bounds, afterScreenUpdates: false)
            }
            return image.jpegData(compressionQuality: 0.8)
        }
        return nil
    }

    private func renderFieldToJPEG(_ field: PendingField) -> Data? {
        let width = CVPixelBufferGetWidth(field.pixelBuffer)
        let height = CVPixelBufferGetHeight(field.pixelBuffer)

        guard width > 0, height > 0,
              let commandBuffer = commandQueue.makeCommandBuffer(),
              let target = offscreenTarget(width: width, height: height),
              encodeDeinterlace(field, into: target, commandBuffer: commandBuffer) else {
            return nil
        }
        commandBuffer.commit()
        commandBuffer.waitUntilCompleted()

        let bytesPerRow = width * 4
        var pixels = [UInt8](repeating: 0, count: bytesPerRow * height)
        pixels.withUnsafeMutableBytes { raw in
            guard let base = raw.baseAddress else { return }
            target.getBytes(
                base,
                bytesPerRow: bytesPerRow,
                from: MTLRegionMake2D(0, 0, width, height),
                mipmapLevel: 0
            )
        }

        let colorSpace = CGColorSpaceCreateDeviceRGB()
        let bitmapInfo = CGBitmapInfo(rawValue: CGImageAlphaInfo.noneSkipFirst.rawValue)
            .union(.byteOrder32Little)
        guard let provider = CGDataProvider(data: Data(pixels) as CFData),
              let cgImage = CGImage(
                width: width,
                height: height,
                bitsPerComponent: 8,
                bitsPerPixel: 32,
                bytesPerRow: bytesPerRow,
                space: colorSpace,
                bitmapInfo: bitmapInfo,
                provider: provider,
                decode: nil,
                shouldInterpolate: false,
                intent: .defaultIntent
              ) else {
            return nil
        }
        return UIImage(cgImage: cgImage).jpegData(compressionQuality: 0.8)
    }
}

/// Owns notification observer tokens and unregisters them when the view goes away.
///
/// A separate object because block-based observers need their token to be removed,
/// and `MetalVideoView`'s `deinit` cannot touch main-actor-isolated state.
private final class ObserverRegistration: @unchecked Sendable {
    private let tokens: [NSObjectProtocol]

    init(tokens: [NSObjectProtocol]) {
        self.tokens = tokens
    }

    deinit {
        for token in tokens {
            NotificationCenter.default.removeObserver(token)
        }
    }
}

/// A frame leaving the reorder buffer, with the PTS gap to its successor.
private struct ReleasedFrame {
    let frame: DecodedVideoFrame
    let gapToNext: Double   // seconds; NaN when either timestamp is missing
}

/// Buffers decoded frames until their PTS order has settled.
///
/// VideoToolbox emits in decode order, so a stream with B-frames delivers
/// frames whose PTS runs backwards. Presenting in arrival order would show them
/// out of sequence; holding a few frames and releasing the lowest PTS restores
/// presentation order.
private final class FrameReorderBuffer: @unchecked Sendable {
    private var frames: [DecodedVideoFrame] = []
    private let lock = NSLock()
    private let capacity: Int
    private let reorderDepth: Int

    /// `reorderDepth` 6 rather than 3: field pictures double the picture rate, so
    /// the same B-pyramid spans twice as many pictures, and a depth of 3 was
    /// releasing frames before a lower-PTS sibling had arrived — measured as
    /// field spacing of -80 ms, two frames of disorder. `capacity` 20 bounds the
    /// pixel buffers held away from VideoToolbox's pool.
    init(capacity: Int = 20, reorderDepth: Int = 6) {
        self.capacity = capacity
        self.reorderDepth = reorderDepth
    }

    /// Inserts by PTS. Returns true if the buffer overflowed and dropped a frame.
    func push(_ frame: DecodedVideoFrame) -> Bool {
        lock.lock()
        defer { lock.unlock() }

        // Frames without a PES timestamp cannot be ordered; append them so they
        // keep their arrival position and pick up a synthesized PTS downstream.
        if frame.pts.isValid {
            let key = frame.pts.seconds
            let index = frames.firstIndex { $0.pts.isValid && $0.pts.seconds > key } ?? frames.count
            frames.insert(frame, at: index)
        } else {
            frames.append(frame)
        }

        if frames.count > capacity {
            frames.removeFirst()
            return true
        }
        return false
    }

    /// Releases frames whose reorder window has closed, oldest PTS first, each
    /// paired with its gap to the following picture.
    ///
    /// The gap is what distinguishes a frame picture from a discrete coded
    /// field, and it costs no extra latency: `reorderDepth` frames are held
    /// back regardless, so the successor of every released frame is already in
    /// hand.
    func drainSettled(allowImmediateFirst: Bool = false) -> [ReleasedFrame] {
        lock.lock()
        defer { lock.unlock() }

        // Fast-track the very first keyframe for instantaneous Time-To-First-Picture (TTFP)
        if allowImmediateFirst, let first = frames.first {
            frames.removeFirst()
            let nextGap = frames.first.flatMap { next in
                (first.pts.isValid && next.pts.isValid) ? (next.pts.seconds - first.pts.seconds) : nil
            } ?? .nan
            return [ReleasedFrame(frame: first, gapToNext: nextGap)]
        }

        guard frames.count > reorderDepth, reorderDepth > 0 else { return [] }

        let releaseCount = frames.count - reorderDepth
        var released: [ReleasedFrame] = []
        released.reserveCapacity(releaseCount)

        for index in 0..<releaseCount {
            let frame = frames[index]
            let next = frames[index + 1]    // in range: reorderDepth >= 1
            let gap: Double
            if frame.pts.isValid && next.pts.isValid {
                gap = next.pts.seconds - frame.pts.seconds
            } else {
                gap = .nan
            }
            released.append(ReleasedFrame(frame: frame, gapToNext: gap))
        }

        frames.removeFirst(releaseCount)
        return released
    }

    func clear() {
        lock.lock()
        defer { lock.unlock() }
        frames.removeAll(keepingCapacity: true)
    }
}
