// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import AVKit
import CoreMedia
import OSLog
import UIKit

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "system-video")

/// Presents already-deinterlaced fields through AVFoundation instead of drawing
/// them ourselves, so the stream can enter the system's video features.
///
/// `AVPlayerViewController` — which is where Apple's full-screen chrome, its
/// transport controls and its Picture-in-Picture button come from — is only
/// available to an `AVPlayer`. This pipeline has none: it decodes to
/// `CVPixelBuffer` and renders through a `CAMetalLayer`, and a Metal layer
/// cannot be hosted inside `AVPlayerViewController`.
///
/// For a custom renderer the supported route is `AVSampleBufferDisplayLayer`,
/// which is what `AVPictureInPictureController` accepts as a content source.
/// It also happens to be the architecturally correct home for this pipeline:
/// the layer is driven by an `AVSampleBufferRenderSynchronizer`, and the audio
/// renderer already runs on one. Handing video to the same synchronizer puts
/// both tracks on a single clock owned by AVFoundation, rather than on our own
/// display-link scheduler running beside it.
///
/// The cost is a render target per field. Drawing straight into the drawable —
/// which is what `MetalVideoView` does — needs no intermediate surface, and that
/// second pass was deliberately removed once before for thermal reasons. Here it
/// is unavoidable: AVFoundation presents sample buffers, so a field has to exist
/// as one.
@MainActor
public final class SystemVideoPresenter: NSObject {

    /// The layer that actually shows the picture. Add it to a view's hierarchy.
    public let displayLayer = AVSampleBufferDisplayLayer()

    private var pictureInPictureController: AVPictureInPictureController?

    /// Set by the pipeline so the presenter can report playback state to PiP.
    public weak var playbackDelegate: SystemVideoPlaybackDelegate?

    private var formatDescription: CMVideoFormatDescription?
    private var formatDimensions: CMVideoDimensions = CMVideoDimensions(width: 0, height: 0)

    /// Fields handed to the renderer since the last flush. Readable so the
    /// render path can be asserted on end to end rather than inferred from
    /// counters that several different failures share.
    public private(set) var enqueuedCount = 0
    private var droppedCount = 0
    private var lastDiagnosticLog: CFTimeInterval = 0

    /// Fields waiting for the renderer to accept them.
    ///
    /// Backpressure is a reason to wait, never a reason to discard: the first
    /// version returned early when `isReadyForMoreMediaData` was false, and once
    /// production moved from the display link to decode arrival the renderer was
    /// full almost all the time. Roughly 1.5 of 50 fields per second survived,
    /// which is what a stuttering picture looks like from the inside.
    private var pendingSamples: [CMSampleBuffer] = []
    private let _atomicPendingCount = OSAllocatedUnfairLock(initialState: 0)
    private var isRequestingData = false

    /// Set on the first field of a tune so the layer shows it at once.
    ///
    /// The clock cannot start at the picture — it has to stay at or behind the
    /// audio or the sound is discarded — so on this source the first field lands
    /// about 2.5 s ahead of where the timeline begins, and the screen stays black
    /// for exactly that long while everything is already decoded and waiting.
    /// Measured: pipeline ready at 6264 ms, picture at 9066 ms.
    ///
    /// `DisplayImmediately` is the attachment for precisely this: show this one
    /// without regard to its timestamp. Everything after it presents from the
    /// timeline as normal, so the picture appears frozen for an instant and then
    /// runs — which is what a television does when you change channel.
    private var needsImmediateDisplay = true

    /// Fires when the field marked `DisplayImmediately` has actually been handed
    /// to the renderer — which, for that one field, is the moment it goes up.
    ///
    /// The pipeline otherwise learns of the first picture from a boundary
    /// observer on the master clock, and that observer is wrong by exactly the
    /// wait this attachment removes: the clock still reaches the field's PTS
    /// seconds later, long after the screen has shown it.
    public var onFirstFieldPresentedImmediately: (() -> Void)?

