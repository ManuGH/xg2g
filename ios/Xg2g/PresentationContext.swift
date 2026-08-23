// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation
import OSLog

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "presentation")

// Who owns the screen.
//
// There is one visible surface - a single AVSampleBufferDisplayLayer, hosted by a
// single MetalVideoView - and until now every playback pipeline reached into it
// directly. That was survivable while exactly one pipeline existed. It stops being
// survivable the moment a second one is prepared beside the first: attaching a
// synchronizer removes the previous renderer from the display layer, flushing
// discards the running channel's queued fields, and resetting the generation makes
// the running channel's frames fail their own identity check. A preparation would
// have blacked out the channel it was supposed to protect, before any commit.
//
// So ownership of the surface is taken away from the sessions and given to one
// object. A session produces pictures and audio and binds to nothing; this decides
// which session the surface belongs to, and that decision is a single atomic step on
// the main actor.
//
// Two capabilities are modelled as values rather than as rules, because a rule that
// only exists in a comment is a rule that gets broken by the next edit:
//
//   - A `PresentationGeneration` cannot be constructed outside this file. A session
//     can hold one and stamp its output with it, but cannot mint one, so no session
//     can declare itself the visible stream.
//   - An `AudibleGrant` likewise. A session's clock cannot be started without one,
//     so a prepared session cannot become audible by anyone accidentally setting a
//     rate on it.

/// Identifies which stream the visible surface belongs to.
///
/// Allocated only by `PresentationContext`, and monotonic across every session in the
/// process. Per-instance counters were the previous arrangement, and two sessions
/// would each have begun at one: the running channel's stale frames would have passed
/// the incoming channel's identity check exactly.
public struct PresentationGeneration: Hashable, Sendable, CustomStringConvertible {
    fileprivate let value: Int

    public var description: String { "gen-\(value)" }

    /// The generation nothing is ever stamped with, for a surface that owns nothing.
    public static let none = PresentationGeneration(value: 0)

    /// Bridges to the interfaces that still carry a plain integer generation.
    public var rawValue: Int { value }
}

/// The right to be heard.
///
/// A prepared session decodes, buffers and proves itself while contributing no sound
/// at all. Its clock can only be started with this, and only the presentation context
/// issues it - at the moment it hands the surface over, and never before.
public struct AudibleGrant: Sendable {
    fileprivate let generation: PresentationGeneration

    /// The generation this grant makes audible.
    public var presentationGeneration: PresentationGeneration { generation }
}

/// What the presentation context requires of something it can put on screen.
///
/// Deliberately narrow. A session is asked for its clock, told which generation it is
/// and whether it may be heard, and nothing else - it never learns what it is being
/// bound to, which is what keeps it from reaching back into the surface.
@MainActor
public protocol PresentablePlaybackSession: AnyObject {
    /// The clock this session's audio and video are timed against.
    var presentationSynchronizer: AVSampleBufferRenderSynchronizer { get }

    /// Stamps everything this session emits. Set by the context, never by the session.
    var presentationGeneration: PresentationGeneration { get set }

    /// Starts this session's clock. Requires a grant, so it cannot happen by accident.
    func becomeAudible(_ grant: AudibleGrant)

    /// Stops this session's clock and silences it.
    func silence()

    /// Whether this session has a picture and usable audio at a common start anchor.
    var isPresentable: Bool { get }

    /// Whether this session carries an interlaced or progressive broadcast.
    var isSourceInterlaced: Bool { get }

    /// Callback invoked when the very first picture is actually rendered/presented on screen.
    var onFirstPictureVisible: (@Sendable () -> Void)? { get set }

    // What the surface reports back to whoever owns it. The context installs these on
    // the view and presenter and forwards them to the bound session, so a session
    // never holds a reference to either and rebinding redirects them by itself.
    func surfaceDidSubmitFirstField(pts: CMTime)
    func surfaceDidRenderFirstFrame()
    func surfaceDidPresentFirstFrame(atScreenTime screenTime: CFTimeInterval)
    func surfaceDidPresentFirstFieldImmediately()
    func surfaceDidWarn(_ text: String)
}

