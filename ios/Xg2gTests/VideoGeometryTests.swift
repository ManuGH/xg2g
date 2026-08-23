// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreGraphics
import CoreMedia
import CoreVideo
import Testing
@testable import Xg2g

@Suite("Video Geometry & Aspect Ratio Tests")
struct VideoGeometryTests {

    private func createTestPixelBuffer(width: Int, height: Int, sarNum: Int? = nil, sarDen: Int? = nil) -> CVPixelBuffer {
        var pixelBuffer: CVPixelBuffer?
        let attributes: [CFString: Any] = [
            kCVPixelBufferMetalCompatibilityKey: true,
            kCVPixelBufferIOSurfacePropertiesKey: [:] as CFDictionary
        ]
        let status = CVPixelBufferCreate(
            kCFAllocatorDefault,
            width,
            height,
            kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
            attributes as CFDictionary,
            &pixelBuffer
        )
        precondition(status == kCVReturnSuccess && pixelBuffer != nil)
        let pb = pixelBuffer!

        if let sarNum = sarNum, let sarDen = sarDen {
            let sarDict: [CFString: Any] = [
                kCVImageBufferPixelAspectRatioHorizontalSpacingKey: sarNum,
                kCVImageBufferPixelAspectRatioVerticalSpacingKey: sarDen
            ]
            CVBufferSetAttachment(pb, kCVImageBufferPixelAspectRatioKey, sarDict as CFDictionary, .shouldPropagate)
        }

        return pb
    }

    @Test("CVPixelBuffer: SAR extraction from actual PixelBuffer attachments")
    func testPixelBufferSARExtraction() {
        // 1. 1920x1080 SAR 1:1
        let pb1080 = createTestPixelBuffer(width: 1920, height: 1080, sarNum: 1, sarDen: 1)
        let sar1080 = VideoGeometry.extractSAR(from: pb1080)
        #expect(sar1080.numerator == 1)
        #expect(sar1080.denominator == 1)
        let dar1080 = VideoGeometry.effectiveDAR(
            width: CVPixelBufferGetWidth(pb1080),
            height: CVPixelBufferGetHeight(pb1080),
            sarNumerator: sar1080.numerator,
            sarDenominator: sar1080.denominator
        )
        #expect(abs(dar1080 - (16.0 / 9.0)) < 0.0001)

        // 2. 720x576 SD 4:3 with SAR 16:15
        let pb576_4_3 = createTestPixelBuffer(width: 720, height: 576, sarNum: 16, sarDen: 15)
        let sar576_4_3 = VideoGeometry.extractSAR(from: pb576_4_3)
        #expect(sar576_4_3.numerator == 16)
        #expect(sar576_4_3.denominator == 15)
        let dar576_4_3 = VideoGeometry.effectiveDAR(
            width: CVPixelBufferGetWidth(pb576_4_3),
            height: CVPixelBufferGetHeight(pb576_4_3),
            sarNumerator: sar576_4_3.numerator,
            sarDenominator: sar576_4_3.denominator
        )
        #expect(abs(dar576_4_3 - (4.0 / 3.0)) < 0.0001)

        // 3. 720x576 SD Anamorphic 16:9 with SAR 64:45
        let pb576_16_9 = createTestPixelBuffer(width: 720, height: 576, sarNum: 64, sarDen: 45)
        let sar576_16_9 = VideoGeometry.extractSAR(from: pb576_16_9)
        #expect(sar576_16_9.numerator == 64)
        #expect(sar576_16_9.denominator == 45)
        let dar576_16_9 = VideoGeometry.effectiveDAR(
            width: CVPixelBufferGetWidth(pb576_16_9),
            height: CVPixelBufferGetHeight(pb576_16_9),
            sarNumerator: sar576_16_9.numerator,
            sarDenominator: sar576_16_9.denominator
        )
        #expect(abs(dar576_16_9 - (16.0 / 9.0)) < 0.0001)
    }