    /// Set between marking the field and the renderer taking it. The queue is
    /// empty at a tune, so the marked field is the next one out of it.
    private var awaitingImmediateHandoff = false

    /// Separates "the renderer will not take more" from "nothing is asking it".
    ///
    /// Both look identical from the drop counter, and they have opposite fixes.
    /// `requestMediaDataWhenReady` delivers its block on the main queue, which is
    /// also where the per-field deinterlace pass runs — so a starved main thread
    /// and a full renderer are indistinguishable without counting the calls.
    /// Reports a state the presenter recognises as a fault in itself, rather
    /// than a number someone would have to interpret.
    public var onWarning: ((String) -> Void)?

    /// Each warning is raised once. A stuck renderer stays stuck, and repeating
    /// it every two seconds buries the line that mattered.
    private var raisedWarnings = Set<String>()

    private var pullInvocations = 0
    private var readyTrue = 0
    private var readyFalse = 0

    /// Hard ceiling on the queue, ~2.4 s at 50 fields/s.
    ///
    /// Sized against the video lead, which is not a free choice. The mux delivers
    /// video roughly a second ahead of audio, and the clock cannot start ahead of
    /// the audio without silencing it — so once audio has its cushion, video
    /// necessarily sits about 1.5 s in front of the clock. The display layer
    /// buffers only a fraction of that and refuses the rest, which means the
    /// surplus is *ours* to hold. It is early data, not late data.
    ///
    /// Holding less does not make the surplus go away, it makes it get dropped:
    /// Empirically calibrated queue limit (sweet spot): 150 samples (~3.0s headroom).
    /// - 60 samples: 38.6% drops (severe stream stall)
    /// - 100 samples: 6.3% drops (micro-stutters during network bursts)
    /// - 125/150 samples: 0 drops (100% smooth 50Hz playback)
    /// Capped at 150 samples to guarantee 0 drops while preventing runaway RAM peaks (caps memory under ~500MB).
    public static var maxPendingSamples: Int = 150

    /// Current number of unrendered CMSampleBuffers waiting in the presenter queue.
    nonisolated public var pendingSamplesCount: Int {
        _atomicPendingCount.withLock { $0 }
    }

    public override init() {
        super.init()
        displayLayer.videoGravity = .resizeAspect
    }

    /// Joins the clock the audio renderer already drives.
    ///
    /// Both tracks then advance on one timebase, which is the arrangement
    /// `AVSampleBufferRenderSynchronizer` exists for. Called again after a zap,
    /// because the pipeline replaces the synchronizer on reset.
    /// Which synchronizer currently drives this layer, so the previous one can
    /// be let go of.
    ///
    /// The pipeline builds a fresh synchronizer on every stop, and only the
    /// *audio* renderer was ever removed from the old one. The display layer's
    /// renderer stayed registered with every synchronizer it had ever been given,
    /// accumulating one per zap — each retired, each parked at rate 0, and each
    /// still holding a reference to the renderer that is supposed to be driven by
    /// the live one.
    private weak var attachedSynchronizer: AVSampleBufferRenderSynchronizer?

    public func attach(to synchronizer: AVSampleBufferRenderSynchronizer) {
        guard attachedSynchronizer !== synchronizer else { return }
        if let previous = attachedSynchronizer {
            previous.removeRenderer(displayLayer.sampleBufferRenderer, at: .invalid) { _ in }
        }
        synchronizer.addRenderer(displayLayer.sampleBufferRenderer)
        attachedSynchronizer = synchronizer
        logger.notice("[SystemVideo] display layer attached to render synchronizer")
    }

    public func detach(from synchronizer: AVSampleBufferRenderSynchronizer) {
        synchronizer.removeRenderer(displayLayer.sampleBufferRenderer, at: .invalid) { _ in }
        if attachedSynchronizer === synchronizer {
            attachedSynchronizer = nil
        }
    }

    // MARK: - Picture in Picture

