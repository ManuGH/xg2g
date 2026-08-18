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

/// A surface the GPU has finished with, on its way to AVFoundation.
///
/// `CVPixelBuffer` is not `Sendable`, and the Metal completion handler runs off
/// the main thread. The buffer is written by the GPU before that handler fires
/// and only read after it, so the handoff is sound — this states that rather
/// than leaving the compiler to assume the worst.
private struct FinishedField: @unchecked Sendable {
    let pixelBuffer: CVPixelBuffer
    let pts: CMTime
    let duration: CMTime
}

/// Luma and chroma views onto one decoded picture, together with the cache
/// wrappers that own them.
///
/// `CVMetalTextureGetTexture` hands back a texture owned by its `CVMetalTexture`;
/// the wrapper is what holds the binding to the underlying `IOSurface` open.
/// Returning only the `MTLTexture` — which this did — lets the wrapper die at the
/// end of the function that created it, before the command buffer has even been
/// committed.
private struct SourceTextures {
    let luma: MTLTexture
    let chroma: MTLTexture
    /// Must outlive every command buffer that samples `luma` or `chroma`.
    let wrappers: [CVMetalTexture]
}

/// Holds the surfaces a command buffer reads from until the GPU says it is done.
///
/// Two separate lifetimes were being ended too early, and both produce the same
/// symptom — an occasional field that is torn, or a blend of two pictures:
///
///   * the `CVMetalTexture` wrappers, whose release lets the texture cache
///     recycle the surface the encoder still points at, and
///   * the decoded `CVPixelBuffer` itself, which goes back to VideoToolbox's
///     pool as soon as the field is dropped from `fieldQueue` and is then handed
///     out again for the next picture while the GPU is still sampling it.
///
/// Retained by the completion handler, released with it.
private final class GPUSurfaceHold: @unchecked Sendable {
    private let pixelBuffers: [CVPixelBuffer]
    private let textures: [CVMetalTexture]

    init(pixelBuffers: [CVPixelBuffer], textures: [CVMetalTexture]) {
        self.pixelBuffers = pixelBuffers
        self.textures = textures
    }
}

/// Metal-based video rendering view with per-field bob deinterlacing and
/// PTS-driven presentation scheduling.
@MainActor
public final class MetalVideoView: UIView {

    private let metalDevice: MTLDevice
    private let commandQueue: MTLCommandQueue
    private var deinterlacePipeline: MTLRenderPipelineState?
    /// Field reconstruction straight into the two planes of an NV12 surface.
    /// Used by the system path; the drawable and screenshot paths keep the
    /// single-pass RGB pipeline above.
    private var deinterlaceLumaPipeline: MTLRenderPipelineState?
    private var deinterlaceChromaPipeline: MTLRenderPipelineState?
    private var presentPipeline: MTLRenderPipelineState?
    private var textureCache: CVMetalTextureCache?

    /// Full-resolution deinterlace target. Rendering the field reconstruction at
    /// native size and scaling in a second pass keeps the row-exact field taps
    /// from aliasing when the layer is smaller than the video, and gives
    /// `captureCurrentFrameJPEG` the actual on-screen pixels to read back.
    private var offscreenTexture: MTLTexture?
    private var offscreenSize: (width: Int, height: Int) = (0, 0)

    /// Where fields go under `.systemLayer`, and what hosts Picture in Picture.
    ///
    /// Required for that path but no longer sufficient to select it — see
    /// `presentationPath`. This is a different presentation model, not an extra
    /// output: the display layer schedules against the render synchronizer, so
    /// the field queue, the anchor clock and the starve/hold logic below are all
    /// bypassed while it is active. The system route costs a full render target
    /// per field, which is exactly the intermediate surface removed earlier for
    /// thermal reasons — which is why both paths are worth being able to compare.
    public weak var systemPresenter: SystemVideoPresenter? {
        didSet { applyPresentationPath() }
    }

    /// Which of the two presentation models is live.
    ///
    /// Both have always existed, but the choice was implicit in whether a
    /// presenter had been assigned — and the only caller assigns one
    /// unconditionally, so `.metalDrawable` and everything that serves it (the
    /// display link, the anchor clock, the starve resync, the drift trim, the
    /// queue bound) had become unreachable. Fixes aimed at that code landed on
    /// nothing. Naming the choice makes it selectable, and therefore measurable,
    /// which is what the two paths were kept for.
    public enum PresentationPath: String, Sendable {
        /// Fields go to `AVSampleBufferDisplayLayer`, scheduled by AVFoundation
        /// from their timestamps. Required for Picture in Picture.
        case systemLayer
        /// Fields are drawn into this view's own drawable, scheduled here
        /// against the stream clock.
        case metalDrawable
    }