    @Test("CVPixelBuffer: Application of DAR Override attaches correct SAR for AVFoundation")
    func testPixelBufferDAROverrideAttachment() {
        // 1920x1080 native frame overridden to 4:3
        let pb = createTestPixelBuffer(width: 1920, height: 1080)
        VideoGeometry.applyPixelAspectRatio(to: pb, aspectRatioOverride: .r4_3)
        let sar = VideoGeometry.extractSAR(from: pb)
        #expect(sar.numerator == 3)
        #expect(sar.denominator == 4)

        let effective = VideoGeometry.effectiveDAR(
            width: 1920,
            height: 1080,
            sarNumerator: sar.numerator,
            sarDenominator: sar.denominator
        )
        #expect(abs(effective - (4.0 / 3.0)) < 0.0001)

        // 720x576 frame overridden to 16:9
        let pbSD = createTestPixelBuffer(width: 720, height: 576)
        VideoGeometry.applyPixelAspectRatio(to: pbSD, aspectRatioOverride: .r16_9)
        let sarSD = VideoGeometry.extractSAR(from: pbSD)
        #expect(sarSD.numerator == 64)
        #expect(sarSD.denominator == 45)

        let effectiveSD = VideoGeometry.effectiveDAR(
            width: 720,
            height: 576,
            sarNumerator: sarSD.numerator,
            sarDenominator: sarSD.denominator
        )
        #expect(abs(effectiveSD - (16.0 / 9.0)) < 0.0001)
    }

    @Test("Rational Fraction Approximation")
    func testRationalFraction() {
        let f1 = VideoGeometry.rationalFraction(from: 16.0 / 9.0)
        #expect(f1.numerator == 16 && f1.denominator == 9)

        let f2 = VideoGeometry.rationalFraction(from: 4.0 / 3.0)
        #expect(f2.numerator == 4 && f2.denominator == 3)

        let f3 = VideoGeometry.rationalFraction(from: 0.75)
        #expect(f3.numerator == 3 && f3.denominator == 4)

        let f4 = VideoGeometry.rationalFraction(from: 64.0 / 45.0)
        #expect(f4.numerator == 64 && f4.denominator == 45)
    }

    @Test("Metal Geometry: Aspect Fit on Wide Viewport (iPhone Landscape)")
    func testMetalFitLandscape() {
        let geom = VideoGeometry.calculateMetalGeometry(
            viewportSize: CGSize(width: 852, height: 393),
            sourceWidth: 1920,
            sourceHeight: 1080,
            aspectRatioOverride: .auto,
            scalingMode: .fit
        )

        #expect(geom.positionScaleX < 1.0)
        #expect(geom.positionScaleY == 1.0)
        #expect(geom.uvScaleX == 1.0)
        #expect(geom.uvScaleY == 1.0)
        #expect(geom.uvOffsetX == 0.0)
        #expect(geom.uvOffsetY == 0.0)

        let expectedWidthScale = Float((16.0 / 9.0) / (852.0 / 393.0))
        #expect(abs(geom.positionScaleX - expectedWidthScale) < 0.001)
    }

    @Test("Metal Geometry: Aspect Fill on Wide Viewport (iPhone Landscape Center Crop)")
    func testMetalFillLandscape() {
        let geom = VideoGeometry.calculateMetalGeometry(
            viewportSize: CGSize(width: 852, height: 393),
            sourceWidth: 1920,
            sourceHeight: 1080,
            aspectRatioOverride: .auto,
            scalingMode: .fill
        )

        #expect(geom.positionScaleX == 1.0)
        #expect(geom.positionScaleY == 1.0)
        #expect(geom.uvScaleX == 1.0)
        #expect(geom.uvScaleY < 1.0)
        #expect(geom.uvOffsetX == 0.0)
        #expect(geom.uvOffsetY > 0.0)
        #expect(abs(geom.uvOffsetY - ((1.0 - geom.uvScaleY) * 0.5)) < 0.0001)
    }

    @Test("Metal Geometry: Aspect Fit on Tall Viewport (iPhone Portrait)")
    func testMetalFitPortrait() {
        let geom = VideoGeometry.calculateMetalGeometry(
            viewportSize: CGSize(width: 393, height: 852),
            sourceWidth: 1920,
            sourceHeight: 1080,
            aspectRatioOverride: .auto,
            scalingMode: .fit
        )

        #expect(geom.positionScaleX == 1.0)
        #expect(geom.positionScaleY < 1.0)
        #expect(geom.uvScaleX == 1.0)
        #expect(geom.uvScaleY == 1.0)

        let expectedHeightScale = Float((393.0 / 852.0) / (16.0 / 9.0))
        #expect(abs(geom.positionScaleY - expectedHeightScale) < 0.001)
    }

