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
        let inspected = VideoGeometry.inspectSAR(from: pb576_16_9)
        #expect(inspected.isSignaled == true)
        #expect(inspected.numerator == 64)
        #expect(inspected.denominator == 45)
        let dar576_16_9 = VideoGeometry.effectiveDAR(
            width: CVPixelBufferGetWidth(pb576_16_9),
            height: CVPixelBufferGetHeight(pb576_16_9),
            sarNumerator: inspected.numerator,
            sarDenominator: inspected.denominator
        )
        #expect(abs(dar576_16_9 - (16.0 / 9.0)) < 0.0001)

        // 4. Unsignaled buffer fallback (no SAR attachment)
        let pbUnsignaled = createTestPixelBuffer(width: 720, height: 576)
        let unsignaled = VideoGeometry.inspectSAR(from: pbUnsignaled)
        #expect(unsignaled.isSignaled == false)
        #expect(unsignaled.numerator == 1)
        #expect(unsignaled.denominator == 1)
    }

    @Test("Source DAR vs Output DAR Separation on 720x576 with 4:3 Override")
    func testSourceDARvsOutputDARSeparation() {
        // Given a 720x576 SD buffer with signaled SAR 64:45 (native 16:9 DVB)
        let pb = createTestPixelBuffer(width: 720, height: 576, sarNum: 64, sarDen: 45)
        let width = CVPixelBufferGetWidth(pb)
        let height = CVPixelBufferGetHeight(pb)
        let sar = VideoGeometry.inspectSAR(from: pb)
        #expect(sar.isSignaled == true)
        #expect(sar.numerator == 64)
        #expect(sar.denominator == 45)

        // When applying a manual 4:3 override
        let override = VideoAspectRatio.r4_3

        // Then sourceDAR is strictly the un-overridden native DAR (16:9)
        let sourceDAR = VideoGeometry.effectiveDAR(
            width: width,
            height: height,
            sarNumerator: sar.numerator,
            sarDenominator: sar.denominator,
            override: .auto
        )
        #expect(abs(sourceDAR - (16.0 / 9.0)) < 0.0001)
        #expect(VideoGeometry.describeDAR(sourceDAR) == "16:9")

        // And outputDAR is the overridden display aspect ratio (4:3)
        let outputDAR = VideoGeometry.effectiveDAR(
            width: width,
            height: height,
            sarNumerator: sar.numerator,
            sarDenominator: sar.denominator,
            override: override
        )
        #expect(abs(outputDAR - (4.0 / 3.0)) < 0.0001)
        #expect(VideoGeometry.describeDAR(outputDAR) == "4:3")
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

    @MainActor
    @Test("CMFormatDescription: FormatDescription recreates with correct PixelAspectRatio extension on DAR override")
    func testFormatDescriptionExtensionsOnDAROverride() {
        let presenter = SystemVideoPresenter()
        let pb = createTestPixelBuffer(width: 1920, height: 1080, sarNum: 1, sarDen: 1)

        // 1. Initial 1920x1080 Auto (1:1 SAR)
        guard let format1 = presenter.formatDescription(for: pb) else {
            #expect(Bool(false), "formatDescription must not be nil")
            return
        }
        let ext1 = CMFormatDescriptionGetExtensions(format1) as? [CFString: Any]
        if let parDict1 = ext1?[kCMFormatDescriptionExtension_PixelAspectRatio] as? [CFString: Any] {
            let h = (parDict1[kCVImageBufferPixelAspectRatioHorizontalSpacingKey] as? NSNumber)?.intValue ?? 1
            let v = (parDict1[kCVImageBufferPixelAspectRatioVerticalSpacingKey] as? NSNumber)?.intValue ?? 1
            #expect(h == 1 && v == 1)
        }

        // 2. Override to 4:3 (PAR becomes 3:4) on same 1920x1080 dimensions
        VideoGeometry.applyPixelAspectRatio(to: pb, aspectRatioOverride: .r4_3)
        guard let format2 = presenter.formatDescription(for: pb) else {
            #expect(Bool(false), "formatDescription must not be nil")
            return
        }
        // Format description MUST be regenerated (not equal to previous cached instance)
        #expect(format1 !== format2)

        let ext2 = CMFormatDescriptionGetExtensions(format2) as? [CFString: Any]
        let parDict2 = ext2?[kCMFormatDescriptionExtension_PixelAspectRatio] as? [CFString: Any]
        #expect(parDict2 != nil)
        let h2 = (parDict2?[kCVImageBufferPixelAspectRatioHorizontalSpacingKey] as? NSNumber)?.intValue
        let v2 = (parDict2?[kCVImageBufferPixelAspectRatioVerticalSpacingKey] as? NSNumber)?.intValue
        #expect(h2 == 3)
        #expect(v2 == 4)

        // 3. Override to 16:9 on 1920x1080 (PAR becomes 1:1)
        VideoGeometry.applyPixelAspectRatio(to: pb, aspectRatioOverride: .r16_9)
        guard let format3 = presenter.formatDescription(for: pb) else {
            #expect(Bool(false), "formatDescription must not be nil")
            return
        }
        #expect(format2 !== format3)
        let ext3 = CMFormatDescriptionGetExtensions(format3) as? [CFString: Any]
        let parDict3 = ext3?[kCMFormatDescriptionExtension_PixelAspectRatio] as? [CFString: Any]
        let h3 = (parDict3?[kCVImageBufferPixelAspectRatioHorizontalSpacingKey] as? NSNumber)?.intValue ?? 1
        let v3 = (parDict3?[kCVImageBufferPixelAspectRatioVerticalSpacingKey] as? NSNumber)?.intValue ?? 1
        #expect(h3 == 1 && v3 == 1)
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
    @Test("MetalVideoView: Switching Presets during live playback preserves generation, increments enqueued count, and causes 0 drops")
    func testLivePlaybackGeometrySwitchingWithMetrics() async throws {
        let view = MetalVideoView(frame: CGRect(x: 0, y: 0, width: 852, height: 393))
        let presenter = SystemVideoPresenter()
        view.systemPresenter = presenter
        view.presentationPath = .systemLayer

        let telemetry = StreamTelemetry()
        view.telemetry = telemetry

        let generation = 1
        view.resetForChannelZap(generation: generation)
        presenter.flush(generation: generation)

        let pb = createTestPixelBuffer(width: 1920, height: 1080, sarNum: 1, sarDen: 1)

        let initialEnqueued = presenter.enqueuedCount
        let initialDrops = telemetry.snapshot().droppedFrames

        // Sequence of mode transitions: Standard -> Fill -> 16:9 -> 4:3 -> Standard
        let modes: [(VideoScalingMode, VideoAspectRatio)] = [
            (.fit, .auto),
            (.fill, .auto),
            (.fit, .r16_9),
            (.fit, .r4_3),
            (.fit, .auto)
        ]

        var frameIndex = 0
        for (scaling, aspect) in modes {
            view.scalingMode = scaling
            view.aspectRatioOverride = aspect

            // Assert immediate presentation layer gravity update
            #expect(presenter.displayLayer.videoGravity == (scaling == .fill ? .resizeAspectFill : .resizeAspect))

            frameIndex += 1
            let frame = DecodedVideoFrame(
                pixelBuffer: pb,
                pts: CMTime(seconds: Double(frameIndex) * 0.04, preferredTimescale: 90_000),
                structure: .wovenTopFieldFirst,
                generation: generation
            )
            view.enqueueFrame(frame)
        }

        // Wait for async presentation pump to process fields
        try await Task.sleep(nanoseconds: 100_000_000)

        // Verify presenter received all fields and generation was preserved
        #expect(presenter.enqueuedCount > initialEnqueued)
        #expect(view.currentGeneration == generation)

        // Verify telemetry recorded ZERO dropped frames
        let finalTelemetry = telemetry.snapshot()
        #expect(finalTelemetry.droppedFrames == initialDrops)
    }

    @Test("VideoViewPreset: Short UI labels are bounded and concise")
    func testVideoViewPresetShortLabels() {
        for preset in VideoViewPreset.allCases {
            let label = preset.shortLabel
            #expect(!label.isEmpty)
            #expect(label.count <= 8, "Short label '\(label)' exceeds maximum compact length")
        }
        #expect(VideoViewPreset.standard.shortLabel == "Standard")
        #expect(VideoViewPreset.fillScreen.shortLabel == "Füllen")
        #expect(VideoViewPreset.r16_9.shortLabel == "16:9")
        #expect(VideoViewPreset.r4_3.shortLabel == "4:3")
    }

    @Test("Player Header: Width Budgeting across standard iPhone viewports")
    func testHeaderWidthBudgeting() {
        struct ViewportBudget {
            let totalWidth: CGFloat
            let isLandscape: Bool
            let sideInset: CGFloat
            let dismissButtonWidth: CGFloat = 44
            let logoWidth: CGFloat = 32
            let aspectButtonEstimatedWidth: CGFloat = 72
            let menuButtonWidth: CGFloat = 44
            let pipButtonWidth: CGFloat = 44
            let airplayButtonWidth: CGFloat = 44
            let spacing: CGFloat = 8

            var channelInfoAvailableWidth: CGFloat {
                let fixedElementsWidth: CGFloat
                if isLandscape {
                    fixedElementsWidth = dismissButtonWidth + spacing + logoWidth + spacing +
                                         aspectButtonEstimatedWidth + spacing + pipButtonWidth + spacing +
                                         airplayButtonWidth + spacing + menuButtonWidth
                } else {
                    fixedElementsWidth = dismissButtonWidth + spacing + logoWidth + spacing +
                                         aspectButtonEstimatedWidth + spacing + menuButtonWidth
                }
                let insets = sideInset * 2
                return totalWidth - insets - fixedElementsWidth
            }
        }

        // Test 375 pt (iPhone SE / 13 mini)
        let seBudget = ViewportBudget(totalWidth: 375, isLandscape: false, sideInset: 16)
        #expect(seBudget.channelInfoAvailableWidth >= 125, "iPhone SE must have at least 125pt for channel title")

        // Test 390 pt (iPhone 12/13/14)
        let ip14Budget = ViewportBudget(totalWidth: 390, isLandscape: false, sideInset: 16)
        #expect(ip14Budget.channelInfoAvailableWidth >= 140)

        // Test 393 pt (iPhone 15/16 Pro)
        let proBudget = ViewportBudget(totalWidth: 393, isLandscape: false, sideInset: 16)
        #expect(proBudget.channelInfoAvailableWidth >= 143)

        // Test 430 pt (iPhone 15/16 Pro Max)
        let maxBudget = ViewportBudget(totalWidth: 430, isLandscape: false, sideInset: 16)
        #expect(maxBudget.channelInfoAvailableWidth >= 180)

        // Test 852 pt (iPhone 15/16 Pro Landscape)
        let landscapeBudget = ViewportBudget(totalWidth: 852, isLandscape: true, sideInset: 24)
        #expect(landscapeBudget.channelInfoAvailableWidth >= 450, "Landscape must have abundant space for full metadata")
    }

    @Test("CVPixelBuffer: Colorimetry, Range, and Field Detail Extraction (Positive & Negative)")
    func testColorimetryExtraction() {
        // 1. Positive Test: Explicit BT.709, Limited Range, TFF
        var pixelBuffer: CVPixelBuffer?
        let status = CVPixelBufferCreate(
            kCFAllocatorDefault,
            1920,
            1080,
            kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
            nil,
            &pixelBuffer
        )
        guard status == kCVReturnSuccess, let pb = pixelBuffer else {
            Issue.record("Failed to create CVPixelBuffer")
            return
        }

        CVBufferSetAttachment(pb, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_709_2, .shouldPropagate)
        CVBufferSetAttachment(pb, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_ITU_R_709_2, .shouldPropagate)
        CVBufferSetAttachment(pb, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_709_2, .shouldPropagate)
        CVBufferSetAttachment(pb, kCVImageBufferFieldDetailKey, kCVImageBufferFieldDetailTemporalTopFirst, .shouldPropagate)

        let info = VideoGeometry.inspectColorimetry(from: pb)
        #expect(info.primaries == "ITU-R BT.709")
        #expect(info.transferFunction == "SDR (BT.709)")
        #expect(info.matrix == "ITU-R BT.709")
        #expect(info.range == "Video Range (16–235)")
        #expect(info.fieldDetail == "Top Field First (TFF)")
        #expect(!info.isHDR)

        // 2. Negative Test: Empty PixelBuffer (No attachments)
        var emptyBuffer: CVPixelBuffer?
        let status2 = CVPixelBufferCreate(
            kCFAllocatorDefault,
            1280,
            720,
            kCVPixelFormatType_420YpCbCr8BiPlanarFullRange,
            nil,
            &emptyBuffer
        )
        guard status2 == kCVReturnSuccess, let emptyPb = emptyBuffer else {
            Issue.record("Failed to create empty CVPixelBuffer")
            return
        }

        let emptyInfo = VideoGeometry.inspectColorimetry(from: emptyPb)
        #expect(emptyInfo.primaries == "—", "Missing primaries must report em-dash")
        #expect(emptyInfo.transferFunction == "—", "Missing transfer function must report em-dash, not fake SDR")
        #expect(emptyInfo.matrix == "—", "Missing matrix must report em-dash")
        #expect(emptyInfo.range == "Full Range (0–255)")
        #expect(emptyInfo.fieldDetail == "—", "Missing field detail must report em-dash, not fake progressive")
        #expect(!emptyInfo.isHDR)

        // 3. Negative Test: Unrecognized PixelFormatType (e.g. 32BGRA)
        var bgraBuffer: CVPixelBuffer?
        let status3 = CVPixelBufferCreate(
            kCFAllocatorDefault,
            640,
            480,
            kCVPixelFormatType_32BGRA,
            nil,
            &bgraBuffer
        )
        guard status3 == kCVReturnSuccess, let bgraPb = bgraBuffer else {
            Issue.record("Failed to create BGRA CVPixelBuffer")
            return
        }
        let bgraInfo = VideoGeometry.inspectColorimetry(from: bgraPb)
        #expect(bgraInfo.range == "—", "Unrecognized pixel format must report em-dash")

        // 4. HDR ST 2084 (PQ) Test: Must report 'PQ (ST 2084)' without unfounded HDR10 naming
        CVBufferSetAttachment(pb, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_SMPTE_ST_2084_PQ, .shouldPropagate)
        let pqInfo = VideoGeometry.inspectColorimetry(from: pb)
        #expect(pqInfo.transferFunction == "PQ (ST 2084)")
        #expect(pqInfo.isHDR == true)

        // 5. HLG Test: Must report 'HLG (BT.2100)'
        CVBufferSetAttachment(pb, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_ITU_R_2100_HLG, .shouldPropagate)
        let hlgInfo = VideoGeometry.inspectColorimetry(from: pb)
        #expect(hlgInfo.transferFunction == "HLG (BT.2100)")
        #expect(hlgInfo.isHDR == true)

        // 6. SD Primaries Distinction: EBU Tech 3213 & SMPTE-C
        CVBufferSetAttachment(pb, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_EBU_3213, .shouldPropagate)
        let ebuInfo = VideoGeometry.inspectColorimetry(from: pb)
        #expect(ebuInfo.primaries == "EBU Tech 3213")

        CVBufferSetAttachment(pb, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_SMPTE_C, .shouldPropagate)
        let smpteInfo = VideoGeometry.inspectColorimetry(from: pb)
        #expect(smpteInfo.primaries == "SMPTE-C")
    }

    @Test("AFD: Active Format Description code definitions and descriptions")
    func testActiveFormatDescriptionCodes() {
        // Unsignaled / None
        let afdNone = VideoGeometry.ActiveFormatDescription(code: nil)
        #expect(!afdNone.isSignaled)
        #expect(afdNone.description == "—")

        let afdZero = VideoGeometry.ActiveFormatDescription(code: 0)
        #expect(!afdZero.isSignaled)
        #expect(afdZero.description == "—")

        // 16:9 Letterbox in 4:3 frame (code 0b0010 = 2)
        let afd2 = VideoGeometry.ActiveFormatDescription(code: 0b0010)
        #expect(afd2.isSignaled)
        #expect(afd2.description == "16:9 (Letterbox im 4:3-Frame)")

        // 14:9 Letterbox in 4:3 frame (code 0b0011 = 3)
        let afd3 = VideoGeometry.ActiveFormatDescription(code: 0b0011)
        #expect(afd3.isSignaled)
        #expect(afd3.description == "14:9 (Letterbox im 4:3-Frame)")

        // Full Frame / Same AR (code 0b1000 = 8)
        let afd8 = VideoGeometry.ActiveFormatDescription(code: 0b1000)
        #expect(afd8.isSignaled)
        #expect(afd8.description == "Vollbild (Full Frame / Same AR)")

        // 4:3 Pillarbox in 16:9 frame (code 0b1001 = 9)
        let afd9 = VideoGeometry.ActiveFormatDescription(code: 0b1001)
        #expect(afd9.isSignaled)
        #expect(afd9.description == "4:3 (Pillarbox im 16:9-Frame)")

        // 16:9 Full Frame in 16:9 frame (code 0b1010 = 10)
        let afd10 = VideoGeometry.ActiveFormatDescription(code: 0b1010)
        #expect(afd10.isSignaled)
        #expect(afd10.description == "16:9 (Vollbild im 16:9-Frame)")
    }
}