    public var presentationPath: PresentationPath = .systemLayer {
        didSet {
            guard oldValue != presentationPath else { return }
            // What is queued belongs to the path that queued it: the display
            // layer holds sample buffers timed against the synchronizer, and the
            // field queue holds an anchor the other path never set.
            systemPresenter?.flush()
            resetForChannelZap()
            needsRedraw = true
            applyPresentationPath()
        }
    }

    /// Keeps `usesSystemPresentation` and the display layer's visibility in step
    /// with the selected path. The layer sits above the drawable, so leaving it
    /// on screen while drawing into the drawable shows a frozen picture.
    private func applyPresentationPath() {
        let useSystem = (presentationPath == .systemLayer) && (systemPresenter != nil)
        usesSystemPresentation = useSystem
        if let layer = systemPresenter?.displayLayer {
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            layer.isHidden = !useSystem
            CATransaction.commit()
        }
    }

    /// Mirrors the resolved `presentationPath` for the decoder's thread.
    ///
    /// `enqueueFrame` runs on the decoder queue and has to know which path is
    /// active before it can decide who drives field production. A single Bool
    /// write is atomic on ARM. It used to be written once during view setup;
    /// since the path is selectable it can also change mid-stream, and a read
    /// that catches the old value costs at most one misrouted frame: either a
    /// pump that finds the queue already flushed, or a frame left for the
    /// display link, which is about to run anyway. Both are no-ops.
    nonisolated(unsafe) private var usesSystemPresentation: Bool = false

    /// Pool for the surfaces handed to AVFoundation. A pool rather than fresh
    /// allocations: at 50 fields/s a 1080p NV12 buffer is 3.1 MB, and allocating
    /// one per field would churn ~155 MB/s through the allocator. The BGRA
    /// surface this replaced was 8.3 MB and 415 MB/s.
    private var pixelBufferPool: CVPixelBufferPool?
    private var poolSize: (width: Int, height: Int) = (0, 0)

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

    /// PTS of the last field placed on the timeline. Used only to keep the queue
    /// monotonic — never as the reference for judging whether the *next* field is
    /// crowded, which is what made the correction feed itself.
    private var lastQueuedFieldPTS: Double = .nan

    /// The last field's own, uncorrected PTS. Crowding and out-of-order are judged
    /// against this, so a correction cannot become the baseline for the next one.
    private var lastRawFieldPTS: Double = .nan
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

    /// The first field's presentation timestamp, reported once its surface is
    /// finished and handed to AVFoundation.
    ///
    /// Under `.systemLayer` that handover is *not* when anything becomes
    /// visible: the display layer honours the timestamp, so the picture appears
    /// when the master clock reaches it — which is gated on the audio pre-roll
    /// and can be hundreds of milliseconds later. Reporting the timestamp lets
    /// the pipeline watch the clock for it and measure the difference instead of
    /// calling the submit "time to first picture", which is what it was doing.
    public var onFirstFieldSubmitted: (@MainActor (CMTime) -> Void)?
    private var hasReportedFirstFrame: Bool = false

    private var callbackCount: Int = 0
    private var presentationCount: Int = 0
    private var topFieldCount: Int = 0
    private var bottomFieldCount: Int = 0
    private var repeatedPresentationCount: Int = 0
    private var cumulativeRepeatedFieldCount: Int = 0
    private var skippedLateFieldCount: Int = 0
    private var lastTelemetryUpdate: CFTimeInterval = 0

    /// Main-thread time the per-field deinterlace pass consumes, per second.
    ///
    /// The pass runs on the main actor for every decoded picture, and so does the
    /// display layer's own `requestMediaDataWhenReady` block. If this approaches
    /// a second per second, the two are competing and the feed loop loses.
    private var presentationCPUMs: Double = 0
    private var lastPresentationReport: CFTimeInterval = 0

    /// When the app last left the foreground, so PiP has room to start before
    /// anything decides nobody is watching.
    private var leftForegroundAt: CFTimeInterval = 0

    /// Long enough for an automatic PiP start to announce itself, short enough
    /// that a locked phone is not left rendering for a screen nobody sees.
    private static let backgroundGraceSeconds: Double = 1.5

