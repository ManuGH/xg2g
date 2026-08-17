// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Metal
import MetalKit
import OSLog
import QuartzCore
import UIKit

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "metal")

private struct DeinterlaceShaderParams {
    var isTopField: UInt32
    var isInterlaced: UInt32
    var isTopFieldFirst: UInt32
    var frameHeight: Float
}

/// Metal-based video rendering view with `CVMetalTextureCache` direct texture binding and 50 fps target presentation cadence.
@MainActor
public final class MetalVideoView: UIView {

    private let metalDevice: MTLDevice
    private let commandQueue: MTLCommandQueue
    private var pipelineState: MTLRenderPipelineState?
    private var textureCache: CVMetalTextureCache?

    private var displayLink: CADisplayLink?
    private let frameQueue = ThreadSafeFrameQueue()

    private var currentFrame: DecodedVideoFrame?
    private var currentFieldIsTop: Bool = true
    private var fieldCycleCount: Int = 0

    // Telemetry tracking & Startup gates
    public var telemetry: StreamTelemetry?
    public var onFirstFrameRendered: (@MainActor () -> Void)?
    public var onFirstFrameActuallyPresentedOnScreen: (@MainActor (Double) -> Void)?
    private var hasReportedFirstFrame: Bool = false
    private var lastDisplayTimestamp: CFTimeInterval = 0
    private var callbackCount: Int = 0
    private var presentationCount: Int = 0
    private var topFieldPresentationCount: Int = 0
    private var bottomFieldPresentationCount: Int = 0
    private var repeatedFieldPresentationCount: Int = 0
    private var cumulativeRepeatedFieldCount: Int = 0
    private var lastTelemetryUpdate: CFTimeInterval = 0
    private var jitterAccumulator: Double = 0
    private var jitterSampleCount: Int = 0
    private var fieldCadenceAccumulator: Double = 0

    public func resetForChannelZap() {
        frameQueue.clear()
        hasReportedFirstFrame = false
        // Keep currentFrame to avoid black flash between channel switches
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
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
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
            uint isTopField;
            uint isInterlaced;
            uint isTopFieldFirst;
            float frameHeight;
        };

