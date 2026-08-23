// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia

/// The audio side of a playback session, as the session needs it.
///
/// Extracted so the session's own logic can be proved without Apple's media services
/// in the way. Choosing a start anchor, re-anchoring after a discontinuity, deciding
/// that a prepared channel is presentable, and handing the surface over are ours; an
/// `AVSampleBufferAudioRenderer` running in a simulator whose media services die on
/// their own is not, and a test that exercised both at once could not say which had
/// failed. It failed about one run in two, and every green run was luck.
///
/// Production keeps using `NativeTSAudioRenderer` and therefore the real renderer,
/// the real synchronizer and the real audio session. Only tests substitute anything,
/// and what they substitute is exactly the part that was never ours to begin with.
///
/// The clock stays an `AVSampleBufferRenderSynchronizer` even here: it is what the
/// display layer must be attached to, it costs nothing to create, and it does not
/// depend on media services. Faking it would only weaken the test.
public protocol PlaybackAudioOutput: AnyObject {
    /// The clock this session's audio and video are timed against.
    var synchronizer: AVSampleBufferRenderSynchronizer { get }

    /// Reports failures and status changes back to the session.
    var delegate: NativeTSAudioRendererDelegate? { get set }

    /// What the underlying renderer reports about itself.
    var status: AVQueuedSampleBufferRenderingStatus { get }

    /// Why the renderer failed, when it has.
    var failureReason: Error? { get }

    /// Hands one decoded audio frame over for playback.
    func enqueue(sampleBuffer: CMSampleBuffer)

    /// Starts or stops the clock at a timestamp.
    func setRate(_ rate: Float, time: CMTime)

    /// Parks the clock.
    func stopClock()

    /// Discards what is queued, keeping the renderer.
    func flush()

    /// Replaces the renderer after a failure, carrying audibility across.
    func reset()

    /// Whether this session contributes sound. False until it owns the surface.
    func setAudible(_ audible: Bool)

    /// Whether it currently does. Read back rather than inferred, so a test asserts
    /// the state the renderer is actually in.
    var isAudible: Bool { get }

    /// Whether audio is available from an instant onwards, counting what has already
    /// been handed to the renderer as well as what is still queued.
    func hasBuffersCovering(_ anchor: CMTime) -> Bool

    /// Drops everything that ends before an instant, once, and says what it dropped.
    @discardableResult
    func pruneBuffersBefore(time anchor: CMTime) -> NativeTSAudioRenderer.PruneResult

    /// Flow statistics since the last time they were read.
    func consumeFlowStats() -> NativeTSAudioRenderer.AudioFlowStats
}

extension NativeTSAudioRenderer: PlaybackAudioOutput {
    /// Bridges the concrete renderer's error to the protocol's name, which does not
    /// say "audioRenderer.audioRenderer.error".
    public var failureReason: Error? { audioRenderer.error }
}