    /// What the system path actually hands AVFoundation, per reporting window.
    ///
    /// Every field-production counter lived in `publishRenderTelemetry`, which
    /// only runs on the display link — so under system presentation the entire
    /// production side reported zeros. Spacing, the woven/field mix and the
    /// duration attached to each sample are exactly what decides whether the
    /// display layer can present at source rate, and none of it was visible.
    private var presentedFieldCount = 0
    private var presentedSpacingSum: Double = 0
    private var presentedSpacingMin: Double = .greatestFiniteMagnitude
    private var presentedSpacingMax: Double = 0
    private var lastPresentedFieldPTS: Double = .nan
    private var lastAttachedDurationMs: Double = 0

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
        lastRawFieldPTS = .nan
        recentFrameGaps.removeAll(keepingCapacity: true)

        lastDistinctPresentationHost = 0
        lastPresentedFieldPTS = .nan

        // Hand back the surfaces the previous stream's fields are holding.
        // Without this the pool keeps every buffer it ever grew to, so a session
        // of zapping accumulates the high-water mark of the worst channel and
        // never gives it back. `currentField` still references one, which is why
        // this frees the unused ones rather than the whole pool.
        if let pool = pixelBufferPool {
            CVPixelBufferPoolFlush(pool, .excessBuffers)
        }

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

        // The display layer is a sublayer, so Auto Layout never touches it and it
        // keeps whatever frame it was given. Set in `makeUIView` that frame is
        // `.zero` — the view has no bounds yet — which left a layer that received
        // and rendered every field while occupying no space: audio played, the
        // picture stayed black, and `isPictureInPicturePossible` reported false
        // because PiP needs a layer with real geometry.
        if let layer = systemPresenter?.displayLayer, layer.frame != bounds {
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            layer.frame = bounds
            CATransaction.commit()
        }
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

        // A motion-adaptive variant was tried here and removed: it estimated
        // motion from a single frame buffer, as |other field row - what this field
        // predicts for that row|. That difference measures vertical detail, not
        // motion — static text and fine horizontal edges produce exactly the same
        // large value as real movement, because interpolation cannot predict fine
        // detail either way. With a per-pixel decision and a 0.02 threshold (below
        // the noise floor), neighbouring pixels flipped between weave and bob
        // independently every frame. It read as grain and shimmering artefacts.
        //
        // Doing it properly needs the *previous* frame — same parity, compared
        // across time — not a second field from the same instant.

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

        // Field reconstruction with no colour conversion, one plane at a time.
        //
        // What the drawable path does in a single pass — reconstruct, then apply
        // a BT.709 video-range matrix — is wrong for anything that is not BT.709,
        // and every SD channel in the multiplex is BT.601. Writing YCbCr back out
        // untouched hands that decision to AVFoundation, which reads the matrix,
        // primaries and transfer function off the buffer VideoToolbox produced.
        // It is also 2.7x less memory than the BGRA surface it replaces.
        fragment float4 deinterlaceLumaFragment(
            VertexOutput in [[stage_in]],
            texture2d<float, access::sample> textureY [[texture(0)]],
            constant DeinterlaceParams &params [[buffer(0)]]
        ) {
            constexpr sampler s_point(mag_filter::nearest, min_filter::nearest, address::clamp_to_edge);
            constexpr sampler s_linear(mag_filter::linear, min_filter::linear, address::clamp_to_edge);

            if (params.isInterlaced == 0) {
                return float4(textureY.sample(s_linear, in.texCoords).r, 0.0, 0.0, 1.0);
            }

            float parity = float(params.fieldParity);
            float frac;
            float2 rows = fieldTapRows(in.texCoords.y, params.lumaHeight, parity, frac);
            float y = mix(textureY.sample(s_point, float2(in.texCoords.x, rows.x)).r,
                          textureY.sample(s_point, float2(in.texCoords.x, rows.y)).r,
                          frac);
            return float4(y, 0.0, 0.0, 1.0);
        }