    /// Starts offering PiP for this layer.
    ///
    /// Requires the `audio` background mode, which the app already declares for
    /// the audio renderer. Without a supporting device this is a no-op rather
    /// than an error — `isPictureInPictureSupported` is false on some hardware
    /// and in the simulator.
    public func enablePictureInPicture() {
        guard AVPictureInPictureController.isPictureInPictureSupported() else {
            logger.notice("[SystemVideo] Picture in Picture is not supported on this device")
            return
        }
        guard pictureInPictureController == nil else { return }

        let source = AVPictureInPictureController.ContentSource(
            sampleBufferDisplayLayer: displayLayer,
            playbackDelegate: self
        )
        let controller = AVPictureInPictureController(contentSource: source)
        controller.delegate = self
        // A live broadcast has nothing to skip to.
        controller.requiresLinearPlayback = true
        // Leaving the app hands the picture to PiP rather than freezing it, which
        // is the behaviour the system player has and the one people expect from a
        // video app.
        controller.canStartPictureInPictureAutomaticallyFromInline = true
        pictureInPictureController = controller
        logger.notice("[SystemVideo] Picture in Picture controller ready")
    }

    public var isPictureInPicturePossible: Bool {
        pictureInPictureController?.isPictureInPicturePossible ?? false
    }

    /// Whether the picture currently lives in the PiP window.
    ///
    /// Asked of the controller rather than tracked, so it cannot drift out of
    /// step with what AVKit thinks. The renderer has to keep being fed while
    /// this is true — that is the whole point of PiP — which makes it the one
    /// case where work must continue after the app leaves the foreground.
    public var isPictureInPictureActive: Bool {
        pictureInPictureController?.isPictureInPictureActive ?? false
    }

    /// When a start was announced, so the window it opens can expire.
    private var pictureInPictureStartedAt: CFTimeInterval?

    /// How long `willStart` may vouch for a window that has not become active.
    private static let pictureInPictureStartWindow: Double = 3.0

    /// Whether PiP is running *or on its way there*.
    ///
    /// `isPictureInPictureActive` is the truth once the transition is over and
    /// useless before it. PiP starts as the app leaves the foreground, so
    /// anything that asks "is PiP up?" at that moment is told no — and if the
    /// answer is used to decide whether to keep feeding the layer, the feed stops
    /// exactly when AVKit needs it and PiP never comes up at all. Measured that
    /// way: a background guard written against the active flag alone stopped PiP
    /// from starting.
    public var isPictureInPictureEngaged: Bool {
        if isPictureInPictureActive { return true }
        guard let announced = pictureInPictureStartedAt else { return false }
        // Only the brief window while a start is in flight. A hard stop — a
        // device lock ending the session — does not reliably come back through
        // `didStop`, and a flag left standing would keep speaking for a window
        // that is already gone: the resume recovery would step aside for it and
        // the picture would stay frozen.
        return CACurrentMediaTime() - announced < Self.pictureInPictureStartWindow
    }

    public func startPictureInPicture() {
        guard let controller = pictureInPictureController, controller.isPictureInPicturePossible else {
            logger.notice("[SystemVideo] Picture in Picture requested but not currently possible")
            return
        }
        controller.startPictureInPicture()
    }

    public func stopPictureInPicture() {
        pictureInPictureController?.stopPictureInPicture()
    }

    public var currentGeneration: Int = 0

