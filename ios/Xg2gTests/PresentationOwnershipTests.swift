// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import CoreVideo
import Testing

@testable import Xg2g

/// Who owns the screen, and what happens to the stream that no longer does.
///
/// The audit that preceded this found the surface reachable from any playback
/// session: attaching a synchronizer removed the running channel's renderer from the
/// display layer, flushing discarded its queued fields, and resetting the generation
/// made its own frames fail their identity check. Preparing a second channel would
/// have blacked out the first before any commit - break-before-make, triggered
/// earlier than before, by the very mechanism meant to remove it.
///
/// It also found that generations were counted per session instance. Two sessions
/// would each have begun at one, so the outgoing channel's stale frames would have
/// passed the incoming channel's check exactly. That is the case these tests are
/// built around.
@MainActor
struct PresentationOwnershipTests {

    /// A session that produces nothing and only reports what it was told.
    final class FakeSession: PresentablePlaybackSession {
        let presentationSynchronizer = AVSampleBufferRenderSynchronizer()
        var presentationGeneration: PresentationGeneration = .none
        var isPresentable: Bool
        private(set) var audibleGrants: [AudibleGrant] = []
        private(set) var silenceCount = 0

        init(presentable: Bool = true) { self.isPresentable = presentable }

        func becomeAudible(_ grant: AudibleGrant) { audibleGrants.append(grant) }
        func silence() { silenceCount += 1 }

        func surfaceDidSubmitFirstField(pts: CMTime) {}
        func surfaceDidRenderFirstFrame() {}
        func surfaceDidPresentFirstFrame(atScreenTime screenTime: CFTimeInterval) {}
        func surfaceDidPresentFirstFieldImmediately() {}
        func surfaceDidWarn(_ text: String) {}
    }

    private func makeContext() -> (PresentationContext, SystemVideoPresenter) {
        let presenter = SystemVideoPresenter()
        return (PresentationContext(presenter: presenter, renderView: nil), presenter)
    }

    private func makeFrame(generation: Int, pts: Double = 1.0) -> DecodedVideoFrame {
        var buffer: CVPixelBuffer?
        CVPixelBufferCreate(kCFAllocatorDefault, 16, 16, kCVPixelFormatType_32BGRA, nil, &buffer)
        return DecodedVideoFrame(
            pixelBuffer: buffer!,
            pts: CMTime(seconds: pts, preferredTimescale: 90_000),
            structure: .wovenTopFieldFirst,
            generation: generation
        )
    }

    /// The mandatory counter-proof: two sessions that would each have been their own
    /// first zap must not be able to collide.
    @Test("Two sessions never share a presentation generation")
    func generationsAreUniqueAcrossSessions() {
        let (context, _) = makeContext()
        let a = FakeSession()
        let b = FakeSession()

        let genA = context.issueGeneration(to: a)
        let genB = context.issueGeneration(to: b)

        #expect(genA != genB, "per-instance counters would have made both of these the first generation")
        #expect(a.presentationGeneration == genA)
        #expect(b.presentationGeneration == genB)
        #expect(genA != .none && genB != .none)
    }

    /// After the surface is handed to B, nothing stamped with A may reach it - which is
    /// the whole proof, and is asked of the outlet frames actually travel through.
    @Test("After committing B, no output of A is admitted")
    func committingBRejectsEverythingFromA() {
        let (context, _) = makeContext()
        let a = FakeSession()
        let b = FakeSession()
        let genA = context.issueGeneration(to: a)
        let genB = context.issueGeneration(to: b)

        #expect(context.bind(a))
        #expect(context.accepts(genA))

        #expect(context.bind(b))
        #expect(!context.accepts(genA), "a retired generation must not be admitted")
        #expect(context.accepts(genB))

        // The outlet is what a session hands pictures to, so it is where the claim has
        // to hold. A retired session keeps producing for a moment; that has to be
        // harmless rather than merely unlikely.
        context.outlet.enqueue(makeFrame(generation: genA.rawValue))
        context.outlet.enqueue(makeFrame(generation: genB.rawValue))
        #expect(!context.accepts(genA))
    }

    /// The outgoing session is silenced by the commit, not left playing underneath.
    @Test("Committing B silences A exactly once")
    func commitSilencesTheOutgoingSession() {
        let (context, _) = makeContext()
        let a = FakeSession()
        let b = FakeSession()
        _ = context.issueGeneration(to: a)
        let genB = context.issueGeneration(to: b)

        #expect(context.bind(a))
        #expect(a.silenceCount == 0, "binding must not silence the session it is binding")

        #expect(context.bind(b))
        #expect(a.silenceCount == 1)
        #expect(b.audibleGrants.count == 1)
        #expect(b.audibleGrants.first?.presentationGeneration == genB)
    }

    /// A session is audible only once the surface is its own.
    @Test("A prepared session is never granted audibility")
    func preparingDoesNotMakeASessionAudible() {
        let (context, _) = makeContext()
        let playing = FakeSession()
        let preparing = FakeSession()
        _ = context.issueGeneration(to: playing)
        _ = context.issueGeneration(to: preparing)

        #expect(context.bind(playing))
        #expect(preparing.audibleGrants.isEmpty)
        #expect(preparing.silenceCount == 0)
        #expect(context.accepts(preparing.presentationGeneration) == false)
    }

    /// A commit that cannot succeed changes nothing at all.
    @Test("Binding an unpresentable session leaves the running one in place")
    func failedBindLeavesTheRunningSessionOwning() {
        let (context, presenter) = makeContext()
        let a = FakeSession()
        let notReady = FakeSession(presentable: false)
        let genA = context.issueGeneration(to: a)
        _ = context.issueGeneration(to: notReady)

        #expect(context.bind(a))
        let generationBefore = context.visibleGeneration
        let presenterGenerationBefore = presenter.currentGeneration

        #expect(context.bind(notReady) == false)

        #expect(context.visibleGeneration == generationBefore)
        #expect(context.visibleGeneration == genA)
        #expect(presenter.currentGeneration == presenterGenerationBefore)
        #expect(a.silenceCount == 0, "a refused commit must not silence the running session")
        #expect(context.boundSession === a)
    }

    /// A session that never received a generation cannot take the surface.
    @Test("A session with no generation cannot be bound")
    func unstampedSessionCannotBind() {
        let (context, _) = makeContext()
        let stray = FakeSession()

        #expect(context.bind(stray) == false)
        #expect(context.boundSession == nil)
        #expect(context.visibleGeneration == .none)
    }

    /// Only the session on screen may re-arm it. A preparing one asking is a no-op,
    /// not an error: the recovery paths run inside a session and cannot know.
    @Test("A preparing session cannot reset the surface")
    func onlyTheBoundSessionMayResetTheSurface() {
        let (context, presenter) = makeContext()
        let playing = FakeSession()
        let preparing = FakeSession()
        let genPlaying = context.issueGeneration(to: playing)
        _ = context.issueGeneration(to: preparing)
        #expect(context.bind(playing))

        context.requestReset(from: preparing)
        context.requestReattach(from: preparing)

        #expect(context.visibleGeneration == genPlaying)
        #expect(presenter.currentGeneration == genPlaying.rawValue)
        #expect(context.boundSession === playing)
    }
}
