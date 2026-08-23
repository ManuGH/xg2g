// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Combine
import Foundation
import OSLog

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "zap")

/// One channel change, from the tap to the moment the old channel stops.
///
/// The sequence is the whole point, and it is the opposite of what it used to be. The
/// old one tore the running channel down and then went looking for the new one, which
/// is why a failed tune left a frozen picture on screen for as long as the viewer was
/// willing to stare at it. This one warms the new channel beside the old, proves it can
/// be shown, hands the surface over in a single step, and only then lets the old one go.
///
///     tap → prepare → transport ready → decode and buffer → presentable
///         → commit → retire
///
/// Every step carries the same zap identifier, so the backend's timeline and this one
/// describe the same channel change rather than two adjacent stories.
///
/// This also owns the surface the sessions are shown on, because something has to hold
/// it across SwiftUI rebuilding the screen, and the object that decides which session is
/// visible is the honest place for it. Which session the surface *belongs* to remains
/// the presentation context's decision alone.
@MainActor
final class ZapCoordinator: ObservableObject {

    /// Where a channel change has got to.
    enum Phase: Equatable {
        case idle
        /// The backend is warming the channel; the old one is still playing.
        case warming(serviceRef: String)
        /// The channel is arriving here and is not yet worth showing.
        case buffering(serviceRef: String)
        /// It failed, and the old channel was not disturbed.
        case failed(serviceRef: String, reason: String)
    }

    @Published private(set) var phase: Phase = .idle

    /// The session on screen. Published so the screen's telemetry follows the channel
    /// that is actually visible rather than whichever one was built last.
    @Published private(set) var playing: NativeTSVideoPipeline?

    /// The visible surface. The screen hosts its layer; it never decides what goes on it.
    let surface: SystemVideoPresenter

    /// Who the surface belongs to.
    let context: PresentationContext

    private struct InFlight {
        let zapID: String
        let serviceRef: String
        let preparationID: String
        let startedAt: CFTimeInterval
        var session: NativeTSVideoPipeline?
    }

    private var inFlight: InFlight?

    /// Republishes the visible session's changes as this object's own.
    ///
    /// The screen used to hold the single pipeline as its `@StateObject` and redrew
    /// whenever it announced something. With the session behind this coordinator that
    /// chain is broken, and the readouts would freeze on whatever they showed when the
    /// channel was committed. Only the visible session is followed, and the previous
    /// subscription is dropped at the same moment the surface changes hands.
    private var visibleSessionObserver: AnyCancellable?

    /// The channel change currently being attempted.
    ///
    /// Separate from `inFlight`, which only exists once the backend has answered. A
    /// viewer holding the channel-down button produces zaps faster than the receiver
    /// tunes, and without this the first one would still be waiting on its preparation
    /// when the second overtook it — and would then build a session and claim the
    /// in-flight slot the second one was using.
    private var activeZapID: String?

    private var zapCounter = 0

    /// `nil` when there is no backend to prepare against, which is what the direct
    /// receiver route and the legacy smoother are. Those cannot make before they
    /// break and do not pretend to.
    private let preparations: ZapPreparationClient?
    private let streamURL: @Sendable (String) -> URL?
    private let makeSession: @MainActor () -> NativeTSVideoPipeline

    init(preparations: ZapPreparationClient?,
         streamURL: @escaping @Sendable (String) -> URL?,
         makeSession: @escaping @MainActor () -> NativeTSVideoPipeline = { NativeTSVideoPipeline() }) {
        let surface = SystemVideoPresenter()
        self.surface = surface
        self.context = PresentationContext(presenter: surface, renderView: nil)
        self.preparations = preparations
        self.streamURL = streamURL
        self.makeSession = makeSession

        // Player-lifetime policy, configured once. Sessions come and go beneath it, and
        // one that reconfigured the process every time a channel was prepared would be
        // interrupting the channel it is being prepared beside.
        AudioSessionManager.shared.configureForPlayback()

        // What is on screen is a property of the surface, not of any one session, so
        // the screenshot endpoint asks the context and keeps working across a commit.
        TelemetryServer.shared.setScreenshotProvider { [weak self] in
            self?.context.captureVisibleFrameJPEG()
        }
    }

    /// How often the backend and the local session are asked whether they are ready.
    ///
    /// A poll interval, not a timeout: neither side is given a deadline here. The
    /// backend settles its own preparation and says so, and the local session either
    /// becomes presentable or the preparation it belongs to is superseded. Inventing a
    /// client-side deadline on top would only add a second opinion about when a
    /// broadcast has failed.
    private static let pollInterval = Duration.milliseconds(40)

    // MARK: -

    /// Changes channel through the backend's preparation.
    ///
    /// Returns when the new channel is on screen, or when it has failed and the old one
    /// is still on screen. Never leaves the viewer looking at a still frame with nothing
    /// happening behind it.
    /// Whether a channel can be warmed before it is shown.
    var canPrepare: Bool { preparations != nil }