    /// Wraps one deinterlaced field and hands it to AVFoundation.
    ///
    /// The PTS is the field's own presentation time on the stream clock, so the
    /// layer schedules it against the synchronizer exactly as the audio renderer
    /// schedules its buffers — no separate presentation logic and no second
    /// clock to keep aligned.
    public func enqueue(pixelBuffer: CVPixelBuffer, pts: CMTime, duration: CMTime, generation: Int) {
        guard generation == currentGeneration else {
            // Stale field from a prior zap generation -> discard immediately
            return
        }

        guard let format = formatDescription(for: pixelBuffer) else { return }

        var timing = CMSampleTimingInfo(
            duration: duration,
            presentationTimeStamp: pts,
            decodeTimeStamp: .invalid
        )

        var sampleBuffer: CMSampleBuffer?
        let status = CMSampleBufferCreateReadyWithImageBuffer(
            allocator: kCFAllocatorDefault,
            imageBuffer: pixelBuffer,
            formatDescription: format,
            sampleTiming: &timing,
            sampleBufferOut: &sampleBuffer
        )

        guard status == noErr, let sample = sampleBuffer else {
            logger.error("[SystemVideo] CMSampleBufferCreateReadyWithImageBuffer failed: \(status)")
            return
        }

        if needsImmediateDisplay {
            needsImmediateDisplay = false
            awaitingImmediateHandoff = true
            if let attachments = CMSampleBufferGetSampleAttachmentsArray(sample, createIfNecessary: true),
               CFArrayGetCount(attachments) > 0 {
                let dict = unsafeBitCast(CFArrayGetValueAtIndex(attachments, 0), to: CFMutableDictionary.self)
                CFDictionarySetValue(
                    dict,
                    Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(),
                    Unmanaged.passUnretained(kCFBooleanTrue).toOpaque()
                )
            }
            let msg = "[SystemVideo] ⚡ First field of this tune (gen \(currentGeneration)) marked DisplayImmediately at PTS \(String(format: "%.3f", pts.seconds))s"
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)
        }

        if pendingSamples.count >= Self.maxPendingSamples {
            // Shed the newest, never the head.
            //
            // `removeFirst` discarded the field due soonest, tearing a hole
            // immediately in front of the playhead while keeping material that
            // was not needed for another two seconds. Refusing the newcomer
            // leaves the run about to be presented contiguous, so overflow costs
            // material at the far end of the buffer where there is still time to
            // recover.
            droppedCount += 1
        } else {
            pendingSamples.append(sample)
            let count = pendingSamples.count
            _atomicPendingCount.withLock { $0 = count }
        }
        drainPendingSamples()