    @Test("Metal Geometry: Aspect Fill on Tall Viewport (iPhone Portrait Center Crop)")
    func testMetalFillPortrait() {
        let geom = VideoGeometry.calculateMetalGeometry(
            viewportSize: CGSize(width: 393, height: 852),
            sourceWidth: 1920,
            sourceHeight: 1080,
            aspectRatioOverride: .auto,
            scalingMode: .fill
        )

        #expect(geom.positionScaleX == 1.0)
        #expect(geom.positionScaleY == 1.0)
        #expect(geom.uvScaleX < 1.0)
        #expect(geom.uvScaleY == 1.0)
        #expect(geom.uvOffsetX > 0.0)
        #expect(geom.uvOffsetY == 0.0)
        #expect(abs(geom.uvOffsetX - ((1.0 - geom.uvScaleX) * 0.5)) < 0.0001)
    }

    @Test("Preset Cycles: Standard 2-Mode Cycle vs Advanced VLC Cycle")
    func testPresetCycles() {
        // 1. Default clean 2-mode cycle
        var standard = VideoViewPreset.standard
        standard = standard.next(includeAdvanced: false)
        #expect(standard == .fillScreen)
        standard = standard.next(includeAdvanced: false)
        #expect(standard == .standard)

        // 2. Advanced cycle
        let expectedAdvanced: [VideoViewPreset] = [
            .standard,
            .fillScreen,
            .r16_9,
            .r4_3,
            .r5_4,
            .r16_10,
            .r2_21,
            .r2_35,
            .r2_39
        ]

        var adv: VideoViewPreset = .standard
        for expected in expectedAdvanced {
            #expect(adv == expected)
            adv = adv.next(includeAdvanced: true)
        }
        #expect(adv == .standard)

        #expect(VideoViewPreset.standard.scalingMode == .fit)
        #expect(VideoViewPreset.fillScreen.scalingMode == .fill)
        #expect(VideoViewPreset.r16_9.scalingMode == .fit)

        #expect(VideoViewPreset.standard.aspectRatio == .auto)
        #expect(VideoViewPreset.fillScreen.aspectRatio == .auto)
        #expect(VideoViewPreset.r4_3.aspectRatio == .r4_3)
        #expect(VideoViewPreset.r16_9.aspectRatio == .r16_9)
        #expect(VideoViewPreset.r2_35.aspectRatio == .r2_35)
    }

    @MainActor
    @Test("Presentation Layer: SystemPresenter respects scalingMode without restarts")
    func testSystemPresenterScalingMode() {
        let presenter = SystemVideoPresenter()
        #expect(presenter.displayLayer.videoGravity == .resizeAspect)

        presenter.scalingMode = .fill
        #expect(presenter.displayLayer.videoGravity == .resizeAspectFill)

        presenter.scalingMode = .fit
        #expect(presenter.displayLayer.videoGravity == .resizeAspect)
    }

    @MainActor
    @Test("MetalVideoView: Switching Presets during live playback causes 0 drops and no resets")
    func testLivePlaybackGeometrySwitching() {
        let view = MetalVideoView(frame: CGRect(x: 0, y: 0, width: 852, height: 393))
        let presenter = SystemVideoPresenter()
        view.systemPresenter = presenter

        let pb = createTestPixelBuffer(width: 1920, height: 1080, sarNum: 1, sarDen: 1)
        let generation = 1

        // Switch modes repeatedly: Standard -> Fill -> 16:9 -> 4:3 -> Standard
        let modes: [(VideoScalingMode, VideoAspectRatio)] = [
            (.fit, .auto),
            (.fill, .auto),
            (.fit, .r16_9),
            (.fit, .r4_3),
            (.fit, .auto)
        ]

        for (scaling, aspect) in modes {
            view.scalingMode = scaling
            view.aspectRatioOverride = aspect

            // Verify presenter gravity updates instantaneously
            #expect(presenter.displayLayer.videoGravity == (scaling == .fill ? .resizeAspectFill : .resizeAspect))

            // Verify enqueueFrame / present continues smoothly
            let frame = DecodedVideoFrame(
                pixelBuffer: pb,
                pts: CMTime(seconds: 100.0, preferredTimescale: 90_000),
                structure: .wovenTopFieldFirst,
                generation: generation
            )
            view.enqueueFrame(frame)
        }
    }
}