    func zap(to serviceRef: String) async {
        guard let preparations else {
            // Nothing to prepare against. Saying so beats silently behaving like the
            // old tear-down-first player while claiming to be a transaction.
            if let url = streamURL(serviceRef) {
                await play(unprepared: url)
            }
            return
        }
        zapCounter += 1
        let zapID = NativeTSVideoPipeline.zapIdentifier(zapCounter)
        activeZapID = zapID

        // One channel change at a time. A viewer moving through a list produces a zap
        // per press, and each one supersedes the last rather than queueing behind it.
        await abandonInFlight(reason: "superseded by \(zapID)")
        guard isCurrent(zapID) else { return }

        let started = CACurrentMediaTime()
        note(zapID, "prepare.requested", serviceRef)
        phase = .warming(serviceRef: serviceRef)

        let preparation: ZapPreparation
        do {
            preparation = try await preparations.start(serviceRef: serviceRef, zapID: zapID)
        } catch {
            guard isCurrent(zapID) else { return }
            return fail(zapID, serviceRef, "the receiver could not be asked to prepare: \(error.localizedDescription)")
        }
        guard isCurrent(zapID) else {
            await preparations.cancel(preparation.preparationId, zapID: zapID)
            return
        }

        // Tracked from here on, so a zap that supersedes this one releases the tuner
        // rather than leaving it held until the backend times the preparation out.
        inFlight = InFlight(zapID: zapID, serviceRef: serviceRef,
                            preparationID: preparation.preparationId, startedAt: started)

        // Transport readiness: the backend proves the receiver is delivering something
        // presentable before anything is built here. A preparation that fails says which
        // condition it was waiting on, which is the difference between "not yet" and
        // "never".
        let settled: ZapPreparation
        do {
            settled = try await awaitSettled(preparation, using: preparations, zapID: zapID)
        } catch {
            guard isCurrent(zapID) else { return }
            await abandonInFlight(reason: "lost track of the preparation")
            return fail(zapID, serviceRef, "lost track of the preparation: \(error.localizedDescription)")
        }
        guard isCurrent(zapID) else { return }

        guard settled.parsedState == .ready, let backendGeneration = settled.generation else {
            await abandonInFlight(reason: "preparation did not become ready")
            guard isCurrent(zapID) else { return }
            return fail(zapID, serviceRef, settled.failureSummary)
        }
        note(zapID, "transport.ready", serviceRef, extra: "after \(settled.readyAfterMs ?? -1)ms")

        // The stream is warm on the backend, so opening it here coalesces onto the
        // ingest that was just proven rather than dialling the receiver a second time.
        guard let url = streamURL(serviceRef) else {
            await abandonInFlight(reason: "no stream address")
            guard isCurrent(zapID) else { return }
            return fail(zapID, serviceRef, "no stream address for \(serviceRef)")
        }

        let session = makeSession()
        session.presentationContext = context
        _ = context.issueGeneration(to: session)
        inFlight?.session = session

        phase = .buffering(serviceRef: serviceRef)
        session.startStreaming(url: url, requestedAt: started)

        // Presentation readiness is decided here, not by the backend: a picture decoded,
        // audio that covers the instant the clock will start on, and no recovery in
        // progress. The backend can only say the transport is sound.
        let ready = await awaitPresentable(session, zapID: zapID)
        guard isCurrent(zapID) else { return }
        guard ready else {
            await abandonInFlight(reason: "never became presentable")
            guard isCurrent(zapID) else { return }
            return fail(zapID, serviceRef, "the channel arrived but never became presentable")
        }
        note(zapID, "presentation.ready", serviceRef,
             extra: "after \(Int((CACurrentMediaTime() - started) * 1000))ms")

        // The commit. Ownership of the surface moves in one step, and only then does the
        // old channel stop.
        guard context.bind(session) else {
            await abandonInFlight(reason: "commit refused")
            guard isCurrent(zapID) else { return }
            return fail(zapID, serviceRef, "the surface refused the channel")
        }
        note(zapID, "commit", serviceRef,
             extra: "generation \(session.presentationGeneration.rawValue)")

        let retiring = playing
        playing = session
        follow(session)
        inFlight = nil
        phase = .idle
        // The telemetry endpoint answers for the channel on screen, which is why this
        // moves at the commit and not when the session was built: for the whole
        // preparation the numbers worth reading are still the playing channel's.
        installTelemetry(for: session)

        if let retiring {
            summarize(retiring, zapID: zapID, event: "retire.stats")
            retiring.stopStreaming()
        }
        note(zapID, "retire", serviceRef,
             extra: "total \(Int((CACurrentMediaTime() - started) * 1000))ms")

        // Told last, and its answer changes nothing on screen. The picture and audio are
        // already proven here; a bookkeeping call that fails is worth a log line, not a
        // black screen.
        do {
            _ = try await preparations.commit(settled.preparationId, generation: backendGeneration, zapID: zapID)
        } catch {
            note(zapID, "commit.unacknowledged", serviceRef, extra: error.localizedDescription)
        }
    }