/// The visible surface, as the presentation context needs it.
///
/// Narrow on purpose, and extracted for the same reason the audio sink was: the last
/// thing keeping the deterministic proof of the zap transaction away from the ordinary
/// test run was that this still built a real `AVSampleBufferDisplayLayer` and
/// registered it with a synchronizer. That is media services, it dies on its own in a
/// simulator, and it took the test host with it often enough that the suite had to be
/// opt-in.
///
/// Production uses `SystemVideoPresenter` and therefore the real display layer, the
/// real Picture-in-Picture controller and the real renderer registration. Only tests
/// substitute anything.
@MainActor
public protocol PresentationSurface: AnyObject {
    /// Which generation the surface admits. Anything else is discarded.
    var currentGeneration: Int { get set }

    /// How much is still queued, for diagnostics.
    var pendingSamplesCount: Int { get }

    /// Reported when the first field of a newly bound stream goes up immediately.
    var onFirstFieldPresentedImmediately: (() -> Void)? { get set }

    /// Reported when the surface has something to say about what it was given.
    var onWarning: ((String) -> Void)? { get set }

    /// Moves the surface onto a session's clock.
    func attach(to synchronizer: AVSampleBufferRenderSynchronizer)

    /// Takes it off again.
    func detach(from synchronizer: AVSampleBufferRenderSynchronizer)

    /// Discards what is queued and re-arms the immediate field.
    func flush(generation: Int)

    /// Tells the Picture-in-Picture controls that playback started or stopped.
    func playbackStateDidChange()
}

extension SystemVideoPresenter: PresentationSurface {}

/// Owns the visible surface and decides which session it belongs to.
@MainActor
public final class PresentationContext {
    private let presenter: any PresentationSurface
    private weak var renderView: MetalVideoView?

    /// The session currently on screen, if any.
    public private(set) weak var boundSession: (any PresentablePlaybackSession)?

    /// The generation the surface currently accepts. Anything else is discarded, and
    /// that is what makes a retired session harmless while it is still winding down.
    public private(set) var visibleGeneration: PresentationGeneration = .none

    private var nextGenerationValue = 1

    public init(presenter: any PresentationSurface, renderView: MetalVideoView?) {
        self.presenter = presenter
        self.renderView = renderView
        outlet.update(view: renderView, visibleGeneration: PresentationGeneration.none.rawValue)
    }

    // Picture-in-Picture is set up once by whoever builds the surface, not on every
    // commit: it belongs to the display layer's lifetime, and re-arming it per channel
    // change would tear down a running PiP session on each zap.

    /// Attaches the visible surface, which the screen owns and may recreate.
    public func setRenderView(_ view: MetalVideoView?) {
        renderView = view
        outlet.update(view: view, visibleGeneration: visibleGeneration.rawValue)
        guard let view else { return }
        view.currentGeneration = visibleGeneration.rawValue
        if let boundSession {
            view.synchronizer = boundSession.presentationSynchronizer
        }
    }

    /// Issues the next generation and stamps the session with it.
    ///
    /// A session is given its identity when it is created, not when it becomes
    /// visible: everything it decodes while preparing already carries the stamp the
    /// surface will later accept, so a commit changes what is accepted and nothing
    /// else has to be relabelled.
    public func issueGeneration(to session: any PresentablePlaybackSession) -> PresentationGeneration {
        let generation = PresentationGeneration(value: nextGenerationValue)
        nextGenerationValue += 1
        session.presentationGeneration = generation
        logger.notice("[Presentation] issued \(generation.description, privacy: .public)")
        return generation
    }

    /// Hands the visible surface to a session, atomically.
    ///
    /// The order is the whole point. The surface starts accepting the incoming
    /// generation and stops accepting the outgoing one in the same main-actor step, so
    /// there is no interval in which both are accepted or neither is. Only then is the
    /// incoming session made audible.
    ///
    /// Returns false and changes nothing if the session is not presentable, which
    /// leaves the outgoing session owning the surface exactly as it was.
    @discardableResult
    public func bind(_ session: any PresentablePlaybackSession) -> Bool {
        guard session.isPresentable else {
            logger.error("[Presentation] refused to bind a session that is not presentable; surface unchanged")
            return false
        }

        let incoming = session.presentationGeneration
        guard incoming != .none else {
            logger.error("[Presentation] refused to bind a session with no generation; surface unchanged")
            return false
        }

        let outgoing = boundSession
        let previousGeneration = visibleGeneration

        // From here to the end of this method nothing may suspend: the surface must
        // never be observable in a state where it accepts both generations.
        visibleGeneration = incoming
        boundSession = session
        outlet.update(visibleGeneration: incoming.rawValue)

        presenter.currentGeneration = incoming.rawValue
        presenter.attach(to: session.presentationSynchronizer)
        presenter.flush(generation: incoming.rawValue)

        renderView?.synchronizer = session.presentationSynchronizer
        renderView?.sourceIsInterlaced = session.isSourceInterlaced
        renderView?.resetForChannelZap(generation: incoming.rawValue)

        installSurfaceCallbacks(for: session)
        session.becomeAudible(AudibleGrant(generation: incoming))

        if let outgoing, outgoing !== session {
            // Silenced, not torn down. Retiring it is the caller's next step, and
            // doing it here would mean a bind that fails halfway has already
            // destroyed what it was replacing.
            outgoing.silence()
        }

        logger.notice("""
            [Presentation] surface bound to \(incoming.description, privacy: .public) \
            (was \(previousGeneration.description, privacy: .public))
            """)
        return true
    }