        fragment float4 deinterlaceChromaFragment(
            VertexOutput in [[stage_in]],
            texture2d<float, access::sample> textureCbCr [[texture(0)]],
            constant DeinterlaceParams &params [[buffer(0)]]
        ) {
            constexpr sampler s_point(mag_filter::nearest, min_filter::nearest, address::clamp_to_edge);
            constexpr sampler s_linear(mag_filter::linear, min_filter::linear, address::clamp_to_edge);

            if (params.isInterlaced == 0) {
                float2 cbcr = textureCbCr.sample(s_linear, in.texCoords).rg;
                return float4(cbcr, 0.0, 1.0);
            }

            // 4:2:0 interlaced subsamples each field separately, so the chroma
            // plane carries the same parity interleave as luma.
            float parity = float(params.fieldParity);
            float frac;
            float2 rows = fieldTapRows(in.texCoords.y, params.chromaHeight, parity, frac);
            float2 cbcr = mix(textureCbCr.sample(s_point, float2(in.texCoords.x, rows.x)).rg,
                              textureCbCr.sample(s_point, float2(in.texCoords.x, rows.y)).rg,
                              frac);
            return float4(cbcr, 0.0, 1.0);
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

            let lumaDescriptor = MTLRenderPipelineDescriptor()
            lumaDescriptor.vertexFunction = vertexFunction
            lumaDescriptor.fragmentFunction = library.makeFunction(name: "deinterlaceLumaFragment")
            lumaDescriptor.colorAttachments[0].pixelFormat = .r8Unorm
            deinterlaceLumaPipeline = try metalDevice.makeRenderPipelineState(descriptor: lumaDescriptor)

            let chromaDescriptor = MTLRenderPipelineDescriptor()
            chromaDescriptor.vertexFunction = vertexFunction
            chromaDescriptor.fragmentFunction = library.makeFunction(name: "deinterlaceChromaFragment")
            chromaDescriptor.colorAttachments[0].pixelFormat = .rg8Unorm
            deinterlaceChromaPipeline = try metalDevice.makeRenderPipelineState(descriptor: chromaDescriptor)

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
            // State the cadence the content actually has and let ProMotion pick the
            // refresh rate for it — that is what the variable-refresh panel is for.
            //
            // Pinning the range to 60 Hz was the wrong lever. 1080i50 carries 50
            // distinct fields per second; against a fixed 60 Hz tick, a 20 ms field
            // cadence can only be resolved to 16.7 ms, so fields land one or two
            // ticks apart in an uneven pattern — judder that shows up as a high
            // `Repeats:` count. 100 Hz is the clean 2:1 multiple and is inside what
            // the panel can serve.
            //
            // The range is a preference, not a demand: the system stays free to run
            // slower under thermal or Low Power pressure, and re-drawing only on new
            // fields means a higher tick rate costs wakeups, not GPU work.
            link.preferredFrameRateRange = CAFrameRateRange(minimum: 60, maximum: 120, preferred: 100)
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

        // Under system presentation the display link cannot drive production:
        // `CADisplayLink` stops firing once the app is backgrounded, and that is
        // precisely when Picture in Picture is on screen. The picture froze while
        // audio — which runs on its own path — kept playing.
        //
        // Decode arrival drives it instead, which is also the more honest
        // trigger: AVFoundation schedules presentation from each field's PTS, so
        // there is nothing left for a screen-refresh tick to decide.
        if usesSystemPresentation {
            Task { @MainActor [weak self] in
                self?.pumpSystemPresentation()
            }
        }
    }

