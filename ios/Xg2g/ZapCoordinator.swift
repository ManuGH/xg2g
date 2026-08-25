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

    /// The service currently holding presentation ownership on the visible surface.
    /// This is the Single Source of Truth for which channel is active in sound & picture.
    @Published private(set) var presentedServiceRef: String?

    /// The service whose first frame is actually visible on the display.
    /// Header, Logo, EPG and NowPlaying switch exactly when this changes.
    @Published private(set) var displayedServiceRef: String?

    /// The service currently requested by the viewer and being prepared in the background.
    /// Non-nil only while prepare / transport / presentable are in progress.
    @Published private(set) var requestedServiceRef: String?

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
    private let preparationsProvider: (@MainActor () -> ZapPreparationClient?)?
    var preparations: ZapPreparationClient? { preparationsProvider?() }

    /// Main-actor isolated, not `@Sendable`: the addresses come from the app model,
    /// which is main-actor bound, and every call site here is too.
    private let streamURL: @MainActor (String) -> URL?
    private let makeSession: @MainActor () -> NativeTSVideoPipeline

    init(preparations: ZapPreparationClient? = nil,
         preparationsProvider: (@MainActor () -> ZapPreparationClient?)? = nil,
         streamURL: @escaping @MainActor (String) -> URL?,
         makeSession: @escaping @MainActor () -> NativeTSVideoPipeline = { NativeTSVideoPipeline() }) {
        let surface = SystemVideoPresenter()
        self.surface = surface
        self.context = PresentationContext(presenter: surface, renderView: nil)
        if let preparationsProvider {
            self.preparationsProvider = preparationsProvider
        } else if let preparations {
            self.preparationsProvider = { preparations }
        } else {
            self.preparationsProvider = nil
        }
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
        requestedServiceRef = serviceRef
        note(zapID, "user.requested", serviceRef, extra: "presented=\(presentedServiceRef ?? "none")")
        note(zapID, "prepare.requested", serviceRef)
        phase = .warming(serviceRef: serviceRef)

        let preparation: ZapPreparation
        do {
            preparation = try await preparations.start(serviceRef: serviceRef, zapID: zapID)
        } catch {
            guard isCurrent(zapID) else { return }
            return await failOrStartOutright(zapID, serviceRef, "the receiver could not be asked to prepare: \(describe(error))")
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
            return await failOrStartOutright(zapID, serviceRef, "lost track of the preparation: \(describe(error))")
        }
        guard isCurrent(zapID) else { return }

        guard settled.parsedState == .ready, let backendGeneration = settled.generation else {
            await abandonInFlight(reason: "preparation did not become ready")
            guard isCurrent(zapID) else { return }
            return await failOrStartOutright(
                zapID,
                serviceRef,
                settled.failureSummary,
                isAdmissionDenied: settled.isAdmissionDenied
            )
        }
        let transportReadyAt = CACurrentMediaTime()
        let transportMs = Int((transportReadyAt - started) * 1000)
        note(zapID, "transport.ready", serviceRef, extra: "stage=+\(transportMs)ms (backend=\(settled.readyAfterMs ?? -1)ms)")

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
        let presentationReadyAt = CACurrentMediaTime()
        let presentationMs = Int((presentationReadyAt - transportReadyAt) * 1000)
        note(zapID, "presentation.ready", serviceRef,
             extra: "stage=+\(presentationMs)ms total=\(Int((presentationReadyAt - started) * 1000))ms")

        // The commit. Ownership of the surface moves in one step, and only then does the
        // old channel stop and the presentedChannel change.
        guard context.bind(session) else {
            await abandonInFlight(reason: "commit refused")
            guard isCurrent(zapID) else { return }
            return fail(zapID, serviceRef, "the surface refused the channel")
        }
        let bindAt = CACurrentMediaTime()
        let bindMs = Int((bindAt - presentationReadyAt) * 1000)
        note(zapID, "presentation.bind", serviceRef,
             extra: "stage=+\(bindMs)ms generation=\(session.presentationGeneration.rawValue)")

        let retiring = playing

        session.onFirstPictureVisible = { [weak self, weak session, weak retiring] in
            Task { @MainActor [weak self, weak session, weak retiring] in
                guard let self, let session, self.playing === session, self.isCurrent(zapID) else { return }
                let firstFrameAt = CACurrentMediaTime()
                let firstFrameMs = Int((firstFrameAt - bindAt) * 1000)
                let totalMs = Int((firstFrameAt - started) * 1000)
                self.note(zapID, "firstVisibleFrame", serviceRef, extra: "stage=+\(firstFrameMs)ms total=\(totalMs)ms")
                self.displayedServiceRef = serviceRef
                self.note(zapID, "displayedChannel.changed", serviceRef, extra: "total=\(totalMs)ms")
                if self.requestedServiceRef == serviceRef {
                    self.requestedServiceRef = nil
                }
                self.retireSession(retiring, zapID: zapID, startedAt: started, serviceRef: serviceRef)
            }
        }

        // Safety fallback: if no first frame callback occurs within 500ms, retire old session anyway
        Task { [weak self, weak retiring] in
            try? await Task.sleep(for: .milliseconds(500))
            await MainActor.run { [weak self, weak retiring] in
                guard let self, self.isCurrent(zapID) else { return }
                if self.requestedServiceRef == serviceRef {
                    self.requestedServiceRef = nil
                }
                self.retireSession(retiring, zapID: zapID, startedAt: started, serviceRef: serviceRef)
            }
        }

        playing = session
        presentedServiceRef = serviceRef
        let totalMs = Int((bindAt - started) * 1000)
        note(zapID, "presentedChannel.changed", serviceRef,
             extra: "total=\(totalMs)ms [transport=\(transportMs)ms, presentation=\(presentationMs)ms, bind=\(bindMs)ms]")

        follow(session)
        inFlight = nil
        phase = .idle
        // The telemetry endpoint answers for the channel on screen, which is why this
        // moves at the commit and not when the session was built: for the whole
        // preparation the numbers worth reading are still the playing channel's.
        installTelemetry(for: session)

        // Told last, and its answer changes nothing on screen. The picture and audio are
        // already proven here; a bookkeeping call that fails is worth a log line, not a
        // black screen.
        do {
            _ = try await preparations.commit(settled.preparationId, generation: backendGeneration, zapID: zapID)
        } catch {
            note(zapID, "commit.unacknowledged", serviceRef, extra: describe(error))
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

        await startOutright(url: url, zapID: zapID, requestedAt: requestedAt)
    }

    /// Starts a channel without a preparation behind it, under an existing zap.
    private func startOutright(url: URL, zapID: String, requestedAt: CFTimeInterval) async {
        note(zapID, "direct.start", url.lastPathComponent)

        let session = makeSession()
        session.presentationContext = context
        _ = context.issueGeneration(to: session)

        // Started before it is bound, and the order is not cosmetic. Starting a
        // session tears down its previous one first, and that replaces the render
        // synchronizer - the clock the surface is attached to. Binding first attaches
        // the display layer to a synchronizer that is discarded moments later, so the
        // layer waits on a clock that will never run: it reports itself never ready,
        // the presenter's queue fills, and the picture freezes on the last frame that
        // got through while the audio, which followed the new clock, plays on.
        //
        // This is the order the prepared path has always used, for the same reason.
        session.startStreaming(url: url, requestedAt: requestedAt)
        context.bindWithoutPreparation(session)

        let retiring = playing
        playing = session
        presentedServiceRef = url.lastPathComponent
        displayedServiceRef = url.lastPathComponent
        requestedServiceRef = nil
        note(zapID, "presentedChannel.changed", url.lastPathComponent)

        follow(session)
        phase = .idle
        installTelemetry(for: session)

        if let retiring, retiring !== session {
            summarize(retiring, zapID: zapID, event: "retire.stats")
            retiring.stopStreaming()
        }
    }

    private func retireSession(_ session: NativeTSVideoPipeline?, zapID: String, startedAt: Double, serviceRef: String) {
        guard let session else { return }
        summarize(session, zapID: zapID, event: "retire.stats")
        session.stopStreaming()
        note(zapID, "retire", serviceRef, extra: "total \(Int((CACurrentMediaTime() - startedAt) * 1000))ms")
    }

    #if DEBUG
    /// Test-only hook invoked during `stop()` after `abandonInFlight` has completed,
    /// allowing deterministic barrier simulation of overlapping zap and stop operations.
    var stopYieldHook: (@MainActor () async -> Void)?
    #endif

    /// Stops everything, for a screen going away.
    func stop() async {
        let sessionToStop = self.playing
        let serviceRefToStop = self.presentedServiceRef
        activeZapID = nil
        await abandonInFlight(reason: "player closed")

        #if DEBUG
        if let stopYieldHook {
            await stopYieldHook()
        }
        #endif

        // Deterministic Identity & Generation Guard:
        // If a subsequent zap has started (activeZapID != nil) or already committed a new session (playing !== sessionToStop),
        // or a new service was presented (presentedServiceRef != serviceRefToStop),
        // tear down ONLY the captured old session and leave the coordinator state and new session intact.
        guard self.playing === sessionToStop && activeZapID == nil && presentedServiceRef == serviceRefToStop else {
            sessionToStop?.stopStreaming()
            return
        }

        context.unbind()
        if let playing {
            summarize(playing, zapID: "final", event: "stop.stats")
            playing.stopStreaming()
        }
        playing = nil
        presentedServiceRef = nil
        displayedServiceRef = nil
        requestedServiceRef = nil
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
        requestedServiceRef = nil
        guard let flight = inFlight else { return }
        inFlight = nil
        flight.session?.stopStreaming()
        await preparations?.cancel(flight.preparationID, zapID: flight.zapID)
        note(flight.zapID, "prepare.abandoned", flight.serviceRef, extra: reason)
    }

    /// Renders an error so the log names the cause rather than an enum case number.
    private func describe(_ error: Error) -> String {
        (error as? APIError)?.diagnosticDescription ?? error.localizedDescription
    }

    /// Reports a failed channel change, and falls back to starting the channel outright
    /// only when either:
    ///   1. Nothing is currently playing on screen (`playing == nil`), OR
    ///   2. The preparation failed strictly due to tuner/resource exhaustion (`isAdmissionDenied == true`).
    ///
    /// For any stream-level failure (e.g. timeout, ingest_ended, unpresentable, scrambled,
    /// no PAT/PMT, generation change), the running channel (`playing`) is kept alive and untouched,
    /// preserving the core Make-before-Break safety guarantee.
    private func failOrStartOutright(_ zapID: String, _ serviceRef: String, _ reason: String, isAdmissionDenied: Bool = false) async {
        note(zapID, "zap.failed", serviceRef, extra: reason)

        let allowFallback = (playing == nil) || isAdmissionDenied
        guard allowFallback, let url = streamURL(serviceRef), isCurrent(zapID) else {
            requestedServiceRef = nil
            phase = .failed(serviceRef: serviceRef, reason: reason)
            return
        }

        let fallbackReason = playing == nil
            ? "nothing playing to protect; starting outright"
            : "single tuner / admission denied; breaking before make"
        if isAdmissionDenied {
            note(zapID, "admission.denied", serviceRef, extra: "break-before-make required")
        }
        note(zapID, "zap.fallback", serviceRef, extra: fallbackReason)
        await startOutright(url: url, zapID: zapID, requestedAt: CACurrentMediaTime())
    }

    private func fail(_ zapID: String, _ serviceRef: String, _ reason: String) {
        requestedServiceRef = nil
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