        vertex VertexOutput deinterlaceVertexShader(uint vertexID [[vertex_id]]) {
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

        fragment float4 deinterlaceFragmentShader(
            VertexOutput in [[stage_in]],
            texture2d<float, access::sample> textureY [[texture(0)]],
            texture2d<float, access::sample> textureCbCr [[texture(1)]],
            constant DeinterlaceParams &params [[buffer(0)]]
        ) {
            constexpr sampler s(mag_filter::linear, min_filter::linear, address::clamp_to_edge);
            float2 uv = in.texCoords;
            float yValue = 0.0;

            if (params.isInterlaced == 0) {
                yValue = textureY.sample(s, uv).r;
            } else {
                float lineIndex = floor(uv.y * params.frameHeight);
                bool isEvenLine = (fmod(lineIndex, 2.0) == 0.0);
                bool fieldMatchesLine = (params.isTopField == 1) ? isEvenLine : !isEvenLine;

                if (fieldMatchesLine) {
                    yValue = textureY.sample(s, uv).r;
                } else {
                    float stepY = 1.0 / params.frameHeight;
                    float yAbove = textureY.sample(s, float2(uv.x, uv.y - stepY)).r;
                    float yBelow = textureY.sample(s, float2(uv.x, uv.y + stepY)).r;
                    yValue = (yAbove + yBelow) * 0.5;
                }
            }

            float2 cbcr = textureCbCr.sample(s, uv).rg;
            float cb = cbcr.r - 0.5;
            float cr = cbcr.g - 0.5;

            float yNorm = (yValue - (16.0 / 255.0)) * (255.0 / (235.0 - 16.0));
            yNorm = clamp(yNorm, 0.0, 1.0);

            float r = yNorm + 1.5748 * cr;
            float g = yNorm - 0.1873 * cb - 0.4681 * cr;
            float b = yNorm + 1.8556 * cb;

            return float4(clamp(float3(r, g, b), 0.0, 1.0), 1.0);
        }
        """

        do {
            let library = try metalDevice.makeLibrary(source: shaderSource, options: nil)
            guard let vertexFunc = library.makeFunction(name: "deinterlaceVertexShader"),
                  let fragmentFunc = library.makeFunction(name: "deinterlaceFragmentShader") else {
                return
            }

            let pipelineDesc = MTLRenderPipelineDescriptor()
            pipelineDesc.vertexFunction = vertexFunc
            pipelineDesc.fragmentFunction = fragmentFunc
            pipelineDesc.colorAttachments[0].pixelFormat = .bgra8Unorm

            pipelineState = try metalDevice.makeRenderPipelineState(descriptor: pipelineDesc)
        } catch {
            print("Failed to compile runtime Metal shaders: \(error)")
        }
    }

    private func setupDisplayLink() {
        let link = CADisplayLink(target: self, selector: #selector(displayLinkFired(_:)))
        if #available(iOS 15.0, *) {
            // Target presentation cadence: 50 fps for European 50 Hz broadcast
            link.preferredFrameRateRange = CAFrameRateRange(minimum: 50, maximum: 50, preferred: 50)
        }
        link.add(to: .main, forMode: .common)
        self.displayLink = link
        self.lastDisplayTimestamp = 0
        self.lastTelemetryUpdate = CACurrentMediaTime()
    }

    nonisolated public func enqueueFrame(_ frame: DecodedVideoFrame) {
        let dropped = frameQueue.push(frame)
        if dropped {
            DispatchQueue.main.async { [weak self] in
                self?.telemetry?.droppedFrames += 1
            }
        }
    }

    @objc private func displayLinkFired(_ link: CADisplayLink) {
        let now = link.timestamp
        callbackCount += 1

        if lastDisplayTimestamp > 0 {
            let delta = now - lastDisplayTimestamp
            let expectedDelta: Double = 1.0 / 50.0
            let jitter = abs(delta - expectedDelta) * 1000.0 // ms
            jitterAccumulator += jitter
            jitterSampleCount += 1
            fieldCadenceAccumulator += (delta * 1000.0)
        }
        lastDisplayTimestamp = now

        // Advance to next frame or next field
        var isRepeatedField = false
        var renderedFieldIsTop = false

        if currentFrame == nil || fieldCycleCount >= 2 {
            if let nextFrame = frameQueue.pop() {
                currentFrame = nextFrame
                fieldCycleCount = 0
                currentFieldIsTop = nextFrame.isTopFieldFirst
                renderedFieldIsTop = nextFrame.isTopFieldFirst
            } else if currentFrame != nil {
                // Queue underrun: repeating previous field to avoid black frame
                isRepeatedField = true
                renderedFieldIsTop = currentFieldIsTop
            }
        } else {
            // Normal 2nd field of the current decoded frame (50 fields/s)
            currentFieldIsTop.toggle()
            renderedFieldIsTop = currentFieldIsTop
        }

        let frameToRender = currentFrame
        fieldCycleCount += 1

        if let frame = frameToRender {
            let isFirst = !hasReportedFirstFrame
            renderPixelBuffer(
                frame.pixelBuffer,
                isTopField: renderedFieldIsTop,
                isTopFieldFirst: frame.isTopFieldFirst,
                isFirstFrame: isFirst
            )

            presentationCount += 1
            if isRepeatedField {
                repeatedFieldPresentationCount += 1
                cumulativeRepeatedFieldCount += 1
            } else {
                if renderedFieldIsTop {
                    topFieldPresentationCount += 1
                } else {
                    bottomFieldPresentationCount += 1
                }
            }

            if isFirst {
                hasReportedFirstFrame = true
                onFirstFrameRendered?()
            }
        }

        // Periodic telemetry calculation (every 1 second)
        if now - lastTelemetryUpdate >= 1.0 {
            let elapsed = now - lastTelemetryUpdate
            let actualCallbacks = Double(callbackCount) / elapsed
            let actualPresentations = Double(presentationCount) / elapsed
            let actualTopFields = Double(topFieldPresentationCount) / elapsed
            let actualBottomFields = Double(bottomFieldPresentationCount) / elapsed
            let actualRepeatedFields = Double(repeatedFieldPresentationCount) / elapsed
            let avgJitter = jitterSampleCount > 0 ? (jitterAccumulator / Double(jitterSampleCount)) : 0.0
            let avgCadence = jitterSampleCount > 0 ? (fieldCadenceAccumulator / Double(jitterSampleCount)) : 0.0

            DispatchQueue.main.async { [weak self] in
                guard let self = self, let telemetry = self.telemetry else { return }
                telemetry.displayCallbacksPerSec = actualCallbacks
                telemetry.presentedFramesPerSec = actualPresentations
                telemetry.generatedFieldsPerSec = actualPresentations
                telemetry.topFieldsPerSec = actualTopFields
                telemetry.bottomFieldsPerSec = actualBottomFields
                telemetry.repeatedFieldsPerSec = actualRepeatedFields
                telemetry.repeatedFieldCount = self.cumulativeRepeatedFieldCount
                telemetry.fieldsSubmittedPerSec = actualPresentations
                telemetry.presentationJitterMs = avgJitter
                telemetry.fieldCadenceMs = avgCadence
                telemetry.sourceFieldRate = actualTopFields + actualBottomFields
                telemetry.sourceFrameRate = (actualTopFields + actualBottomFields) / 2.0

                let perfLog = "[1080i50-PERF] Metal: \(String(format: "%.1f", actualPresentations)) fps | Top/Bot: \(String(format: "%.1f", actualTopFields))/\(String(format: "%.1f", actualBottomFields)) | Cadence: \(String(format: "%.2f", avgCadence))ms | Jitter: \(String(format: "%.2f", avgJitter))ms | Rep: \(self.cumulativeRepeatedFieldCount)"
                print(perfLog)
                logger.notice("\(perfLog, privacy: .public)")
            }

            callbackCount = 0
            presentationCount = 0
            topFieldPresentationCount = 0
            bottomFieldPresentationCount = 0
            repeatedFieldPresentationCount = 0
            jitterAccumulator = 0
            jitterSampleCount = 0
            fieldCadenceAccumulator = 0
            lastTelemetryUpdate = now
        }
    }

    private func renderPixelBuffer(_ pixelBuffer: CVPixelBuffer, isTopField: Bool, isTopFieldFirst: Bool, isFirstFrame: Bool) {
        guard let cache = textureCache,
              let pipeline = pipelineState,
              let drawable = metalLayer.nextDrawable(),
              let commandBuffer = commandQueue.makeCommandBuffer() else {
            return
        }

        let width = CVPixelBufferGetWidth(pixelBuffer)
        let height = CVPixelBufferGetHeight(pixelBuffer)

        // 1. Plane 0 (Y Luminance)
        var textureY: CVMetalTexture?
        CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault,
            cache,
            pixelBuffer,
            nil,
            .r8Unorm,
            width,
            height,
            0,
            &textureY
        )

        // 2. Plane 1 (CbCr Chrominance)
        var textureCbCr: CVMetalTexture?
        CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault,
            cache,
            pixelBuffer,
            nil,
            .rg8Unorm,
            width / 2,
            height / 2,
            1,
            &textureCbCr
        )

        guard let yTex = CVMetalTextureGetTexture(textureY!),
              let cbcrTex = CVMetalTextureGetTexture(textureCbCr!) else {
            return
        }

        let renderPassDescriptor = MTLRenderPassDescriptor()
        renderPassDescriptor.colorAttachments[0].texture = drawable.texture
        renderPassDescriptor.colorAttachments[0].loadAction = .dontCare
        renderPassDescriptor.colorAttachments[0].storeAction = .store

        guard let encoder = commandBuffer.makeRenderCommandEncoder(descriptor: renderPassDescriptor) else { return }

        encoder.setRenderPipelineState(pipeline)
        encoder.setFragmentTexture(yTex, index: 0)
        encoder.setFragmentTexture(cbcrTex, index: 1)

        var params = DeinterlaceShaderParams(
            isTopField: isTopField ? 1 : 0,
            isInterlaced: 1, // 1080i
            isTopFieldFirst: isTopFieldFirst ? 1 : 0,
            frameHeight: Float(height)
        )
        encoder.setFragmentBytes(&params, length: MemoryLayout<DeinterlaceShaderParams>.size, index: 0)

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
}

private final class ThreadSafeFrameQueue: @unchecked Sendable {
    private var queue: [DecodedVideoFrame] = []
    private let lock = NSLock()

    func push(_ frame: DecodedVideoFrame, maxCapacity: Int = 10) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        var dropped = false
        if queue.count >= maxCapacity {
            queue.removeFirst()
            dropped = true
        }
        queue.append(frame)
        return dropped
    }

    func pop() -> DecodedVideoFrame? {
        lock.lock()
        defer { lock.unlock() }
        guard !queue.isEmpty else { return nil }
        return queue.removeFirst()
    }

    func clear() {
        lock.lock()
        defer { lock.unlock() }
        queue.removeAll(keepingCapacity: true)
    }
}