    /// Points the surface's reports at the session that now owns it.
    ///
    /// Weak throughout: a retired session must not be kept alive by the surface still
    /// holding a closure that names it.
    private func installSurfaceCallbacks(for session: any PresentablePlaybackSession) {
        presenter.onFirstFieldPresentedImmediately = { [weak session] in
            session?.surfaceDidPresentFirstFieldImmediately()
        }
        presenter.onWarning = { [weak session] text in
            session?.surfaceDidWarn(text)
        }
        renderView?.onFirstFieldSubmitted = { [weak session] pts in
            session?.surfaceDidSubmitFirstField(pts: pts)
        }
        renderView?.onFirstFrameRendered = { [weak session] in
            session?.surfaceDidRenderFirstFrame()
        }
        renderView?.onFirstFrameActuallyPresentedOnScreen = { [weak session] screenTime in
            session?.surfaceDidPresentFirstFrame(atScreenTime: screenTime)
        }
    }

    /// Re-arms the surface for the session that owns it.
    ///
    /// The recovery paths - a timeline jump, a decoder restart, a format change - have
    /// to clear what is queued and re-arm the immediate field. That is legitimate for
    /// the session on screen and meaningless for one still preparing, so the request
    /// is ignored unless it comes from the bound session. A preparing session asking
    /// for it is not an error to report; it is simply not its surface to re-arm.
    public func requestReset(from session: any PresentablePlaybackSession) {
        guard session === boundSession else { return }
        presenter.flush(generation: visibleGeneration.rawValue)
        renderView?.resetForChannelZap(generation: visibleGeneration.rawValue)
    }

    /// Re-attaches the surface to the bound session's clock after it was replaced.
    ///
    /// A session builds a fresh synchronizer when it restarts its renderer, and the
    /// display layer has to follow it. Same ownership rule.
    public func requestReattach(from session: any PresentablePlaybackSession) {
        guard session === boundSession else { return }
        presenter.attach(to: session.presentationSynchronizer)
        renderView?.synchronizer = session.presentationSynchronizer
        presenter.flush(generation: visibleGeneration.rawValue)
        renderView?.resetForChannelZap(generation: visibleGeneration.rawValue)
    }

    /// Where decoded pictures go.
    ///
    /// Separate from the context because pictures arrive on the decoder's queue and the
    /// context is main-actor. The outlet is the only thing a session can reach from a
    /// background thread, it holds the view reference so no session has to, and it
    /// admits a picture only if the generation stamped on it is the one the surface
    /// currently belongs to. A preparing session therefore decodes at full rate and
    /// puts nothing on screen.
    public final class SurfaceOutlet: @unchecked Sendable {
        private let lock = NSLock()
        private weak var view: MetalVideoView?
        private var visibleGeneration = PresentationGeneration.none.rawValue

        fileprivate init() {}

        fileprivate func update(view: MetalVideoView?, visibleGeneration: Int) {
            lock.lock()
            self.view = view
            self.visibleGeneration = visibleGeneration
            lock.unlock()
        }

        fileprivate func update(visibleGeneration: Int) {
            lock.lock()
            self.visibleGeneration = visibleGeneration
            lock.unlock()
        }

        /// Whether the surface currently belongs to this generation.
        ///
        /// Answerable from any thread, which the main-actor context is not. The clock
        /// start runs on the ingest queue and has to ask this before it makes a session
        /// audible.
        public func owns(_ generation: PresentationGeneration) -> Bool {
            guard generation != .none else { return false }
            lock.lock()
            let visible = visibleGeneration
            lock.unlock()
            return generation.rawValue == visible
        }