        let now = CACurrentMediaTime()
        if now - lastDiagnosticLog >= 2.0 {
            lastDiagnosticLog = now
            let renderer = displayLayer.sampleBufferRenderer
            let statusText: String
            switch renderer.status {
            case .unknown: statusText = "unknown"
            case .rendering: statusText = "rendering"
            case .failed: statusText = "failed (\(renderer.error?.localizedDescription ?? "unknown"))"
            @unknown default: statusText = "other"
            }
            let diag = "[SystemVideo] 📺 Enqueued: \(enqueuedCount) | Dropped: \(droppedCount) | Queue: \(pendingSamples.count) | Pulls: \(pullInvocations) (ready \(readyTrue) / full \(readyFalse)) | Status: \(statusText) | PTS: \(String(format: "%.3f", pts.seconds))s | PiP possible: \(isPictureInPicturePossible)"
            // The shape of the failure, not its parts. A renderer that answers
            // every pull with "not ready" while the queue sits at its cap is not
            // a slow renderer, it is a stopped one — measured on a channel whose
            // clock never started: 121 pulls, ready 0, queue at 180, 1394 fields
            // shed against 13 delivered, and every other counter healthy.
            if pullInvocations > 0, readyTrue == 0, pendingSamples.count >= Self.maxPendingSamples {
                raise("Display layer has not accepted a frame in \(String(format: "%.0f", now - lastDiagnosticLog + 2.0))s — \(pullInvocations) pulls, none ready, queue full at \(pendingSamples.count). Playback is stopped, not slow.")
            }

            pullInvocations = 0
            readyTrue = 0
            readyFalse = 0
            print(diag)
            logger.notice("\(diag, privacy: .public)")
            TelemetryServer.shared.log(diag)
        }
    }

    /// Feeds the renderer until it is full or the queue is empty.
    ///
    /// Same discipline the audio renderer uses: `requestMediaDataWhenReady` is a
    /// backpressure signal, armed only while the renderer has actually reported
    /// itself full and torn down the moment the queue drains. Leaving it armed
    /// with nothing to give turns it into a spin — its block is re-invoked
    /// immediately whenever it returns while the renderer is still ready.
    private func drainPendingSamples() {
        let renderer = displayLayer.sampleBufferRenderer
        pullInvocations += 1

        while !pendingSamples.isEmpty {
            guard renderer.isReadyForMoreMediaData else {
                readyFalse += 1
                guard !isRequestingData else { return }
                isRequestingData = true
                // Delivered on `.main` because that is the queue asked for, but
                // the block itself is `@Sendable` and carries no isolation, so
                // the compiler cannot see that. Asserting it beats hopping
                // through `Task`: a hop would put the refill a runloop turn
                // behind the readiness that triggered it, on the path whose
                // whole job is to keep the renderer fed.
                renderer.requestMediaDataWhenReady(on: .main) { [weak self] in
                    MainActor.assumeIsolated {
                        self?.drainPendingSamples()
                    }
                }
                return
            }
            readyTrue += 1
            renderer.enqueue(pendingSamples.removeFirst())
            let count = pendingSamples.count
            _atomicPendingCount.withLock { $0 = count }
            enqueuedCount += 1
            if awaitingImmediateHandoff {
                awaitingImmediateHandoff = false
                onFirstFieldPresentedImmediately?()
            }
        }

        if isRequestingData {
            renderer.stopRequestingMediaData()
            isRequestingData = false
        }
    }

    /// Rebuilds the format description only when the dimensions actually change.
    ///
    /// Creating one per field would be wasteful at 50 fields per second, and a
    /// changing format description makes the layer re-negotiate its pipeline.
    private func formatDescription(for pixelBuffer: CVPixelBuffer) -> CMVideoFormatDescription? {
        let width = Int32(CVPixelBufferGetWidth(pixelBuffer))
        let height = Int32(CVPixelBufferGetHeight(pixelBuffer))

        if let existing = formatDescription,
           formatDimensions.width == width, formatDimensions.height == height {
            return existing
        }

        var created: CMVideoFormatDescription?
        let status = CMVideoFormatDescriptionCreateForImageBuffer(
            allocator: kCFAllocatorDefault,
            imageBuffer: pixelBuffer,
            formatDescriptionOut: &created
        )
        guard status == noErr, let format = created else {
            logger.error("[SystemVideo] CMVideoFormatDescriptionCreateForImageBuffer failed: \(status)")
            return nil
        }

        formatDescription = format
        formatDimensions = CMVideoDimensions(width: width, height: height)
        return format
    }

    /// Drops everything queued. Used on a channel zap, where the buffered fields
    /// belong to the previous stream's timeline.
    public func flush(generation: Int) {
        currentGeneration = generation
        pendingSamples.removeAll(keepingCapacity: true)
        _atomicPendingCount.withLock { $0 = 0 }
        if isRequestingData {
            displayLayer.sampleBufferRenderer.stopRequestingMediaData()
            isRequestingData = false
        }
        displayLayer.sampleBufferRenderer.flush()
        formatDescription = nil
        formatDimensions = CMVideoDimensions(width: 0, height: 0)
        // Re-armed per tune: a zap is exactly the moment the wait is felt.
        needsImmediateDisplay = true
        raisedWarnings.removeAll()
        awaitingImmediateHandoff = false
    }
}

/// What the presenter needs to know from the pipeline to answer PiP's questions.
@MainActor
public protocol SystemVideoPlaybackDelegate: AnyObject {
    /// True while the stream clock is running.
    var isSystemVideoPlaying: Bool { get }
    func systemVideoSetPlaying(_ playing: Bool)
}

// MARK: - AVPictureInPictureSampleBufferPlaybackDelegate