    /// Plays a stream that has no preparation behind it.
    ///
    /// The direct and legacy routes go straight at the receiver or the old smoother, and
    /// there is nothing on the backend to warm or to prove. So this cannot make before it
    /// breaks, and does not pretend to: the surface is handed over unconditionally and
    /// the session starts its own clock once it owns it.
    func play(unprepared url: URL, requestedAt: CFTimeInterval = CACurrentMediaTime()) async {
        zapCounter += 1
        let zapID = NativeTSVideoPipeline.zapIdentifier(zapCounter)
        activeZapID = zapID

        await abandonInFlight(reason: "superseded by \(zapID)")
        guard isCurrent(zapID) else { return }

        note(zapID, "direct.start", url.lastPathComponent)

        let session = makeSession()
        session.presentationContext = context
        _ = context.issueGeneration(to: session)
        context.bindForSingleChannelHarness(session)

        let retiring = playing
        playing = session
        follow(session)
        phase = .idle
        installTelemetry(for: session)

        session.startStreaming(url: url, requestedAt: requestedAt)

        if let retiring, retiring !== session {
            summarize(retiring, zapID: zapID, event: "retire.stats")
            retiring.stopStreaming()
        }
    }

    /// Stops everything, for a screen going away.
    func stop() async {
        activeZapID = nil
        await abandonInFlight(reason: "player closed")
        context.unbind()
        if let playing {
            summarize(playing, zapID: "final", event: "stop.stats")
            playing.stopStreaming()
        }
        playing = nil
        visibleSessionObserver = nil
        phase = .idle
        TelemetryServer.shared.setTelemetryProvider { [:] }
    }

    // MARK: -

    /// Whether this channel change is still the one being attempted.
    private func isCurrent(_ zapID: String) -> Bool { activeZapID == zapID }

    private func awaitSettled(_ preparation: ZapPreparation, using preparations: ZapPreparationClient,
                              zapID: String) async throws -> ZapPreparation {
        var latest = preparation
        while !latest.isSettled {
            try? await Task.sleep(for: Self.pollInterval)
            guard isCurrent(zapID) else { return latest }
            latest = try await preparations.status(latest.preparationId, zapID: zapID)
        }
        return latest
    }

    private func awaitPresentable(_ session: NativeTSVideoPipeline, zapID: String) async -> Bool {
        while isCurrent(zapID) {
            if session.isPresentable { return true }
            try? await Task.sleep(for: Self.pollInterval)
        }
        return false
    }

    /// Drops whatever preparation is in flight, on both sides.
    ///
    /// The session is torn down and the backend told, so a superseded preparation stops
    /// holding a tuner rather than waiting to time out. The channel on screen is not
    /// touched: that is the guarantee the whole transaction exists for.
    private func abandonInFlight(reason: String) async {
        guard let flight = inFlight else { return }
        inFlight = nil
        flight.session?.stopStreaming()
        await preparations?.cancel(flight.preparationID, zapID: flight.zapID)
        note(flight.zapID, "prepare.abandoned", flight.serviceRef, extra: reason)
    }

    private func fail(_ zapID: String, _ serviceRef: String, _ reason: String) {
        phase = .failed(serviceRef: serviceRef, reason: reason)
        note(zapID, "zap.failed", serviceRef, extra: reason)
    }

    /// Follows the visible session, so a screen bound to this object keeps redrawing.
    private func follow(_ session: NativeTSVideoPipeline) {
        visibleSessionObserver = session.objectWillChange.sink { [weak self] _ in
            self?.objectWillChange.send()
        }
    }

    /// Points the telemetry endpoint at one session.
    ///
    /// Weak, and replaced rather than cleared: a retired session going away must not
    /// blank the numbers of the channel that replaced it.
    private func installTelemetry(for session: NativeTSVideoPipeline) {
        TelemetryServer.shared.setTelemetryProvider { [weak session] in
            session?.telemetry.toDictionary() ?? [:]
        }
    }

    /// What a session did while it was on screen.
    ///
    /// Emitted when it stops, which is the only moment the figures are final. Read from
    /// the locked snapshot rather than the published mirror, so the last half second of
    /// a channel that just failed is not missing from its own summary.
    private func summarize(_ session: NativeTSVideoPipeline, zapID: String, event: String) {
        let t = session.telemetry.snapshot()
        note(zapID, event, "\(session.presentationGeneration)", extra: """
            audioLead \(Int(t.audioLeadMs))ms (min \(Int(t.audioMinLeadMs))ms) | \
            underruns \(t.audioUnderruns) | decodeErrors \(t.decodeErrors) | \
            recoveries \(t.decoderRecoveries) | dropped \(t.droppedFrames) | \
            late \(t.lateFrames) | ptsDiscontinuities \(t.ptsDiscontinuities) | \
            earlyIssues \(t.earlyStabilityIssues) | \(session.recoveryEpoch) | \
            ttfp \(Int(t.ttfpTotalMs))ms visible \(Int(t.ttfpVisibleMs))ms
            """)
    }

    private func note(_ zapID: String, _ event: String, _ serviceRef: String, extra: String = "") {
        let suffix = extra.isEmpty ? "" : " | \(extra)"
        let msg = "[ZAP \(zapID)] \(event) \(serviceRef)\(suffix)"
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }
}