        /// Admits a picture if it belongs to the stream the surface is showing.
        public func enqueue(_ frame: DecodedVideoFrame) {
            lock.lock()
            let target = view
            let visible = visibleGeneration
            lock.unlock()
            // Generation zero is what an unstamped session emits. It must never match,
            // or a session the surface does not belong to would be admitted precisely
            // while the surface belongs to nothing.
            guard frame.generation != PresentationGeneration.none.rawValue,
                  frame.generation == visible,
                  let target else { return }
            target.enqueueFrame(frame)
        }
    }

    /// The outlet decoded pictures are handed to.
    public let outlet = SurfaceOutlet()

    /// Tells the surface how the source is scanned, if this session owns it.
    public func setSourceInterlaced(_ interlaced: Bool, from session: any PresentablePlaybackSession) {
        guard session === boundSession else { return }
        renderView?.sourceIsInterlaced = interlaced
    }

    /// How much the surface still has queued, for diagnostics.
    public var pendingSamplesCount: Int { presenter.pendingSamplesCount }

    /// Tells the PiP controls that playback started or stopped, if this session owns
    /// the surface. A preparing session's clock says nothing about what is on screen.
    public func notePlaybackStateChanged(from session: any PresentablePlaybackSession) {
        guard session === boundSession else { return }
        presenter.playbackStateDidChange()
    }

    /// Identity-only variant, for a session that may already be deallocating and so
    /// cannot be referenced. The pointer is compared and never dereferenced.
    public func notePlaybackStateChanged(fromSessionIdentity identity: UInt) {
        guard let boundSession,
              unsafeBitCast(boundSession as AnyObject, to: UInt.self) == identity else { return }
        presenter.playbackStateDidChange()
    }

    /// A still of what is actually on screen, for diagnostics.
    public func captureVisibleFrameJPEG() -> Data? { renderView?.captureCurrentFrameJPEG() }

    /// Whether output stamped with this generation may reach the surface.
    ///
    /// The single question every enqueue path asks. A session that is still winding
    /// down keeps producing for a moment; this is what makes that harmless.
    public func accepts(_ generation: PresentationGeneration) -> Bool {
        generation != .none && generation == visibleGeneration
    }

    /// Binds unconditionally, for the single-channel path that has no preparation to
    /// wait for.
    ///
    /// Separate from `bind` on purpose: `bind` refuses a session that is not yet
    /// presentable, which is the guarantee a channel change depends on. A screen that
    /// only ever shows one channel has nothing to protect and has to be able to hand
    /// the surface over before the first picture exists.
    /// Gives the surface to a session with no preparation behind it.
    ///
    /// The direct and legacy routes go straight at the receiver, so there is nothing to
    /// have proven and no readiness to check — the session starts its own clock once it
    /// owns the surface. That is the only difference from a commit. Everything else has
    /// to happen exactly as it does there, and one of those things is not optional:
    /// flushing the presenter re-arms the display layer's request for media data. Left
    /// out, the layer never asks again, the presenter's queue fills, every frame after
    /// the first few is dropped, and the picture freezes with the sound still playing —
    /// measured on device, and indistinguishable from a decoder fault until you count
    /// the pulls that came back not-ready.
    public func bindWithoutPreparation(_ session: any PresentablePlaybackSession) {
        let incoming = session.presentationGeneration
        guard incoming != .none else { return }

        let outgoing = boundSession
        visibleGeneration = incoming
        boundSession = session
        outlet.update(visibleGeneration: incoming.rawValue)

        presenter.currentGeneration = incoming.rawValue
        presenter.attach(to: session.presentationSynchronizer)
        presenter.flush(generation: incoming.rawValue)

        renderView?.synchronizer = session.presentationSynchronizer
        renderView?.sourceIsInterlaced = session.isSourceInterlaced
        renderView?.resetForChannelZap(generation: incoming.rawValue)

        installSurfaceCallbacks(for: session)
        session.becomeAudible(AudibleGrant(generation: incoming))

        if let outgoing, outgoing !== session {
            outgoing.silence()
        }
    }

    /// Releases the surface without giving it to anything.
    public func unbind() {
        guard let session = boundSession else { return }
        session.silence()
        presenter.detach(from: session.presentationSynchronizer)
        boundSession = nil
        visibleGeneration = .none
        outlet.update(visibleGeneration: PresentationGeneration.none.rawValue)
        presenter.currentGeneration = PresentationGeneration.none.rawValue
        renderView?.synchronizer = nil
        logger.notice("[Presentation] surface released")
    }
}