// AVKit calls both delegates on the main thread, which is where this class
// lives; `@preconcurrency` states that rather than scattering hops through
// methods that are already isolated. Matches how `PlayerScreen` adopts
// `AVPlayerViewControllerDelegate`.
extension SystemVideoPresenter: @preconcurrency AVPictureInPictureSampleBufferPlaybackDelegate {

    public func pictureInPictureController(_ controller: AVPictureInPictureController, setPlaying playing: Bool) {
        playbackDelegate?.systemVideoSetPlaying(playing)
    }

    /// A live broadcast has no seekable range. Reporting an infinite one is what
    /// makes the system draw PiP without a scrubber, matching how it treats a
    /// live HLS stream.
    public func pictureInPictureControllerTimeRangeForPlayback(_ controller: AVPictureInPictureController) -> CMTimeRange {
        CMTimeRange(start: .negativeInfinity, duration: .positiveInfinity)
    }

    public func pictureInPictureControllerIsPlaybackPaused(_ controller: AVPictureInPictureController) -> Bool {
        !(playbackDelegate?.isSystemVideoPlaying ?? true)
    }

    public func pictureInPictureController(
        _ controller: AVPictureInPictureController,
        didTransitionToRenderSize newRenderSize: CMVideoDimensions
    ) {
        logger.notice("[SystemVideo] PiP render size \(newRenderSize.width)x\(newRenderSize.height)")
    }

    /// Live playback cannot skip; the completion still has to be called or the
    /// system leaves the PiP controls stuck.
    public func pictureInPictureController(
        _ controller: AVPictureInPictureController,
        skipByInterval skipInterval: CMTime,
        completion completionHandler: @escaping () -> Void
    ) {
        completionHandler()
    }
}

// MARK: - AVPictureInPictureControllerDelegate

extension SystemVideoPresenter: @preconcurrency AVPictureInPictureControllerDelegate {

    /// Tells PiP that the answers it caches are out of date.
    ///
    /// AVKit reads `isPlaybackPaused` and the playback time range when it is
    /// told to, not when they change. Nothing told it: the PiP controls kept
    /// whatever state they were built with, so the play button stayed a pause
    /// button and pressing it did nothing — which is what "PiP hangs" looks
    /// like from the outside, with the picture itself still updating.
    public func playbackStateDidChange() {
        pictureInPictureController?.invalidatePlaybackState()
    }

    private func raise(_ text: String) {
        guard raisedWarnings.insert(text).inserted else { return }
        let msg = "[SystemVideo] ⚠️ \(text)"
        print(msg)
        logger.error("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
        onWarning?(text)
    }

    public func pictureInPictureControllerWillStartPictureInPicture(_ controller: AVPictureInPictureController) {
        pictureInPictureStartedAt = CACurrentMediaTime()
        logger.notice("[SystemVideo] PiP will start")
    }

    public func pictureInPictureControllerDidStopPictureInPicture(_ controller: AVPictureInPictureController) {
        pictureInPictureStartedAt = nil
        logger.notice("[SystemVideo] PiP stopped")
    }

    public func pictureInPictureController(
        _ controller: AVPictureInPictureController,
        failedToStartPictureInPictureWithError error: Error
    ) {
        // Cleared here as well as at didStop: a start that failed never reaches
        // didStop, and leaving the flag set would keep the background guard open
        // for a window that is not coming.
        pictureInPictureStartedAt = nil
        let msg = "[SystemVideo] ❌ PiP failed to start: \(error.localizedDescription)"
        print(msg)
        logger.error("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
        raise("Picture in Picture refused to start: \(error.localizedDescription)")
    }
}

/// Keeps one `SystemVideoPresenter` alive across SwiftUI view rebuilds.
///
/// `SystemVideoPresenter` owns the `AVPictureInPictureController`, and PiP dies
/// with its controller. Storing it in `@StateObject` rather than constructing it
/// inside the representable means a layout pass cannot end an active PiP session.
@MainActor
public final class SystemVideoPresenterBox: ObservableObject {
    public let presenter = SystemVideoPresenter()
    public init() {}
}