    /// Turns everything whose reorder window has closed into sample buffers.
    ///
    /// No scheduling here on purpose — the field queue is drained completely and
    /// handed over. Holding fields back would only duplicate the timing decision
    /// AVFoundation is already making from their timestamps.
    @MainActor
    private func pumpSystemPresentation() {
        guard let presenter = systemPresenter else { return }

        drainReorderedFrames()

        // Backgrounded with no PiP window, this is 50 deinterlace passes a second
        // drawn for nobody. The two paths that draw into our own layer have
        // guarded against it all along; the path that actually runs never did.
        //
        // PiP is the exception and has to keep going, because the picture people
        // are watching is the one this produces — and it is why the test is not
        // simply "are we in the background". PiP starts *as* the app leaves the
        // foreground, so for a moment it is neither running nor absent, and a
        // guard that cuts the feed in that moment stops it from ever starting.
        // Hence both the engaged flag, which is set before the transition, and a
        // grace period for the case where the auto-start has not fired yet.
        let backgrounded = UIApplication.shared.applicationState == .background
        if !backgrounded {
            leftForegroundAt = 0
        } else if leftForegroundAt == 0 {
            leftForegroundAt = CACurrentMediaTime()
        }
        let settled = leftForegroundAt > 0
            && CACurrentMediaTime() - leftForegroundAt >= Self.backgroundGraceSeconds

        if backgrounded, settled, !presenter.isPictureInPictureEngaged {
            // Dropped rather than held: by the time the app returns these fields
            // are behind the clock, and a queue kept across a lock screen is a
            // backlog to shed, not a head start.
            fieldQueue.removeAll(keepingCapacity: true)
            return
        }

        let started = CACurrentMediaTime()
        while !fieldQueue.isEmpty {
            let field = fieldQueue.removeFirst()
            currentField = field
            presentThroughSystemLayer(field, presenter: presenter)
        }

        flushTextureCache()
        presentationCPUMs += (CACurrentMediaTime() - started) * 1000.0

        let now = CACurrentMediaTime()
        if lastPresentationReport == 0 { lastPresentationReport = now }
        let window = now - lastPresentationReport
        if window >= 2.0 {
            let share = presentationCPUMs / (window * 1000.0) * 100.0
            let rate = Double(presentedFieldCount) / window
            let meanSpacing = presentedFieldCount > 1
                ? presentedSpacingSum / Double(presentedFieldCount - 1) * 1000.0
                : Double.nan
            let minSpacing = presentedSpacingMin == .greatestFiniteMagnitude ? Double.nan : presentedSpacingMin * 1000.0
            let msg = "[1080i50-PRESENT] Fields: \(String(format: "%.1f", rate))/s | Spacing: \(String(format: "%.1f", meanSpacing))ms mean (\(String(format: "%.1f", minSpacing))–\(String(format: "%.1f", presentedSpacingMax * 1000.0))ms) | Duration attached: \(String(format: "%.1f", lastAttachedDurationMs))ms | FrameDur est: \(String(format: "%.1f", estimatedFrameDuration * 1000.0))ms | Pictures: \(wovenFrameCount) woven / \(discreteFieldCount) field | SpacingFix: \(fieldSpacingCorrections) | OutOfOrder: \(lateFieldDropCount) | Main-thread render: \(String(format: "%.0f", presentationCPUMs))ms (\(String(format: "%.1f", share))%)"
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)

            telemetry?.mutate {
                $0.fieldsSubmittedPerSec = rate
                $0.sourceFrameRate = self.estimatedFrameDuration > 0 ? 1.0 / self.estimatedFrameDuration : 0
                $0.queuedFieldCount = self.fieldQueue.count
            }

            presentationCPUMs = 0
            presentedFieldCount = 0
            presentedSpacingSum = 0
            presentedSpacingMin = .greatestFiniteMagnitude
            presentedSpacingMax = 0
            wovenFrameCount = 0
            discreteFieldCount = 0
            fieldSpacingCorrections = 0
            lateFieldDropCount = 0
            lastPresentationReport = now
        }
    }

    // MARK: - Presentation

    @objc private func displayLinkFired(_ link: CADisplayLink) {
        // System presentation owns both production and scheduling; this tick has
        // nothing to contribute and must not consume the field queue behind it.
        guard !usesSystemPresentation else { return }

        guard UIApplication.shared.applicationState == .active else { return }

        callbackCount += 1
        drainReorderedFrames()

        let targetHost = link.targetTimestamp
        var streamNow: Double

        let syncRate = synchronizer.map { CMTimebaseGetRate($0.timebase) } ?? 0.0
        if syncRate > 0, let sync = synchronizer {
            let baseTime = CMTimebaseGetTime(sync.timebase)
            streamNow = baseTime.isValid ? baseTime.seconds : 0.0
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

        // Bound the queue so pixel buffers cannot balloon, but only *after* the
        // presenter has taken what is due.
        //
        // This ran before the presentation loop, sharing its `ptsSeconds <=
        // streamNow` condition — so it consumed each field in the very tick it
        // became due and the presenter never saw one. The picture froze on a
        // single frame while 100+ fields sat waiting, reported as `Fields: 0.0/s`
        // against `Repeats: 60.0/s`. Order is the whole fix: what is due belongs
        // to the presenter, and only a genuine backlog behind it is dropped.
        if fieldQueue.count > 25 {
            while fieldQueue.count > 20, let first = fieldQueue.first, first.ptsSeconds <= streamNow {
                _ = fieldQueue.removeFirst()
                lateFieldDropCount += 1
            }
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

        flushTextureCache()

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
                // A frame picture's two fields are half a *frame* apart, always.
                //
                // The gap to the next picture is only that distance when the next
                // picture is also a frame. PAFF mixes the two per picture, so on a
                // channel like Sky Sport F1 the successor is routinely a field
                // picture 20 ms away — halving that put the second field 10 ms
                // behind the first instead of 20, tight enough to trip the
                // separation floor below and start the correction ratchet.
                // The gap is used only when it plausibly spans a whole frame.
                let frameDuration = estimatedFrameDuration
                let spansFullFrame = gap.isFinite && gap >= frameDuration * 0.75
                let half = (spansFullFrame ? gap : frameDuration) * 0.5
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

        // Crowding is judged between the *incoming* timestamps, never against the
        // corrected timeline.
        //
        // Comparing against `lastQueuedFieldPTS` made the correction self-feeding:
        // once a field had been nudged 20 ms forward, the next field's own PTS
        // landed exactly on that nudged value, read as "coincident", and was
        // nudged in turn. On a PAFF channel this locked in permanently — 47 of 47
        // fields rewritten every second, the queue 2.3 s deep, and the presented
        // timeline no longer the stream's. Raw-to-raw, correctly spaced fields
        // pass through untouched and `placed == pts`, which is the normal case.

        // A jump this large is a stream discontinuity, not sloppy spacing; follow
        // it instead of extending the old timeline forever.
        let isDiscontinuity = lastRawFieldPTS.isFinite && abs(pts - lastRawFieldPTS) > 1.0

        if lastRawFieldPTS.isFinite && !isDiscontinuity {
            // A field whose timestamp sits behind its predecessor arrived too late
            // to be shown in order. Dropping it is the only honest option: placing
            // it anyway put it *before* its predecessor, which unsorted the queue
            // and made the scheduler discard the surrounding fields as superseded.
            if pts < lastRawFieldPTS - frameDuration * 0.5 {
                lateFieldDropCount += 1
                telemetry?.mutate { $0.droppedFrames += 1 }
                return
            }

            if pts < lastRawFieldPTS + Self.minFieldSeparation {
                // Two fields at one instant make the second invisible by
                // construction, so this pair genuinely has to be pushed apart.
                placed = min(lastRawFieldPTS + frameDuration * 0.5, pts + frameDuration)
                fieldSpacingCorrections += 1
            }
        }

        lastRawFieldPTS = pts

        // Keep the queue monotonic — but a discontinuity is allowed to move the
        // timeline wherever the stream went.
        if !isDiscontinuity, lastQueuedFieldPTS.isFinite {
            placed = max(placed, lastQueuedFieldPTS + Self.minFieldSeparation)
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

    private func makeTextures(from pixelBuffer: CVPixelBuffer) -> SourceTextures? {
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
        return SourceTextures(luma: yTex, chroma: cbcrTex, wrappers: [cvTextureY, cvTextureCbCr])
    }

    /// Releases cache entries nothing references any more.
    ///
    /// The cache keeps every texture it has vended until told otherwise, and each
    /// one pins an `IOSurface` — including the decoder's own output buffers, which
    /// VideoToolbox then cannot reuse. Cheap, and a no-op for anything a command
    /// buffer still holds through `GPUSurfaceHold`.
    private func flushTextureCache() {
        guard let cache = textureCache else { return }
        CVMetalTextureCacheFlush(cache, 0)
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
              let source = makeTextures(from: field.pixelBuffer) else {
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
        encoder.setFragmentTexture(source.luma, index: 0)
        encoder.setFragmentTexture(source.chroma, index: 1)
        encoder.setFragmentBytes(&params, length: MemoryLayout<DeinterlaceShaderParams>.stride, index: 0)
        encoder.drawPrimitives(type: .triangleStrip, vertexStart: 0, vertexCount: 4)
        encoder.endEncoding()

        // The encoder retains the `MTLTexture`s; nothing retains what they are
        // views onto. Both the cache wrappers and the decoded picture have to
        // survive until the GPU has read them.
        let hold = GPUSurfaceHold(pixelBuffers: [field.pixelBuffer], textures: source.wrappers)
        commandBuffer.addCompletedHandler { _ in withExtendedLifetime(hold) {} }
        return true
    }

    /// Reuses the pool while the video dimensions hold, rebuilding it when they change.
    private func pixelBufferPool(width: Int, height: Int) -> CVPixelBufferPool? {
        if let existing = pixelBufferPool, poolSize == (width, height) {
            return existing
        }

        let attributes: [CFString: Any] = [
            // Same format the decoder produces, so the deinterlace passes write
            // the planes back untouched and AVFoundation does the colour.
            kCVPixelBufferPixelFormatTypeKey: kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
            kCVPixelBufferWidthKey: width,
            kCVPixelBufferHeightKey: height,
            kCVPixelBufferMetalCompatibilityKey: kCFBooleanTrue as Any,
            // AVSampleBufferDisplayLayer needs an IOSurface to present without a copy.
            kCVPixelBufferIOSurfacePropertiesKey: [:] as CFDictionary
        ]
        // A minimum, not a maximum — `CVPixelBufferPoolCreatePixelBuffer` keeps
        // allocating past it. The ceiling that actually bounds this is the
        // presenter's queue depth, which is what a stalled consumer fills.
        // At 50 fields/s six buffers is ~120 ms of slack.
        let poolAttributes: [CFString: Any] = [kCVPixelBufferPoolMinimumBufferCountKey: 6]

        var pool: CVPixelBufferPool?
        let status = CVPixelBufferPoolCreate(
            kCFAllocatorDefault,
            poolAttributes as CFDictionary,
            attributes as CFDictionary,
            &pool
        )
        guard status == kCVReturnSuccess, let created = pool else {
            let msg = "[SystemVideo] ❌ CVPixelBufferPoolCreate failed: \(status)"
            print(msg)
            logger.error("\(msg, privacy: .public)")
            return nil
        }

        pixelBufferPool = created
        poolSize = (width, height)
        return created
    }

    /// Reconstructs one field into the two planes of an NV12 surface.
    ///
    /// Two passes rather than one because the planes are different sizes; both
    /// are cheap — a field tap, a lerp, and a write, with no colour matrix.
    private func encodeDeinterlaceToNV12(
        _ field: PendingField,
        into destination: CVPixelBuffer,
        commandBuffer: MTLCommandBuffer
    ) -> Bool {
        guard let lumaPipeline = deinterlaceLumaPipeline,
              let chromaPipeline = deinterlaceChromaPipeline,
              let cache = textureCache,
              let source = makeTextures(from: field.pixelBuffer) else {
            return false
        }

        let lumaWidth = CVPixelBufferGetWidthOfPlane(destination, 0)
        let lumaHeight = CVPixelBufferGetHeightOfPlane(destination, 0)
        let chromaWidth = CVPixelBufferGetWidthOfPlane(destination, 1)
        let chromaHeight = CVPixelBufferGetHeightOfPlane(destination, 1)

        var lumaCV: CVMetalTexture?
        var chromaCV: CVMetalTexture?
        let lumaStatus = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, cache, destination, nil, .r8Unorm, lumaWidth, lumaHeight, 0, &lumaCV
        )
        let chromaStatus = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, cache, destination, nil, .rg8Unorm, chromaWidth, chromaHeight, 1, &chromaCV
        )

        guard lumaStatus == kCVReturnSuccess, chromaStatus == kCVReturnSuccess,
              let lumaWrap = lumaCV, let chromaWrap = chromaCV,
              let lumaTarget = CVMetalTextureGetTexture(lumaWrap),
              let chromaTarget = CVMetalTextureGetTexture(chromaWrap) else {
            return false
        }

        // Field geometry is the *source* picture's, not the destination's.
        let sourceHeight = CVPixelBufferGetHeight(field.pixelBuffer)
        var params = DeinterlaceShaderParams(
            fieldParity: field.parity,
            isInterlaced: sourceIsInterlaced ? 1 : 0,
            lumaHeight: Float(sourceHeight),
            chromaHeight: Float(sourceHeight / 2)
        )

        func encodePlane(
            target: MTLTexture,
            pipeline: MTLRenderPipelineState,
            texture: MTLTexture
        ) -> Bool {
            let descriptor = MTLRenderPassDescriptor()
            descriptor.colorAttachments[0].texture = target
            descriptor.colorAttachments[0].loadAction = .dontCare
            descriptor.colorAttachments[0].storeAction = .store
            guard let encoder = commandBuffer.makeRenderCommandEncoder(descriptor: descriptor) else {
                return false
            }
            encoder.setRenderPipelineState(pipeline)
            encoder.setFragmentTexture(texture, index: 0)
            encoder.setFragmentBytes(&params, length: MemoryLayout<DeinterlaceShaderParams>.stride, index: 0)
            encoder.drawPrimitives(type: .triangleStrip, vertexStart: 0, vertexCount: 4)
            encoder.endEncoding()
            return true
        }

        guard encodePlane(target: lumaTarget, pipeline: lumaPipeline, texture: source.luma),
              encodePlane(target: chromaTarget, pipeline: chromaPipeline, texture: source.chroma) else {
            return false
        }

        let hold = GPUSurfaceHold(
            pixelBuffers: [field.pixelBuffer],
            textures: source.wrappers + [lumaWrap, chromaWrap]
        )
        commandBuffer.addCompletedHandler { _ in withExtendedLifetime(hold) {} }
        return true
    }

    /// Renders one field into a pooled surface and hands it to AVFoundation.
    ///
    /// The deinterlace pass is the same one the drawable path uses — it already
    /// targeted an arbitrary texture for `captureCurrentFrameJPEG`, so only the
    /// destination differs.
    private func presentThroughSystemLayer(_ field: PendingField, presenter: SystemVideoPresenter) {
        let width = CVPixelBufferGetWidth(field.pixelBuffer)
        let height = CVPixelBufferGetHeight(field.pixelBuffer)
        guard width > 0, height > 0,
              let pool = pixelBufferPool(width: width, height: height) else { return }

        var output: CVPixelBuffer?
        guard CVPixelBufferPoolCreatePixelBuffer(kCFAllocatorDefault, pool, &output) == kCVReturnSuccess,
              let destination = output else {
            telemetry?.mutate { $0.droppedFrames += 1 }
            return
        }

        // Carries the decoder's colour metadata — matrix, primaries, transfer
        // function, chroma siting — onto the surface the layer will present, so
        // AVFoundation converts using what the stream actually declared.
        CVBufferPropagateAttachments(field.pixelBuffer, destination)

        guard let commandBuffer = commandQueue.makeCommandBuffer(),
              encodeDeinterlaceToNV12(field, into: destination, commandBuffer: commandBuffer) else {
            telemetry?.mutate { $0.droppedFrames += 1 }
            return
        }

        // The surface must be finished before AVFoundation reads it, but waiting
        // here would block the display link. Enqueue from the completion handler
        // instead, which runs off the main thread.
        let pts = CMTime(seconds: field.ptsSeconds, preferredTimescale: 90_000)
        let duration = CMTime(
            seconds: sourceIsInterlaced ? estimatedFrameDuration * 0.5 : estimatedFrameDuration,
            preferredTimescale: 90_000
        )
        // Spacing is measured on what is handed over, not on what was queued:
        // the correction in `appendField` sits between the two.
        if lastPresentedFieldPTS.isFinite {
            let gap = field.ptsSeconds - lastPresentedFieldPTS
            presentedSpacingSum += gap
            presentedSpacingMin = min(presentedSpacingMin, gap)
            presentedSpacingMax = max(presentedSpacingMax, gap)
        }
        lastPresentedFieldPTS = field.ptsSeconds
        presentedFieldCount += 1
        lastAttachedDurationMs = duration.seconds * 1000.0

        let finished = FinishedField(pixelBuffer: destination, pts: pts, duration: duration)

        // Reported from the completion handler rather than from here. Committing
        // a command buffer says the work is queued, not done, and the surface is
        // not AVFoundation's until the GPU has written it — announcing the first
        // frame at commit time credited the pipeline with work it had not
        // finished.
        let isFirstField = !hasReportedFirstFrame
        if isFirstField { hasReportedFirstFrame = true }

        commandBuffer.addCompletedHandler { _ in
            DispatchQueue.main.async {
                presenter.enqueue(
                    pixelBuffer: finished.pixelBuffer,
                    pts: finished.pts,
                    duration: finished.duration
                )
                if isFirstField {
                    self.onFirstFrameRendered?()
                    self.onFirstFieldSubmitted?(finished.pts)
                }
            }
        }
        commandBuffer.commit()
    }

    /// Draws one field into the layer's own drawable. Used only when the stream
    /// is *not* presented through AVFoundation — `pumpSystemPresentation` is the
    /// entry point in that case, and having both paths able to deinterlace the
    /// same field is how a picture ends up rendered twice.
    private func renderField(_ field: PendingField, isFirstFrame: Bool) {
        guard UIApplication.shared.applicationState == .active,
              let deinterlacePipeline = deinterlacePipeline,
              let source = makeTextures(from: field.pixelBuffer),
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
        encoder.setFragmentTexture(source.luma, index: 0)
        encoder.setFragmentTexture(source.chroma, index: 1)
        encoder.setFragmentBytes(&params, length: MemoryLayout<DeinterlaceShaderParams>.stride, index: 0)
        encoder.drawPrimitives(type: .triangleStrip, vertexStart: 0, vertexCount: 4)
        encoder.endEncoding()

        let hold = GPUSurfaceHold(pixelBuffers: [field.pixelBuffer], textures: source.wrappers)
        commandBuffer.addCompletedHandler { _ in withExtendedLifetime(hold) {} }

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
